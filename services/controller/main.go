// Command controller is the autonomous decision-maker: every DECIDE_SECONDS it
// reads each of its rooms from the pipeline, decides the HVAC setpoint and fan
// speed, and asks the independently deployable actuator process to apply them.
//
// It combines three techniques, each for a reason we measured:
//
//  1. Rules with hysteresis (occupied / setback / pre-heat / failsafe). Simple
//     and verifiable; the 30-minute grace period stops the room flipping to
//     setback every time someone goes for coffee.
//  2. A learned occupancy prior: P(room occupied | weekday or weekend, half hour
//     of the model day), counted from the room's own history. If people usually
//     arrive within the next hour, the room is pre-heated so it is comfortable
//     when they walk in. (A linear trend, which we tried first, can only react
//     to people *leaving*, never predict an arrival.)
//  3. Data-quality gating: a stale, stuck or missing reading forces a safe
//     command instead of a decision made on bad data.
//
// All times in the decision logic are on the model clock owned by the
// occupancy simulator (60x real time by default).
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

type command struct {
	Room     string  `json:"room"`
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Reason   string  `json:"reason"`
	Source   string  `json:"source"`
}

// decision is what decide() returns for one room.
type decision struct {
	Setpoint float64 `json:"setpoint"`
	FanSpeed float64 `json:"fan_speed"`
	Mode     string  `json:"mode"` // occupied | setback | preheat | failsafe
	Reason   string  `json:"reason"`
}

// Comfort targets (EN 16798-1 category II for offices in the heating season is
// 20–24 °C; we aim for the middle). The setpoint goes to the room unit's own
// thermostat; the fan is the fresh-air ventilation, which people need but which
// costs heat (cold outdoor air), so it follows occupancy.
const (
	occupiedSetpoint = 22.0
	setbackSetpoint  = 18.0
	failsafeSetpoint = 21.0
	occupiedFan      = 50.0 // ~1.5 air changes/h, well above 7 l/s per person (AFS 2020:1)
	emptyFan         = 10.0 // minimum ventilation of an empty office
	failsafeFan      = 50.0 // blind: ventilate as if someone were in the room
	humidFan         = 80.0 // ventilate harder when the air gets humid
	humidityLimit    = 60.0 // %RH, above this mould and discomfort start

	emptyGrace = 30 * time.Minute // hysteresis: hold "occupied" this long after the room empties
)

func main() {
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	actuatorURL := getenv("ACTUATOR_URL", "http://127.0.0.1:8091")
	occupancyURL := getenv("OCCUPANCY_URL", "http://127.0.0.1:8081")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1013")
	listenAddr := getenv("LISTEN_ADDR", ":8092")
	dataDir := getenv("DATA_DIR", "/data")
	decideEvery := getenvDuration("DECIDE_SECONDS", 10*time.Second)
	threshold := getenvFloat("PREHEAT_THRESHOLD", 0.5)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	prior := loadPrior(filepath.Join(dataDir, "prior.json"))
	audit, err := os.OpenFile(filepath.Join(dataDir, "decisions.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("controller: no audit log (%v), decisions are only logged to stdout", err)
	}

	st := newStatus()
	go serveStatus(listenAddr, st, prior)

	client := &http.Client{Timeout: 5 * time.Second}
	clock := &simClock{url: occupancyURL, client: client}
	lastOccupied := map[string]time.Time{}
	lastMode := map[string]string{}

	ticker := time.NewTicker(decideEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("controller: shutting down")
			return
		case <-ticker.C:
			started := time.Now()
			simNow := clock.now(ctx)
			for _, room := range rooms {
				roomKey := level + "/" + room
				var d decision
				state, err := fetchState(ctx, client, pipelineURL, roomKey)
				if err != nil {
					d = failsafe("pipeline unreachable: " + err.Error())
				} else {
					occ, occOK := state.Readings["occupancy"]
					if occOK && state.Quality["occupancy"] == "ok" {
						occupied := occ.Value > 0
						prior.observe(room, simNow, occupied)
						if occupied {
							st.arrival(room, lastMode[room])
							lastOccupied[room] = simNow
						}
					}
					d = decide(state, simNow, lastOccupied[room], prior.ahead(room, simNow), threshold)
				}
				st.preheatEnded(room, lastMode[room], d.Mode)
				lastMode[room] = d.Mode

				cmd := command{Room: room, Setpoint: d.Setpoint, FanSpeed: d.FanSpeed, Reason: d.Reason, Source: "controller"}
				sendErr := sendCommand(ctx, client, actuatorURL, cmd)
				if sendErr != nil {
					log.Printf("controller[%s]: send command: %v", roomKey, sendErr)
				}
				st.set(room, d, simNow, prior.ahead(room, simNow), sendErr == nil)
				writeAudit(audit, room, simNow, d, sendErr)
				log.Printf("controller: room=%s mode=%s setpoint=%.1f fan=%.0f%% reason=%q",
					roomKey, d.Mode, d.Setpoint, d.FanSpeed, d.Reason)
			}
			prior.save()
			st.cycle(time.Since(started))
		}
	}
}

