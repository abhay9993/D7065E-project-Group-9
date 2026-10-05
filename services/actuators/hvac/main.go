// Command hvac-actuator is the only process authorised to write HVAC actuator
// state into BuildSim. It owns validation, range-clamping, and reporting; the
// controller proposes a command here instead of writing BuildSim directly.
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
	"sync"
	"syscall"
	"time"
)

type incomingCommand struct {
	Room     string  `json:"room"`
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
}

type appliedState struct {
	Room      string    `json:"room"`
	Setpoint  float64   `json:"setpoint"`
	FanSpeed  float64   `json:"fan_speed"`
	Reason    string    `json:"reason"`
	AppliedAt time.Time `json:"applied_at"`
}

const (
	minSetpointC = 16.0
	maxSetpointC = 28.0
	minFanSpeed  = 0.0
	maxFanSpeed  = 100.0
)

type lastState struct {
	mu     sync.RWMutex
	byRoom map[string]appliedState
}

func newLastState() *lastState {
	return &lastState{byRoom: make(map[string]appliedState)}
}

func (l *lastState) set(s appliedState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byRoom[s.Room] = s
}

func (l *lastState) snapshot() map[string]appliedState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]appliedState, len(l.byRoom))
	for k, v := range l.byRoom {
		out[k] = v
	}
	return out
}

func main() {
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	listenAddr := getenv("LISTEN_ADDR", ":8091")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	ls := newLastState()
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
		setpoint := clamp(in.Setpoint, minSetpointC, maxSetpointC)
		fan := clamp(in.FanSpeed, minFanSpeed, maxFanSpeed)

		if err := setActuatorState(r.Context(), client, buildsimURL, in.Room+"-setpoint", setpoint); err != nil {
			log.Printf("hvac-actuator[%s]: apply setpoint: %v", in.Room, err)
			http.Error(w, "failed to apply setpoint", http.StatusBadGateway)
			return
		}
		if err := setActuatorState(r.Context(), client, buildsimURL, in.Room+"-fan", fan); err != nil {
			log.Printf("hvac-actuator[%s]: apply fan speed: %v", in.Room, err)
			http.Error(w, "failed to apply fan speed", http.StatusBadGateway)
			return
		}

		applied := appliedState{Room: in.Room, Setpoint: setpoint, FanSpeed: fan, Reason: in.Reason, AppliedAt: time.Now()}
		ls.set(applied)
		log.Printf("hvac-actuator[%s]: applied setpoint=%.1f fan=%.0f%% reason=%q", in.Room, setpoint, fan, in.Reason)
		writeJSON(w, applied)
	})

	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ls.snapshot())
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{Addr: listenAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("hvac-actuator: listening on %s", listenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("hvac-actuator: server stopped: %v", err)
	}
}

func setActuatorState(ctx context.Context, client *http.Client, baseURL, actuatorID string, value float64) error {
	body, err := json.Marshal(map[string]string{"state": fmt.Sprintf("%.1f", value)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		baseURL+"/api/actuators/"+url.PathEscape(actuatorID)+"/state", bytes.NewReader(body))
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
		return fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	return nil
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
