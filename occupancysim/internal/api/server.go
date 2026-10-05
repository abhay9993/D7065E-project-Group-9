// Package api serves the occupancy simulator's own HTTP interface: JSON state,
// parameter editing, clock control, a server-sent event stream, and the
// embedded web UI.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eislab-cps/D7065E/occupancysim/internal/buildsim"
	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

const maxPeopleInSnapshot = 600

type Server struct {
	Sim        *sim.Simulation
	Publisher  *buildsim.Publisher
	UI         fs.FS
	ConfigPath string // optional: parameters are written here after each change
}

type stateResponse struct {
	Sim      sim.Snapshot    `json:"sim"`
	BuildSim buildsim.Status `json:"buildsim"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Sim.Parameters())
	})
	mux.HandleFunc("GET /api/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, sim.DefaultParameters())
	})
	mux.HandleFunc("PUT /api/config", s.putConfig)
	mux.HandleFunc("POST /api/clock", s.postClock)
	mux.Handle("/", s.static())
	return mux
}

func (s *Server) state() stateResponse {
	return stateResponse{Sim: s.Sim.Snapshot(maxPeopleInSnapshot), BuildSim: s.Publisher.Status()}
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

// events streams the state once per second as server-sent events.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	send := func() bool {
		data, err := json.Marshal(s.state())
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var params sim.Parameters
	if err := decodeBody(r, &params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.Sim.SetParameters(params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.ConfigPath != "" {
		if err := SaveParameters(s.ConfigPath, s.Sim.Parameters()); err != nil {
			log.Printf("save parameters: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, s.Sim.Parameters())
}

type clockRequest struct {
	Factor  *float64 `json:"factor"`
	Running *bool    `json:"running"`
	Time    string   `json:"time"` // "HH:MM"
	Date    string   `json:"date"` // "2006-01-02"
}

func (s *Server) postClock(w http.ResponseWriter, r *http.Request) {
	var req clockRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Factor != nil {
		if err := s.Sim.SetFactor(*req.Factor); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if req.Running != nil {
		s.Sim.SetRunning(*req.Running)
	}
	if req.Time != "" || req.Date != "" {
		current, err := time.Parse(time.RFC3339, s.Sim.Clock().Time)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		target := current
		if req.Date != "" {
			day, err := time.ParseInLocation("2006-01-02", req.Date, current.Location())
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
				return
			}
			target = time.Date(day.Year(), day.Month(), day.Day(), current.Hour(), current.Minute(), current.Second(), 0, current.Location())
		}
		if req.Time != "" {
			minutes, err := sim.ParseClock(req.Time)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			target = time.Date(target.Year(), target.Month(), target.Day(), int(minutes)/60, int(minutes)%60, 0, 0, target.Location())
		}
		s.Sim.SetTime(target)
	}
	writeJSON(w, http.StatusOK, s.Sim.Clock())
}

// static serves the embedded single-page UI with an index.html fallback.
func (s *Server) static() http.Handler {
	fileServer := http.FileServer(http.FS(s.UI))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(s.UI, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "The web UI has not been built. Run `make ui` (requires Node.js) or use the JSON API under /api.\n")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(s.UI, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func decodeBody(r *http.Request, into any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// LoadParameters reads parameters from a JSON file, returning defaults when the
// file does not exist.
func LoadParameters(path string) (sim.Parameters, error) {
	params := sim.DefaultParameters()
	if path == "" {
		return params, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return params, nil
	}
	if err != nil {
		return params, err
	}
	if err := json.Unmarshal(data, &params); err != nil {
		return params, fmt.Errorf("parse %s: %w", path, err)
	}
	return params, params.Normalize()
}

func SaveParameters(path string, params sim.Parameters) error {
	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