// decide is the whole policy for one room, as a pure function so every branch
// can be unit tested. pAhead is the prior's probability that the room will be
// occupied within the next hour (-1 when the prior has not learned enough yet).
func decide(state roomState, simNow, lastOccupied time.Time, pAhead, threshold float64) decision {
	// 3. Data quality first: never decide on a reading we cannot trust.
	for _, signal := range []string{"temperature", "occupancy"} {
		if _, ok := state.Readings[signal]; !ok {
			return failsafe("no " + signal + " reading")
		}
		if q := state.Quality[signal]; q != "ok" {
			return failsafe(fmt.Sprintf("%s sensor is %s", signal, q))
		}
	}
	temp := state.Readings["temperature"].Value
	occ := state.Readings["occupancy"].Value

	// 1. Rules with hysteresis, 2. the prior for pre-heating.
	var d decision
	emptyFor := simNow.Sub(lastOccupied)
	switch {
	case occ > 0:
		d = occupied(fmt.Sprintf("%.0f occupant(s) at %.1f°C, holding %.0f°C", occ, temp, occupiedSetpoint))
	case !lastOccupied.IsZero() && emptyFor >= 0 && emptyFor < emptyGrace: // < 0: the model clock was set back
		d = occupied(fmt.Sprintf("empty for %.0f min, holding comfort for %.0f min", emptyFor.Minutes(), emptyGrace.Minutes()))
	case pAhead >= threshold:
		// Heat with the room unit only: nobody is there yet, so no fresh air needed.
		d = decision{Setpoint: occupiedSetpoint, FanSpeed: emptyFan, Mode: "preheat",
			Reason: fmt.Sprintf("usually occupied soon (p=%.2f), pre-heating at %.1f°C", pAhead, temp)}
	default:
		d = decision{Setpoint: setbackSetpoint, FanSpeed: emptyFan, Mode: "setback",
			Reason: fmt.Sprintf("empty, not expected within the hour (p=%.2f): setback", pAhead)}
	}

	// Humidity is a comfort/health guard, not safety critical: if its sensor is
	// unreliable we simply skip this rule instead of failing safe.
	if h, ok := state.Readings["humidity"]; ok && state.Quality["humidity"] == "ok" && h.Value > humidityLimit {
		d.FanSpeed = max(d.FanSpeed, humidFan)
		d.Reason += fmt.Sprintf("; humidity %.0f%%, ventilating", h.Value)
	}
	return d
}

// occupied keeps the room at the comfort setpoint and ventilates for people.
func occupied(reason string) decision {
	return decision{Setpoint: occupiedSetpoint, FanSpeed: occupiedFan, Mode: "occupied", Reason: reason}
}

func failsafe(why string) decision {
	return decision{Setpoint: failsafeSetpoint, FanSpeed: failsafeFan, Mode: "failsafe", Reason: why + ", failing safe"}
}

// prior counts, per room and per half hour of the model week (weekday or
// weekend), how often the room was occupied when the controller looked.
type prior struct {
	mu     sync.Mutex
	path   string
	Counts map[string]map[string]*slotCount `json:"counts"` // room -> "weekday|09:30" -> counts
}

type slotCount struct {
	Seen     int `json:"seen"`
	Occupied int `json:"occupied"`
}

const (
	slotLength      = 30 * time.Minute
	lookahead       = time.Hour
	minSlotSamples  = 6 // ~2 model days at one decision per 10 model minutes
	priorDayWeekday = "weekday"
	priorDayWeekend = "weekend"
)

func slotKey(t time.Time) string {
	day := priorDayWeekday
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		day = priorDayWeekend
	}
	return day + "|" + t.Truncate(slotLength).Format("15:04")
}

func loadPrior(path string) *prior {
	p := &prior{path: path, Counts: map[string]map[string]*slotCount{}}
	data, err := os.ReadFile(path)
	if err == nil && json.Unmarshal(data, p) == nil {
		log.Printf("controller: loaded occupancy prior for %d rooms from %s", len(p.Counts), path)
	}
	return p
}

func (p *prior) observe(room string, t time.Time, occupied bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Counts[room] == nil {
		p.Counts[room] = map[string]*slotCount{}
	}
	key := slotKey(t)
	if p.Counts[room][key] == nil {
		p.Counts[room][key] = &slotCount{}
	}
	p.Counts[room][key].Seen++
	if occupied {
		p.Counts[room][key].Occupied++
	}
}

