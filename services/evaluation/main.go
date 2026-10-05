// Command evaluation continuously measures the running system: an energy
// proxy (mean applied fan speed) and a comfort proxy (fraction of occupied
// time within the comfort band), per room, and a side-by-side comparison
// between the adaptive controller's rooms and the fixed-schedule baseline
// room. This is evidence for the report's evaluation section, not a
// replacement for it.
package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
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

type reading struct {
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

type roomState struct {
	Readings map[string]reading `json:"readings"`
}

type appliedState struct {
	Setpoint  float64   `json:"setpoint"`
	FanSpeed  float64   `json:"fan_speed"`
	AppliedAt time.Time `json:"applied_at"`
}

// comfortBandC must match the controller's own comfort band so "in comfort"
// means the same thing here as it did when the decision was made.
const comfortBandC = 1.0

type roomAcc struct {
	Samples         int
	FanSpeedSum     float64
	OccupiedSamples int
	ComfortSamples  int
	SetbackSamples  int
	StartedAt       time.Time
}

type report struct {
	Rooms      map[string]roomReport `json:"rooms"`
	Comparison comparison            `json:"comparison"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

type roomReport struct {
	Samples            int       `json:"samples"`
	AvgFanSpeed        float64   `json:"avg_fan_speed"`
	OccupiedPct        float64   `json:"occupied_pct"`
	ComfortPctOccupied float64   `json:"comfort_pct_when_occupied"`
	SetbackPct         float64   `json:"setback_pct"`
	StartedAt          time.Time `json:"started_at"`
}

type comparison struct {
	AdaptiveAvgFanSpeed        float64 `json:"adaptive_avg_fan_speed"`
	BaselineAvgFanSpeed        float64 `json:"baseline_avg_fan_speed"`
	AdaptiveComfortPctOccupied float64 `json:"adaptive_comfort_pct_when_occupied"`
	BaselineComfortPctOccupied float64 `json:"baseline_comfort_pct_when_occupied"`
	AdaptiveSetbackPct         float64 `json:"adaptive_setback_pct"`
	BaselineSetbackPct         float64 `json:"baseline_setback_pct"`
}

type store struct {
	mu     sync.RWMutex
	byRoom map[string]*roomAcc
}

func newStore() *store {
	return &store{byRoom: make(map[string]*roomAcc)}
}

func (s *store) record(room string, occupancy, temp, setpoint, fanSpeed float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.byRoom[room]
	if !ok {
		a = &roomAcc{StartedAt: time.Now()}
		s.byRoom[room] = a
	}
	a.Samples++
	a.FanSpeedSum += fanSpeed
	if fanSpeed <= 0.01 {
		a.SetbackSamples++
	}
	if occupancy > 0 {
		a.OccupiedSamples++
		if math.Abs(temp-setpoint) <= comfortBandC {
			a.ComfortSamples++
		}
	}
}

func (s *store) snapshot() map[string]roomAcc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]roomAcc, len(s.byRoom))
	for k, v := range s.byRoom {
		out[k] = *v
	}
	return out
}

func main() {
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	actuatorURL := getenv("ACTUATOR_URL", "http://127.0.0.1:8091")
	level := getenv("ROOM_LEVEL", "level0")
	adaptiveRooms := getenvList("ADAPTIVE_ROOMS", "A1009,A1010,A1013")
	baselineRooms := getenvList("BASELINE_ROOMS", "A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8099")
	poll := getenvDuration("POLL_SECONDS", 10*time.Second)

	allRooms := append(append([]string{}, adaptiveRooms...), baselineRooms...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	s := newStore()

	go pollLoop(ctx, client, s, pipelineURL, actuatorURL, level, allRooms, poll)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/report", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildReport(s.snapshot(), adaptiveRooms, baselineRooms))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("evaluation: listening on %s (adaptive=%v baseline=%v)", listenAddr, adaptiveRooms, baselineRooms)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("evaluation: server stopped: %v", err)
	}
}

func pollLoop(ctx context.Context, client *http.Client, s *store, pipelineURL, actuatorURL, level string, rooms []string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var actuatorState map[string]appliedState
			if err := getJSON(ctx, client, actuatorURL+"/state", &actuatorState); err != nil {
				log.Printf("evaluation: actuator state: %v", err)
				continue
			}
			for _, room := range rooms {
				roomKey := level + "/" + room
				var rs roomState
				if err := getJSON(ctx, client, pipelineURL+"/api/rooms/state?room="+url.QueryEscape(roomKey), &rs); err != nil {
					log.Printf("evaluation[%s]: pipeline state: %v", room, err)
					continue
				}
				occ, haveOcc := rs.Readings["occupancy"]
				temp, haveTemp := rs.Readings["temperature"]
				applied, haveApplied := actuatorState[room]
				if !haveOcc || !haveTemp || !haveApplied {
					continue
				}
				s.record(room, occ.Value, temp.Value, applied.Setpoint, applied.FanSpeed)
			}
		}
	}
}

func buildReport(snapshot map[string]roomAcc, adaptiveRooms, baselineRooms []string) report {
	rep := report{Rooms: map[string]roomReport{}, UpdatedAt: time.Now()}
	for room, a := range snapshot {
		rep.Rooms[room] = roomReportFrom(a)
	}
	rep.Comparison = comparison{
		AdaptiveAvgFanSpeed:        groupAvgFan(snapshot, adaptiveRooms),
		BaselineAvgFanSpeed:        groupAvgFan(snapshot, baselineRooms),
		AdaptiveComfortPctOccupied: groupComfortPct(snapshot, adaptiveRooms),
		BaselineComfortPctOccupied: groupComfortPct(snapshot, baselineRooms),
		AdaptiveSetbackPct:         groupSetbackPct(snapshot, adaptiveRooms),
		BaselineSetbackPct:         groupSetbackPct(snapshot, baselineRooms),
	}
	return rep
}

func roomReportFrom(a roomAcc) roomReport {
	if a.Samples == 0 {
		return roomReport{StartedAt: a.StartedAt}
	}
	rr := roomReport{
		Samples:     a.Samples,
		AvgFanSpeed: a.FanSpeedSum / float64(a.Samples),
		OccupiedPct: 100 * float64(a.OccupiedSamples) / float64(a.Samples),
		SetbackPct:  100 * float64(a.SetbackSamples) / float64(a.Samples),
		StartedAt:   a.StartedAt,
	}
	if a.OccupiedSamples > 0 {
		rr.ComfortPctOccupied = 100 * float64(a.ComfortSamples) / float64(a.OccupiedSamples)
	}
	return rr
}

func groupAvgFan(snapshot map[string]roomAcc, rooms []string) float64 {
	var sum, samples float64
	for _, room := range rooms {
		if a, ok := snapshot[room]; ok {
			sum += a.FanSpeedSum
			samples += float64(a.Samples)
		}
	}
	if samples == 0 {
		return 0
	}
	return sum / samples
}

func groupComfortPct(snapshot map[string]roomAcc, rooms []string) float64 {
	var comfort, occupied float64
	for _, room := range rooms {
		if a, ok := snapshot[room]; ok {
			comfort += float64(a.ComfortSamples)
			occupied += float64(a.OccupiedSamples)
		}
	}
	if occupied == 0 {
		return 0
	}
	return 100 * comfort / occupied
}

func groupSetbackPct(snapshot map[string]roomAcc, rooms []string) float64 {
	var setback, samples float64
	for _, room := range rooms {
		if a, ok := snapshot[room]; ok {
			setback += float64(a.SetbackSamples)
			samples += float64(a.Samples)
		}
	}
	if samples == 0 {
		return 0
	}
	return 100 * setback / samples
}

func getJSON(ctx context.Context, client *http.Client, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

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
	}
	return fallback
}
