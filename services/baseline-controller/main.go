// Command baseline-controller is the evaluation baseline: a fixed-schedule
// HVAC policy with no sensor feedback at all, used to measure how much the
// occupancy-aware controller actually improves on a conventional
// building-management schedule.
//
// The schedule runs on the model clock (the occupancy simulator's), the same
// clock people arrive and leave by. On the wall clock its "office hours" would
// have nothing to do with when anyone is actually in the room.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type command struct {
	Room     string  `json:"room"`
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
	Source   string  `json:"source"`
}

type roomStatus struct {
	LastCmd   command   `json:"last_command"`
	SimTime   time.Time `json:"sim_time"`
	DecidedAt time.Time `json:"decided_at"`
}

// Occupied-hours schedule, independent of any sensor: a conventional
// building-management-system policy (Mon-Fri, fixed hours with an early start
// so rooms are warm by the time people arrive). Setpoints and the setback
// ventilation are the same as the adaptive controller's, so only the *when* differs.
const (
	scheduleSetpoint = 22.0
	scheduleFanSpeed = 60.0 // ventilation for a full office, whether anyone is in or not
	setbackSetpoint  = 18.0
	setbackFanSpeed  = 10.0 // minimum ventilation outside office hours
	occupiedStartHr  = 7
	occupiedEndHr    = 18
)

type status struct {
	mu     sync.RWMutex
	byRoom map[string]roomStatus
}

func newStatus() *status {
	return &status{byRoom: make(map[string]roomStatus)}
}

func (s *status) set(room string, c command, simNow time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byRoom[room] = roomStatus{LastCmd: c, SimTime: simNow, DecidedAt: time.Now()}
}

func (s *status) snapshot() map[string]roomStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]roomStatus, len(s.byRoom))
	for k, v := range s.byRoom {
		out[k] = v
	}
	return out
}

func main() {
	actuatorURL := getenv("ACTUATOR_URL", "http://127.0.0.1:8091")
	occupancyURL := getenv("OCCUPANCY_URL", "http://127.0.0.1:8081")
	rooms := getenvList("ROOM_NAMES", "A1010,A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8098")
	decideEvery := getenvDuration("DECIDE_SECONDS", 10*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := newStatus()
	go serveStatus(listenAddr, st)

	client := &http.Client{Timeout: 5 * time.Second}
	clock := &simClock{url: occupancyURL, client: client}

	ticker := time.NewTicker(decideEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("baseline-controller: shutting down")
			return
		case <-ticker.C:
			simNow := clock.now(ctx)
			for _, room := range rooms {
				cmd := schedule(room, simNow)
				if err := sendCommand(ctx, client, actuatorURL, cmd); err != nil {
					log.Printf("baseline-controller[%s]: send command: %v", room, err)
					continue
				}
				st.set(room, cmd, simNow)
				log.Printf("baseline-controller: room=%s model-time=%s setpoint=%.1f fan=%.0f%% reason=%q",
					room, simNow.Format("Mon 15:04"), cmd.Setpoint, cmd.FanSpeed, cmd.Reason)
			}
		}
	}
}

// schedule is a pure function of model time: Mon-Fri 07:00-18:00 holds a
// fixed setpoint and fan speed; everything else drops to setback. No sensor
// reading is consulted.
func schedule(room string, now time.Time) command {
	weekday := now.Weekday()
	hour := now.Hour()
	occupiedHours := weekday >= time.Monday && weekday <= time.Friday && hour >= occupiedStartHr && hour < occupiedEndHr
	if occupiedHours {
		return command{Room: room, Setpoint: scheduleSetpoint, FanSpeed: scheduleFanSpeed, Source: "baseline",
			Reason: fmt.Sprintf("fixed schedule: occupied hours (%02d:00-%02d:00 Mon-Fri)", occupiedStartHr, occupiedEndHr)}
	}
	return command{Room: room, Setpoint: setbackSetpoint, FanSpeed: setbackFanSpeed, Source: "baseline",
		Reason: "fixed schedule: outside occupied hours"}
}

func sendCommand(ctx context.Context, client *http.Client, actuatorURL string, cmd command) error {
	body, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, actuatorURL+"/commands", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("actuator returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func serveStatus(addr string, st *status) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.snapshot())
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("baseline-controller: status endpoint on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("baseline-controller: status server stopped: %v", err)
	}
}

// simClock follows the occupancy simulator's clock, the single model clock of
// the whole system. If the occupancy simulator is unreachable we extrapolate
// from the last reading with the last known speed factor.
type simClock struct {
	url     string
	client  *http.Client
	sim     time.Time
	real    time.Time
	factor  float64
	running bool
}

func (c *simClock) now(ctx context.Context) time.Time {
	var state struct {
		Sim struct {
			Clock struct {
				Time    time.Time `json:"time"`
				Factor  float64   `json:"factor"`
				Running bool      `json:"running"`
			} `json:"clock"`
		} `json:"sim"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/api/state", nil)
	if err == nil {
		var resp *http.Response
		if resp, err = c.client.Do(req); err == nil {
			err = json.NewDecoder(resp.Body).Decode(&state)
			resp.Body.Close()
		}
	}
	if err == nil && !state.Sim.Clock.Time.IsZero() {
		c.sim, c.real = state.Sim.Clock.Time, time.Now()
		c.factor, c.running = state.Sim.Clock.Factor, state.Sim.Clock.Running
		return c.sim
	}
	log.Printf("baseline-controller: model clock unavailable (%v), extrapolating", err)
	if c.sim.IsZero() {
		return time.Now()
	}
	if !c.running {
		return c.sim
	}
	return c.sim.Add(time.Duration(float64(time.Since(c.real)) * c.factor))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getenvList splits a comma-separated env var into trimmed, non-empty entries.
func getenvList(key, fallback string) []string {
	raw := getenv(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			return time.Duration(seconds) * time.Second
		}
		if d, err := time.ParseDuration(v); err == nil {
			return d // e.g. "500ms", used by the stress test
		}
	}
	return fallback
}