// ahead returns the highest learned probability of the room being occupied in
// the half-hour slots within the next hour, or -1 if nothing is learned yet.
func (p *prior) ahead(room string, t time.Time) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	best := -1.0
	for step := slotLength; step <= lookahead; step += slotLength {
		c := p.Counts[room][slotKey(t.Add(step))]
		if c == nil || c.Seen < minSlotSamples {
			continue
		}
		best = max(best, float64(c.Occupied)/float64(c.Seen))
	}
	return best
}

// save writes the prior to a temporary file and renames it, so a crash in the
// middle of writing never leaves a half-written prior behind.
func (p *prior) save() {
	p.mu.Lock()
	data, err := json.Marshal(p)
	p.mu.Unlock()
	if err != nil {
		return
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("controller: save prior: %v", err)
		return
	}
	_ = os.Rename(tmp, p.path)
}

func writeAudit(f *os.File, room string, simNow time.Time, d decision, sendErr error) {
	if f == nil {
		return
	}
	entry := map[string]any{"at": time.Now().UTC(), "sim_time": simNow, "room": room, "mode": d.Mode,
		"setpoint": d.Setpoint, "fan_speed": d.FanSpeed, "reason": d.Reason, "applied": sendErr == nil}
	line, _ := json.Marshal(entry)
	_, _ = f.Write(append(line, '\n'))
}

// status is what GET /api/status shows: the last decision per room, how long a
// decision cycle took (the stress test reads this), and how well the prior
// predicted arrivals.
type status struct {
	mu          sync.RWMutex
	byRoom      map[string]roomStatus
	lastCycleMs float64
	forecast    map[string]*forecastStats
}

type roomStatus struct {
	LastCmd   decision  `json:"last_command"`
	Failsafe  bool      `json:"failsafe"`
	Applied   bool      `json:"applied"`
	PAhead    float64   `json:"p_occupied_next_hour"`
	SimTime   time.Time `json:"sim_time"`
	DecidedAt time.Time `json:"decided_at"`
}

// forecastStats evaluates the prior against what actually happened: an arrival
// is "pre-heated" if the room was in pre-heat mode when the person walked in,
// and a pre-heat that ends without anyone arriving is a false alarm.
type forecastStats struct {
	Arrivals      int `json:"arrivals"`
	Preheated     int `json:"preheated_arrivals"`
	FalsePreheats int `json:"false_preheats"`
	PreheatCycles int `json:"preheat_cycles"`
	inPreheat     bool
}

func newStatus() *status {
	return &status{byRoom: map[string]roomStatus{}, forecast: map[string]*forecastStats{}}
}

func (s *status) stats(room string) *forecastStats {
	if s.forecast[room] == nil {
		s.forecast[room] = &forecastStats{}
	}
	return s.forecast[room]
}

// arrival is called every cycle the room is occupied. It is a real arrival only
// if the room was in setback or pre-heat (empty for longer than the grace
// period); in "occupied" mode it is the same visit or a short break.
func (s *status) arrival(room, previousMode string) {
	if previousMode != "setback" && previousMode != "preheat" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.stats(room)
	f.Arrivals++
	if previousMode == "preheat" {
		f.Preheated++
		f.inPreheat = false // the pre-heat paid off
	}
}

func (s *status) preheatEnded(room, previousMode, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.stats(room)
	if mode == "preheat" {
		f.PreheatCycles++
		f.inPreheat = true
	}
	if previousMode == "preheat" && mode == "setback" && f.inPreheat {
		f.FalsePreheats++
		f.inPreheat = false
	}
}

func (s *status) set(room string, d decision, simNow time.Time, pAhead float64, applied bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byRoom[room] = roomStatus{LastCmd: d, Failsafe: d.Mode == "failsafe", Applied: applied,
		PAhead: pAhead, SimTime: simNow, DecidedAt: time.Now()}
}

func (s *status) cycle(took time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCycleMs = float64(took.Microseconds()) / 1000
}

func fetchState(ctx context.Context, client *http.Client, pipelineURL, roomKey string) (roomState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		pipelineURL+"/api/rooms/state?room="+url.QueryEscape(roomKey), nil)
	if err != nil {
		return roomState{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return roomState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return roomState{}, fmt.Errorf("pipeline returned HTTP %d", resp.StatusCode)
	}
	var s roomState
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return roomState{}, err
	}
	return s, nil
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

func serveStatus(addr string, st *status, p *prior) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		defer st.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": st.byRoom, "last_cycle_ms": st.lastCycleMs, "forecast": st.forecast})
	})
	mux.HandleFunc("GET /api/prior", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.Counts)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("controller: status endpoint on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("controller: status server stopped: %v", err)
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
	log.Printf("controller: model clock unavailable (%v), extrapolating", err)
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

func getenvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
