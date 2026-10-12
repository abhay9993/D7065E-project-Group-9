// Command pipeline ingests sensor readings from the sensor processes and serves
// them to the controller, the evaluation and the dashboard.
//
// Every reading goes through the same steps:
//
//	validate  -> unknown type, NaN or out-of-range values are rejected (422) and quarantined
//	dedupe    -> a reading not newer than the last one for that room/type is a duplicate (409)
//	store     -> appended to readings.jsonl on a volume, then acknowledged (204)
//	serve     -> latest value per room/type with freshness and quality flags
//
// The JSONL file is the raw history. It is replayed on start-up, so restarting
// the pipeline loses nothing.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	Room     string             `json:"room"`
	Readings map[string]reading `json:"readings"`
	Stale    map[string]bool    `json:"stale"`
	Quality  map[string]string  `json:"quality"` // ok | stale | stuck
}

// validRange is what a working sensor of each type can physically report.
var validRange = map[string][2]float64{
	"temperature": {-20, 50},
	"humidity":    {0, 100},
	"occupancy":   {0, 200},
	"energy":      {0, 1e6},
}

const (
	historyLimit = 720 // readings kept in memory per room/type (1 h at a 5 s poll)
	// stuckWindow identical readings in a row mean a frozen sensor. Real sensors
	// are noisy (±0.1 °C, ±0.5 %RH), so twelve exact repeats (one minute) do not
	// happen by chance. Only analogue signals are checked: a people counter or an
	// idle energy meter can legitimately repeat itself.
	stuckWindow = 12
)

var checkStuck = map[string]bool{"temperature": true, "humidity": true}

var (
	errInvalid   = errors.New("invalid reading")
	errDuplicate = errors.New("duplicate or out-of-order reading")
)

type counters struct {
	Accepted   int       `json:"accepted"`
	Rejected   int       `json:"rejected"`
	Duplicates int       `json:"duplicates"`
	Replayed   int       `json:"replayed"`
	LastWrite  time.Time `json:"last_write"`
}

type store struct {
	mu       sync.RWMutex
	latest   map[string]map[string]reading // room -> type -> latest reading
	history  map[string][]reading          // "room|type" -> most recent readings
	file     *os.File                      // readings.jsonl, append-only (nil = memory only)
	rejected *os.File                      // rejected.jsonl, the quarantine
	counts   counters
}

func newStore() *store {
	return &store{
		latest:  make(map[string]map[string]reading),
		history: make(map[string][]reading),
	}
}

// openStore replays the existing history from dir and opens the files for appending.
func openStore(dir string) (*store, error) {
	s := newStore()
	path := filepath.Join(dir, "readings.jsonl")
	if err := s.replay(path); err != nil {
		return nil, err
	}
	var err error
	if s.file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		return nil, err
	}
	if s.rejected, err = os.OpenFile(filepath.Join(dir, "rejected.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *store) replay(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	broken := 0
	for scanner.Scan() {
		var r reading
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			broken++ // e.g. a half-written last line after a crash
			continue
		}
		s.remember(r)
		s.counts.Replayed++
	}
	log.Printf("pipeline: replayed %d readings from %s (%d unreadable lines skipped)", s.counts.Replayed, path, broken)
	return scanner.Err()
}

func validate(r reading, now time.Time) error {
	limits, known := validRange[r.Type]
	switch {
	case r.Room == "" || !strings.Contains(r.Room, "/"):
		return fmt.Errorf("%w: room must be <level>/<room>, got %q", errInvalid, r.Room)
	case !known:
		return fmt.Errorf("%w: unknown type %q", errInvalid, r.Type)
	case math.IsNaN(r.Value) || math.IsInf(r.Value, 0):
		return fmt.Errorf("%w: value is not a number", errInvalid)
	case r.Value < limits[0] || r.Value > limits[1]:
		return fmt.Errorf("%w: %s %.2f outside %.0f..%.0f", errInvalid, r.Type, r.Value, limits[0], limits[1])
	case r.Timestamp.After(now.Add(time.Minute)):
		return fmt.Errorf("%w: timestamp %s is in the future", errInvalid, r.Timestamp)
	}
	return nil
}

// ingest validates, dedupes and stores one reading. It only returns nil once the
// reading is on disk, so a sensor that gets 204 can safely forget it.
func (s *store) ingest(r reading) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validate(r, time.Now()); err != nil {
		s.counts.Rejected++
		s.appendLine(s.rejected, map[string]any{"reading": r, "reason": err.Error(), "at": time.Now().UTC()})
		return err
	}
	if last, ok := s.latest[r.Room][r.Type]; ok && !r.Timestamp.After(last.Timestamp) {
		s.counts.Duplicates++
		return errDuplicate
	}
	if err := s.appendLine(s.file, r); err != nil {
		return fmt.Errorf("store reading: %w", err)
	}
	s.remember(r)
	s.counts.Accepted++
	s.counts.LastWrite = time.Now().UTC()
	return nil
}

func (s *store) appendLine(f *os.File, v any) error {
	if f == nil {
		return nil
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// remember keeps the reading in memory (latest + bounded history).
func (s *store) remember(r reading) {
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
	out := roomState{Room: room, Readings: map[string]reading{}, Stale: map[string]bool{}, Quality: map[string]string{}}
	now := time.Now()
	for t, r := range s.latest[room] {
		out.Readings[t] = r
		out.Stale[t] = now.Sub(r.Timestamp) > staleAfter
		switch {
		case out.Stale[t]:
			out.Quality[t] = "stale"
		case checkStuck[t] && stuck(s.history[room+"|"+t]):
			out.Quality[t] = "stuck"
		default:
			out.Quality[t] = "ok"
		}
	}
	return out
}

// stuck reports whether the last stuckWindow readings are all exactly equal.
func stuck(history []reading) bool {
	if len(history) < stuckWindow {
		return false
	}
	recent := history[len(history)-stuckWindow:]
	for _, r := range recent[1:] {
		if r.Value != recent[0].Value {
			return false
		}
	}
	return true
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
	dataDir := getenv("DATA_DIR", "/data")

	s, err := openStore(dataDir)
	if err != nil {
		log.Fatalf("pipeline: open store in %s: %v", dataDir, err)
	}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/readings", func(w http.ResponseWriter, r *http.Request) {
		var in reading
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid reading: "+err.Error(), http.StatusBadRequest)
			return
		}
		if in.Timestamp.IsZero() {
			in.Timestamp = time.Now().UTC()
		}
		switch err := s.ingest(in); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, errInvalid):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		case errors.Is(err, errDuplicate):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			log.Printf("pipeline: %v", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
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

	// metrics is the pipeline's own telemetry: what came in, what was refused, and
	// when the last reading was stored.
	mux.HandleFunc("GET /api/metrics", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		writeJSON(w, s.counts)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("pipeline: listening on %s, storing in %s", listenAddr, dataDir)
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
		if d, err := time.ParseDuration(v); err == nil {
			return d // e.g. "500ms", used by the stress test
		}
	}
	return fallback
}
