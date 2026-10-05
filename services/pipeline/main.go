// Command pipeline ingests sensor readings from the sensor processes and keeps
// recent per-room history in memory for the controller and the dashboard.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type reading struct {
	Room      string    `json:"room"`
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

// roomState is the latest reading of each known type for a room, used by the controller.
type roomState struct {
	Room     string              `json:"room"`
	Readings map[string]reading  `json:"readings"`
	Stale    map[string]bool     `json:"stale"`
	Forecast map[string]forecast `json:"forecast"`
}

// forecast is a short-horizon linear trend, not a trained model: a least-squares
// slope over recent history extrapolated forward. Deliberately simple per the
// report guide ("choose the simplest that meets the requirement").
type forecast struct {
	SlopePerMin float64 `json:"slope_per_min"`
	Predicted5m float64 `json:"predicted_5m"`
	Samples     int     `json:"samples"`
}

// forecastWindow bounds how much history feeds the trend, roughly the last 2
// minutes at the sensors' default 5s poll interval.
const forecastWindow = 24

const historyLimit = 200

type store struct {
	mu      sync.RWMutex
	latest  map[string]map[string]reading   // room -> type -> latest reading
	history map[string][]reading            // "room|type" -> ring buffer
}

func newStore() *store {
	return &store{
		latest:  make(map[string]map[string]reading),
		history: make(map[string][]reading),
	}
}

func (s *store) ingest(r reading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest[r.Room] == nil {
		s.latest[r.Room] = make(map[string]reading)
	}
	s.latest[r.Room][r.Type] = r

	key := r.Room + "|" + r.Type
	h := append(s.history[key], r)
	if len(h) > historyLimit {
		h = h[len(h)-historyLimit:]
	}
	s.history[key] = h
}

func (s *store) state(room string, staleAfter time.Duration) roomState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := roomState{Room: room, Readings: map[string]reading{}, Stale: map[string]bool{}, Forecast: map[string]forecast{}}
	now := time.Now()
	for t, r := range s.latest[room] {
		out.Readings[t] = r
		out.Stale[t] = now.Sub(r.Timestamp) > staleAfter
		out.Forecast[t] = trend(s.history[room+"|"+t])
	}
	return out
}

// trend fits a least-squares line through the most recent samples (time in
// minutes vs. value) and extrapolates 5 minutes ahead. Fewer than two samples
// yields a flat forecast (slope 0), so callers fall back to reactive behaviour.
func trend(history []reading) forecast {
	samples := history
	if len(samples) > forecastWindow {
		samples = samples[len(samples)-forecastWindow:]
	}
	n := len(samples)
	if n < 2 {
		last := 0.0
		if n == 1 {
			last = samples[0].Value
		}
		return forecast{Predicted5m: last, Samples: n}
	}

	t0 := samples[0].Timestamp
	var sumX, sumY, sumXY, sumX2 float64
	for _, r := range samples {
		x := r.Timestamp.Sub(t0).Minutes()
		y := r.Value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	var slope float64
	if denom != 0 {
		slope = (nf*sumXY - sumX*sumY) / denom
	}
	current := samples[n-1].Value
	return forecast{SlopePerMin: slope, Predicted5m: current + slope*5, Samples: n}
}

func (s *store) historyFor(room, sensorType string, limit int) []reading {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.history[room+"|"+sensorType]
	if limit > 0 && limit < len(h) {
		h = h[len(h)-limit:]
	}
	out := make([]reading, len(h))
	copy(out, h)
	return out
}

func main() {
	listenAddr := getenv("LISTEN_ADDR", ":8090")
	staleAfter := getenvDuration("STALE_AFTER_SECONDS", 30*time.Second)

	s := newStore()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/readings", func(w http.ResponseWriter, r *http.Request) {
		var in reading
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid reading: "+err.Error(), http.StatusBadRequest)
			return
		}
		if in.Room == "" || in.Type == "" {
			http.Error(w, "room and type are required", http.StatusBadRequest)
			return
		}
		if in.Timestamp.IsZero() {
			in.Timestamp = time.Now()
		}
		s.ingest(in)
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/rooms/state", func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("room")
		if room == "" {
			http.Error(w, "room query parameter is required", http.StatusBadRequest)
			return
		}
		writeJSON(w, s.state(room, staleAfter))
	})

	mux.HandleFunc("GET /api/rooms/history", func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("room")
		sensorType := r.URL.Query().Get("type")
		if room == "" || sensorType == "" {
			http.Error(w, "room and type query parameters are required", http.StatusBadRequest)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, s.historyFor(room, sensorType, limit))
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("pipeline: listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("pipeline: server stopped: %v", err)
	}
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
	}
	return fallback
}
