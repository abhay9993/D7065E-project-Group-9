// Command evaluation continuously measures the running system and compares the
// adaptive controller's rooms with the fixed-schedule baseline rooms.
//
// What it measures, per room, every POLL_SECONDS (one sample = one model-time step):
//   - comfort: share of occupied time inside a FIXED band of 20–24 °C
//     (EN 16798-1, category II, offices in the heating season). Comparing the
//     temperature with whatever setpoint the controller had chosen would let a
//     controller grade its own homework;
//   - comfort at arrival: the same, but only for the first 30 model minutes
//     after someone walks in (this is what pre-heating is for);
//   - energy: kWh from the room's energy meter (heating/cooling + fan), per m².
//
// Samples with stale or stuck inputs are skipped and counted, not scored. The
// accumulators are saved to a volume so a restart does not lose the evidence,
// and every scored sample is appended to samples.jsonl for plotting.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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
	Quality  map[string]string  `json:"quality"`
}

type appliedState struct {
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
	Source   string  `json:"source"`
	Watchdog bool    `json:"watchdog"`
}

const (
	comfortMinC   = 20.0
	comfortMaxC   = 24.0
	arrivalWindow = 30 * time.Minute
	maxStep       = 30 * time.Minute // a longer gap (clock jump, outage) is not credited as one sample
	// maxUnitWPerM2 is the most a room's unit can use (100 W/m² heating/cooling + 5 W/m² fan);
	// an energy step bigger than twice that is a meter glitch, not consumption.
	maxUnitWPerM2 = 105.0
)

// roomAcc accumulates one room's evidence. Durations are model time.
type roomAcc struct {
	Group          string    `json:"group"`
	AreaM2         float64   `json:"area_m2"`
	Samples        int       `json:"samples"`
	SkippedBadData int       `json:"skipped_bad_data"`
	Hours          float64   `json:"hours"`
	OccupiedHours  float64   `json:"occupied_hours"`
	ComfortHours   float64   `json:"comfort_hours"`
	ArrivalHours   float64   `json:"arrival_hours"`
	ArrivalComfort float64   `json:"arrival_comfort_hours"`
	EnergyKWh      float64   `json:"energy_kwh"`
	LastEnergy     float64   `json:"last_energy_kwh"`
	MeterGlitches  int       `json:"meter_glitches"`
	LastSample     time.Time `json:"last_sample"`
	ArrivedAt      time.Time `json:"arrived_at"`
	WasOccupied    bool      `json:"was_occupied"`
}

// sample is one scored observation of a room.
type sample struct {
	Occupancy   float64
	Temperature float64
	EnergyKWh   float64
}

// record scores one sample taken at model time now.
func (a *roomAcc) record(now time.Time, s sample) {
	occupied := s.Occupancy > 0
	if occupied && !a.WasOccupied {
		a.ArrivedAt = now
	}
	a.WasOccupied = occupied

	first := a.LastSample.IsZero()
	step := now.Sub(a.LastSample)
	a.LastSample = now
	if first || step <= 0 || step > maxStep {
		a.LastEnergy = s.EnergyKWh // start (or restart) counting from here
		return
	}
	hours := step.Hours()
	a.Samples++
	a.Hours += hours
	delta := s.EnergyKWh - a.LastEnergy
	possible := 2 * maxUnitWPerM2 * a.AreaM2 * hours / 1000
	switch {
	case delta < 0:
		a.MeterGlitches++ // the meter went backwards (reset after a restart): skip
	case a.AreaM2 > 0 && delta > possible:
		a.MeterGlitches++ // more than the unit can physically use: skip
	default:
		a.EnergyKWh += delta
	}
	a.LastEnergy = s.EnergyKWh

	if !occupied {
		return
	}
	inBand := s.Temperature >= comfortMinC && s.Temperature <= comfortMaxC
	a.OccupiedHours += hours
	if inBand {
		a.ComfortHours += hours
	}
	if now.Sub(a.ArrivedAt) < arrivalWindow {
		a.ArrivalHours += hours
		if inBand {
			a.ArrivalComfort += hours
		}
	}
}

type store struct {
	mu     sync.RWMutex
	path   string
	ByRoom map[string]*roomAcc `json:"rooms"`
	Since  time.Time           `json:"since"` // model time of the first sample
}

func loadStore(path string) *store {
	s := &store{path: path, ByRoom: map[string]*roomAcc{}}
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, s) == nil {
		log.Printf("evaluation: resumed %d rooms from %s", len(s.ByRoom), path)
	}
	return s
}

func (s *store) save() {
	s.mu.RLock()
	data, err := json.Marshal(s)
	s.mu.RUnlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(s.path+".tmp", data, 0o644); err == nil {
		_ = os.Rename(s.path+".tmp", s.path)
	}
}

