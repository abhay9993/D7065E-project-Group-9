// Command hvac-actuator is the only process authorised to write HVAC actuator
// state into BuildSim. It owns validation, range-clamping, and reporting; the
// controllers propose a command here instead of writing BuildSim directly.
//
// Two safety rules live here, independent of any controller:
//   - ownership: each room accepts commands from exactly one source
//     (ROOM_OWNERS), so the adaptive controller and the baseline can never
//     fight over a room;
//   - a command lease (watchdog): if a room's controller goes quiet for
//     COMMAND_TIMEOUT_SECONDS, the room is put in a safe state instead of
//     holding its last command forever (e.g. a fan at 81 % in an empty room).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type incomingCommand struct {
	Room     string  `json:"room"`
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
	Source   string  `json:"source"`
}

type appliedState struct {
	Room      string    `json:"room"`
	Setpoint  float64   `json:"setpoint"`
	FanSpeed  float64   `json:"fan_speed"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Watchdog  bool      `json:"watchdog"` // true while the safe state is applied by the lease
	AppliedAt time.Time `json:"applied_at"`
}

const (
	minSetpointC = 16.0
	maxSetpointC = 28.0
	minFanSpeed  = 0.0
	maxFanSpeed  = 100.0

	// The watchdog's safe state: the same command the controller itself uses
	// when it cannot trust its inputs.
	safeSetpointC = 21.0
	safeFanSpeed  = 50.0
)

type actuator struct {
	buildsimURL string
	client      *http.Client
	owners      map[string]string // room -> the only source allowed to command it
	timeout     time.Duration

	mu          sync.RWMutex
	byRoom      map[string]appliedState
	lastCommand map[string]time.Time
}

func newActuator(buildsimURL string, owners map[string]string, timeout time.Duration) *actuator {
	a := &actuator{
		buildsimURL: buildsimURL,
		client:      &http.Client{Timeout: 5 * time.Second},
		owners:      owners,
		timeout:     timeout,
		byRoom:      map[string]appliedState{},
		lastCommand: map[string]time.Time{},
	}
	for room := range owners {
		a.lastCommand[room] = time.Now() // a controller that never shows up also trips the watchdog
	}
	return a
}

func main() {
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	listenAddr := getenv("LISTEN_ADDR", ":8091")
	owners := parseOwners(getenv("ROOM_OWNERS", "A1009=controller,A1013=controller,A1010=baseline,A1015=baseline"))
	timeout := getenvDuration("COMMAND_TIMEOUT_SECONDS", 60*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := newActuator(buildsimURL, owners, timeout)
	go a.watchdog(ctx)

	server := &http.Server{Addr: listenAddr, Handler: a.routes()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("hvac-actuator: listening on %s, owners=%v, lease=%s", listenAddr, owners, timeout)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("hvac-actuator: server stopped: %v", err)
	}
}

func (a *actuator) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /commands", func(w http.ResponseWriter, r *http.Request) {
		var in incomingCommand
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid command: "+err.Error(), http.StatusBadRequest)
			return
		}
		if in.Room == "" {
			http.Error(w, "room is required", http.StatusBadRequest)
			return
		}
		if owner, ok := a.owners[in.Room]; !ok || owner != in.Source {
			log.Printf("hvac-actuator[%s]: refused command from %q (owner %q)", in.Room, in.Source, owner)
			http.Error(w, fmt.Sprintf("room %s is not commanded by %q", in.Room, in.Source), http.StatusForbidden)
			return
		}
		a.mu.Lock()
		a.lastCommand[in.Room] = time.Now()
		a.mu.Unlock()

		applied, err := a.apply(r.Context(), in, false)
		if err != nil {
			log.Printf("hvac-actuator[%s]: %v", in.Room, err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, applied)
	})

	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		writeJSON(w, a.byRoom)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// apply clamps the command to the safe range and writes it to BuildSim.
func (a *actuator) apply(ctx context.Context, in incomingCommand, watchdog bool) (appliedState, error) {
	setpoint := clamp(in.Setpoint, minSetpointC, maxSetpointC)
	fan := clamp(in.FanSpeed, minFanSpeed, maxFanSpeed)

	if err := a.setActuatorState(ctx, in.Room+"-setpoint", setpoint); err != nil {
		return appliedState{}, fmt.Errorf("apply setpoint: %w", err)
	}
	if err := a.setActuatorState(ctx, in.Room+"-fan", fan); err != nil {
		return appliedState{}, fmt.Errorf("apply fan speed: %w", err)
	}
	applied := appliedState{Room: in.Room, Setpoint: setpoint, FanSpeed: fan, Reason: in.Reason,
		Source: in.Source, Watchdog: watchdog, AppliedAt: time.Now()}
	a.mu.Lock()
	a.byRoom[in.Room] = applied
	a.mu.Unlock()
	log.Printf("hvac-actuator[%s]: applied setpoint=%.1f fan=%.0f%% source=%s reason=%q", in.Room, setpoint, fan, in.Source, in.Reason)
	return applied, nil
}

// watchdog checks the command leases every few seconds.
func (a *actuator) watchdog(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkLeases(ctx, time.Now())
		}
	}
}

// checkLeases puts every room whose controller has been quiet for longer than
// the timeout into the safe state (once; it stays there until a command comes).
func (a *actuator) checkLeases(ctx context.Context, now time.Time) {
	for room, owner := range a.owners {
		a.mu.RLock()
		quiet := now.Sub(a.lastCommand[room])
		alreadySafe := a.byRoom[room].Watchdog
		a.mu.RUnlock()
		if quiet < a.timeout || alreadySafe {
			continue
		}
		reason := fmt.Sprintf("watchdog: no command from %s for %.0fs, safe state", owner, quiet.Seconds())
		if _, err := a.apply(ctx, incomingCommand{Room: room, Setpoint: safeSetpointC, FanSpeed: safeFanSpeed,
			Reason: reason, Source: "watchdog"}, true); err != nil {
			log.Printf("hvac-actuator[%s]: watchdog could not apply safe state: %v", room, err)
		}
	}
}

func (a *actuator) setActuatorState(ctx context.Context, actuatorID string, value float64) error {
	body, err := json.Marshal(map[string]string{"state": fmt.Sprintf("%.1f", value)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		a.buildsimURL+"/api/actuators/"+url.PathEscape(actuatorID)+"/state", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// parseOwners reads "A1009=controller,A1010=baseline" into a map.
func parseOwners(raw string) map[string]string {
	owners := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		room, owner, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && room != "" && owner != "" {
			owners[room] = owner
		}
	}
	return owners
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
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
		if d, err := time.ParseDuration(v); err == nil {
			return d // e.g. "500ms", used by the stress test
		}
	}
	return fallback
}
