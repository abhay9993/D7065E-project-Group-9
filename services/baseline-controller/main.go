// Command baseline-controller is the evaluation baseline: a fixed-schedule
// HVAC policy with no sensor feedback at all, used to measure how much the
// occupancy/temperature-aware controller actually improves on a conventional
// building-management schedule.
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
	"sync"
	"syscall"
	"time"
)

type command struct {
	Room     string  `json:"room"`
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
}

type roomStatus struct {
	LastCmd   command   `json:"last_command"`
	DecidedAt time.Time `json:"decided_at"`
}

// Occupied-hours schedule, independent of any sensor: a conventional
// building-management-system policy (Mon-Fri, fixed hours, fixed setpoint and
// fan), used only as the point of comparison for the adaptive controller.
const (
	scheduleSetpoint = 22.0
	scheduleFanSpeed = 30.0
	setbackSetpoint  = 18.0
	setbackFanSpeed  = 0.0
	occupiedStartHr  = 8
	occupiedEndHr    = 18
)

type status struct {
	mu     sync.RWMutex
	byRoom map[string]roomStatus
}

func newStatus() *status {
	return &status{byRoom: make(map[string]roomStatus)}
}

func (s *status) set(room string, c command) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byRoom[room] = roomStatus{LastCmd: c, DecidedAt: time.Now()}
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
	room := getenv("ROOM_NAME", "A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8098")
	decideEvery := getenvDuration("DECIDE_SECONDS", 10*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st := newStatus()
	go serveStatus(listenAddr, st)

	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(decideEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("baseline-controller: shutting down")
			return
		case <-ticker.C:
			cmd := schedule(room, time.Now())
			if err := sendCommand(ctx, client, actuatorURL, cmd); err != nil {
				log.Printf("baseline-controller[%s]: send command: %v", room, err)
				continue
			}
			st.set(room, cmd)
			log.Printf("baseline-controller: room=%s setpoint=%.1f fan=%.0f%% reason=%q",
				room, cmd.Setpoint, cmd.FanSpeed, cmd.Reason)
		}
	}
}

// schedule is a pure function of wall-clock time: Mon-Fri 08:00-18:00 holds a
// fixed setpoint and fan speed; everything else drops to setback. No sensor
// reading is consulted.
func schedule(room string, now time.Time) command {
	weekday := now.Weekday()
	hour := now.Hour()
	occupiedHours := weekday >= time.Monday && weekday <= time.Friday && hour >= occupiedStartHr && hour < occupiedEndHr
	if occupiedHours {
		return command{Room: room, Setpoint: scheduleSetpoint, FanSpeed: scheduleFanSpeed,
			Reason: fmt.Sprintf("fixed schedule: occupied hours (%02d:00-%02d:00 Mon-Fri)", occupiedStartHr, occupiedEndHr)}
	}
	return command{Room: room, Setpoint: setbackSetpoint, FanSpeed: setbackFanSpeed,
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}