func main() {
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	actuatorURL := getenv("ACTUATOR_URL", "http://127.0.0.1:8091")
	occupancyURL := getenv("OCCUPANCY_URL", "http://127.0.0.1:8081")
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	level := getenv("ROOM_LEVEL", "level0")
	adaptiveRooms := getenvList("ADAPTIVE_ROOMS", "A1009,A1013")
	baselineRooms := getenvList("BASELINE_ROOMS", "A1010,A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8099")
	dataDir := getenv("DATA_DIR", "/data")
	poll := getenvDuration("POLL_SECONDS", 10*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	s := loadStore(filepath.Join(dataDir, "evaluation.json"))
	areas := roomAreas(ctx, client, buildsimURL, level)
	for group, rooms := range map[string][]string{"adaptive": adaptiveRooms, "baseline": baselineRooms} {
		for _, room := range rooms {
			if s.ByRoom[room] == nil {
				s.ByRoom[room] = &roomAcc{}
			}
			s.ByRoom[room].Group, s.ByRoom[room].AreaM2 = group, areas[room]
		}
	}
	samples, err := os.OpenFile(filepath.Join(dataDir, "samples.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("evaluation: no samples file (%v)", err)
	}

	go pollLoop(ctx, client, s, samples, pipelineURL, actuatorURL, occupancyURL, level, poll)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/report", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildReport(s))
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

func pollLoop(ctx context.Context, client *http.Client, s *store, samples *os.File, pipelineURL, actuatorURL, occupancyURL, level string, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			simNow, err := modelTime(ctx, client, occupancyURL)
			if err != nil {
				log.Printf("evaluation: model clock: %v", err)
				continue
			}
			var applied map[string]appliedState
			if err := getJSON(ctx, client, actuatorURL+"/state", &applied); err != nil {
				log.Printf("evaluation: actuator state: %v", err)
			}
			s.mu.Lock()
			if s.Since.IsZero() {
				s.Since = simNow
			}
			for room, acc := range s.ByRoom {
				var rs roomState
				if err := getJSON(ctx, client, pipelineURL+"/api/rooms/state?room="+url.QueryEscape(level+"/"+room), &rs); err != nil {
					log.Printf("evaluation[%s]: pipeline state: %v", room, err)
					continue
				}
				if !usable(rs, "occupancy", "temperature", "energy") {
					acc.SkippedBadData++
					continue
				}
				smp := sample{Occupancy: rs.Readings["occupancy"].Value, Temperature: rs.Readings["temperature"].Value,
					EnergyKWh: rs.Readings["energy"].Value}
				acc.record(simNow, smp)
				writeSample(samples, simNow, room, acc.Group, smp, rs.Readings["humidity"].Value, applied[room])
			}
			s.mu.Unlock()
			s.save()
		}
	}
}

// usable is true when every listed signal is present and its quality is ok.
func usable(rs roomState, signals ...string) bool {
	for _, sig := range signals {
		if _, ok := rs.Readings[sig]; !ok || rs.Quality[sig] != "ok" {
			return false
		}
	}
	return true
}

func writeSample(f *os.File, simNow time.Time, room, group string, smp sample, humidity float64, a appliedState) {
	if f == nil {
		return
	}
	line, _ := json.Marshal(map[string]any{"sim_time": simNow, "room": room, "group": group,
		"occupancy": smp.Occupancy, "temperature": smp.Temperature, "humidity": humidity, "energy_kwh": smp.EnergyKWh,
		"setpoint": a.Setpoint, "fan_speed": a.FanSpeed, "source": a.Source, "watchdog": a.Watchdog, "reason": a.Reason})
	_, _ = f.Write(append(line, '\n'))
}

// groupResult is the comparison the report is built on.
type groupResult struct {
	Rooms             []string `json:"rooms"`
	AreaM2            float64  `json:"area_m2"`
	ModelDays         float64  `json:"model_days"`
	OccupiedHours     float64  `json:"occupied_hours"`
	ComfortPct        float64  `json:"comfort_pct_occupied"`
	ArrivalComfortPct float64  `json:"arrival_comfort_pct"`
	EnergyKWh         float64  `json:"energy_kwh"`
	KWhPerM2PerDay    float64  `json:"kwh_per_m2_per_day"`
	SkippedBadData    int      `json:"skipped_bad_data"`
}

type check struct {
	ID       string  `json:"id"`
	What     string  `json:"what"`
	Target   string  `json:"target"`
	Measured float64 `json:"measured"`
	Pass     bool    `json:"pass"`
}

type report struct {
	Since    time.Time              `json:"since_model_time"`
	Rooms    map[string]roomAcc     `json:"rooms"`
	Groups   map[string]groupResult `json:"groups"`
	Checks   []check                `json:"requirement_checks"`
	Updated  time.Time              `json:"updated_at"`
	BandC    [2]float64             `json:"comfort_band_c"`
	ArrivalM float64                `json:"arrival_window_min"`
}

func buildReport(s *store) report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rep := report{Since: s.Since, Rooms: map[string]roomAcc{}, Groups: map[string]groupResult{}, Updated: time.Now(),
		BandC: [2]float64{comfortMinC, comfortMaxC}, ArrivalM: arrivalWindow.Minutes()}
	sums := map[string]*roomAcc{}
	for room, a := range s.ByRoom {
		rep.Rooms[room] = *a
		g := rep.Groups[a.Group]
		g.Rooms = append(g.Rooms, room)
		g.AreaM2 += a.AreaM2
		g.SkippedBadData += a.SkippedBadData
		rep.Groups[a.Group] = g
		if sums[a.Group] == nil {
			sums[a.Group] = &roomAcc{}
		}
		sum := sums[a.Group]
		sum.Hours += a.Hours
		sum.OccupiedHours += a.OccupiedHours
		sum.ComfortHours += a.ComfortHours
		sum.ArrivalHours += a.ArrivalHours
		sum.ArrivalComfort += a.ArrivalComfort
		sum.EnergyKWh += a.EnergyKWh
	}
	for group, sum := range sums {
		g := rep.Groups[group]
		rooms := float64(len(g.Rooms))
		g.ModelDays = sum.Hours / 24 / rooms
		g.OccupiedHours = sum.OccupiedHours
		g.ComfortPct = pct(sum.ComfortHours, sum.OccupiedHours)
		g.ArrivalComfortPct = pct(sum.ArrivalComfort, sum.ArrivalHours)
		g.EnergyKWh = sum.EnergyKWh
		if g.AreaM2 > 0 && g.ModelDays > 0 {
			g.KWhPerM2PerDay = sum.EnergyKWh / g.AreaM2 / g.ModelDays
		}
		rep.Groups[group] = g
	}

	adaptive, baseline := rep.Groups["adaptive"], rep.Groups["baseline"]
	saving := 0.0
	if baseline.KWhPerM2PerDay > 0 {
		saving = 100 * (1 - adaptive.KWhPerM2PerDay/baseline.KWhPerM2PerDay)
	}
	rep.Checks = []check{
		{ID: "FR-01", What: "occupied time inside 20–24 °C (adaptive)", Target: ">= 90 %",
			Measured: adaptive.ComfortPct, Pass: adaptive.ComfortPct >= 90},
		{ID: "FR-02", What: "first 30 min after arrival inside 20–24 °C (adaptive)", Target: ">= 80 %",
			Measured: adaptive.ArrivalComfortPct, Pass: adaptive.ArrivalComfortPct >= 80},
		{ID: "NFR-04", What: "energy per m² saved vs the fixed schedule", Target: ">= 15 %",
			Measured: saving, Pass: saving >= 15},
	}
	return rep
}

func pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * part / whole
}

// modelTime asks the occupancy simulator for the model clock.
func modelTime(ctx context.Context, client *http.Client, occupancyURL string) (time.Time, error) {
	var state struct {
		Sim struct {
			Clock struct {
				Time time.Time `json:"time"`
			} `json:"clock"`
		} `json:"sim"`
	}
	if err := getJSON(ctx, client, occupancyURL+"/api/state", &state); err != nil {
		return time.Time{}, err
	}
	if state.Sim.Clock.Time.IsZero() {
		return time.Time{}, fmt.Errorf("no clock in occupancy state")
	}
	return state.Sim.Clock.Time, nil
}

// roomAreas reads room sizes (m²) from the floor plan, to compare energy per m².
func roomAreas(ctx context.Context, client *http.Client, buildsimURL, level string) map[string]float64 {
	var plan struct {
		Rooms []struct {
			Name string  `json:"name"`
			Area float64 `json:"area"`
		} `json:"rooms"`
	}
	areas := map[string]float64{}
	for attempt := 0; attempt < 30; attempt++ {
		if err := getJSON(ctx, client, buildsimURL+"/api/building/floors/"+level, &plan); err == nil {
			for _, r := range plan.Rooms {
				areas[r.Name] = r.Area * 0.25 // drawing units are 0.5 m
			}
			return areas
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("evaluation: could not read room areas, energy per m² will be 0")
	return areas
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", target, resp.StatusCode)
	}
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
		if d, err := time.ParseDuration(v); err == nil {
			return d // e.g. "500ms", used by the stress test
		}
	}
	return fallback
}
