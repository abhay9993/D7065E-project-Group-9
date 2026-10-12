// Command simulator generates the physical behaviour behind the sensors: it runs a
// heat- and moisture-balance model per room, closing the loop with BuildSim by
// reading back the actuator state the HVAC actuator process applies.
//
// Time: the occupancy simulator owns the model clock (it runs FACTOR times
// faster than real time, 60x by default). The physics is stepped by how much
// *simulated* time passed since the last tick, so a person's one-hour meeting
// and the room's one-hour warm-up happen on the same clock.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hvac/simulator/internal/buildsim"
)

// maxJump caps how much simulated time one tick may cover. If someone jumps the
// occupancy clock forward by a day we do not know who was where in between, so
// we only simulate the last two hours of it.
const maxJump = 2 * time.Hour

func main() {
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	occupancyURL := getenv("OCCUPANCY_URL", "http://127.0.0.1:8081")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1010,A1013,A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8094")
	tick := getenvDuration("TICK_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bs := buildsim.New(buildsimURL)
	log.Printf("simulator: waiting for buildsim at %s", buildsimURL)
	if err := bs.WaitReady(ctx); err != nil {
		log.Fatalf("simulator: buildsim never became ready: %v", err)
	}
	areas, err := bs.RoomAreas(ctx, level)
	if err != nil {
		log.Fatalf("simulator: read floor plan: %v", err)
	}
	models := make(map[string]*model, len(rooms))
	for _, room := range rooms {
		area, ok := areas[room]
		if !ok || area <= 0 {
			log.Fatalf("simulator: room %s not found on %s", room, level)
		}
		if err := bs.SeedEquipment(ctx, level, room); err != nil {
			log.Fatalf("simulator: failed to seed equipment for %s: %v", room, err)
		}
		m := newModel(area)
		m.restore(ctx, bs, room)
		models[room] = m
		log.Printf("simulator: seeded hvac-%s in %s/%s (%.1f m²) starting at %.2fC %.1f%% %.3fkWh",
			room, level, room, area, m.temperature, m.humidity, m.energyKWh)
	}

	st := &status{Rooms: map[string]roomStatus{}}
	go serveStatus(listenAddr, st)

	clock := &simClock{url: occupancyURL, client: &http.Client{Timeout: 3 * time.Second}}
	lastSim := clock.now(ctx)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("simulator: shutting down")
			return
		case <-ticker.C:
			started := time.Now()
			simNow := clock.now(ctx)
			dt := simNow.Sub(lastSim)
			lastSim = simNow
			if dt < 0 {
				log.Printf("simulator: model clock went backwards by %s (day replayed?), not stepping", -dt)
				dt = 0
			}
			if dt > maxJump {
				log.Printf("simulator: model clock jumped %s, simulating the last %s only", dt, maxJump)
				dt = maxJump
			}
			occupancy, err := bs.Occupancy(ctx)
			if err != nil {
				log.Printf("simulator: read occupancy: %v", err)
				continue
			}
			outdoor := outdoorTemperature(simNow)
			for _, room := range rooms {
				stepRoom(ctx, bs, models[room], level, room, occupancy[level+"/"+room], dt.Seconds(), outdoor)
				st.setRoom(room, models[room], occupancy[level+"/"+room])
			}
			st.setTick(simNow, outdoor, time.Since(started))
		}
	}
}

func stepRoom(ctx context.Context, bs *buildsim.Client, m *model, level, room string, occupancy int, dtSeconds, outdoor float64) {
	setpoint, err := readFloat(ctx, bs, room+"-setpoint", 21.0)
	if err != nil {
		log.Printf("simulator[%s]: read setpoint: %v", room, err)
	}
	fanPct, err := readFloat(ctx, bs, room+"-fan", 50.0)
	if err != nil {
		log.Printf("simulator[%s]: read fan speed: %v", room, err)
	}

	m.step(dtSeconds, occupancy, setpoint, fanPct, outdoor)

	err = errors.Join(
		bs.SetSensorValue(ctx, room+"-temp", m.temperature),
		bs.SetSensorValue(ctx, room+"-humidity", m.humidity),
		bs.SetSensorValue(ctx, room+"-energy", m.energyKWh),
	)
	if errors.Is(err, buildsim.ErrNotFound) {
		// BuildSim keeps its state in memory: after a restart our equipment is
		// gone. Register it again; the next tick writes the values. (Rule from
		// the brief: a process re-registers with BuildSim and continues.)
		log.Printf("simulator[%s]: buildsim lost our equipment, re-registering", room)
		if err := bs.SeedEquipment(ctx, level, room); err != nil {
			log.Printf("simulator[%s]: re-register failed: %v", room, err)
			return
		}
		// Overwrite the seed defaults straight away, so no sensor reads a
		// meter that jumped back to 0 kWh.
		err = errors.Join(
			bs.SetSensorValue(ctx, room+"-temp", m.temperature),
			bs.SetSensorValue(ctx, room+"-humidity", m.humidity),
			bs.SetSensorValue(ctx, room+"-energy", m.energyKWh),
		)
	}
	if err != nil {
		log.Printf("simulator[%s]: write sensors: %v", room, err)
	}
	log.Printf("simulator[%s]: occ=%d setpoint=%.1f fan=%.0f%% out=%.1fC -> temp=%.2fC humidity=%.1f%% hvac=%.0fW energy=%.3fkWh",
		room, occupancy, setpoint, fanPct, outdoor, m.temperature, m.humidity, m.hvacPowerW, m.energyKWh)
}

// model is a simplified heat- and moisture-balance for a single room, in SI
// units so every constant can be checked against a real building. All
// per-area constants scale with the room's floor area from the floor plan.
type model struct {
	areaM2      float64
	temperature float64 // °C
	humidity    float64 // % relative humidity
	energyKWh   float64 // cumulative HVAC energy: heating/cooling delivered + fan
	hvacPowerW  float64 // last heating power from the room unit
}

const (
	roomHeightM     = 2.7
	heatCapacityJK  = 30000.0 // J/K per m² floor: air, furniture, inner surfaces
	envelopeWK      = 2.5     // W/K per m² floor: walls, window, infiltration
	bodyHeatW       = 75.0    // sensible heat per person doing office work (ASHRAE)
	hvacMaxW        = 100.0   // W per m² floor the room unit can heat with
	thermostatGainK = 2.0     // 1/K: the unit's own thermostat is at full output 0.5 K below setpoint
	fanPowerW       = 3.5     // W per m² floor of fan electricity at 100 % fan (SFP 1.5 kW per m³/s)
	airHeatJM3K     = 1200.0  // J/(m³·K): heat carried by one m³ of air per kelvin
	heatRecovery    = 0.7     // the ventilation unit's heat exchanger recovers 70 % (Swedish FTX)
	outdoorRH       = 30.0    // %RH of cold outdoor air once it is warmed to room temperature
	infiltrationACH = 0.5     // air changes per hour with the fan off (its heat loss is part of envelopeWK)
	ventilationACH  = 3.0     // extra fresh-air changes per hour at 100 % fan
	moistureGPerH   = 50.0    // water vapour breathed out per person
	gPerM3PerRH     = 0.194   // g/m³ of water vapour for 1 %RH at about 22 °C
	maxSubstepS     = 30.0    // explicit Euler step, see step()
)

func newModel(areaM2 float64) *model {
	return &model{areaM2: areaM2, temperature: 21.0, humidity: 35.0}
}

// restore continues from the values already in BuildSim, so restarting the
// simulator does not make every room jump back to 21 °C.
func (m *model) restore(ctx context.Context, bs *buildsim.Client, room string) {
	if v, err := bs.SensorValue(ctx, room+"-temp"); err == nil {
		m.temperature = v
	}
	if v, err := bs.SensorValue(ctx, room+"-humidity"); err == nil {
		m.humidity = v
	}
	if v, err := bs.SensorValue(ctx, room+"-energy"); err == nil {
		m.energyKWh = v
	}
}

// step advances the room by dtSeconds of simulated time.
//
//	C·dT/dt = UA·(T_out − T) + (1 − η)·ρc·V·ACH_fan·(T_out − T) + 75 W·people + P_unit
//	dRH/dt  = ACH·(RH_out − RH) + moisture·people
//
// The fan is the *ventilation*: it brings in fresh outdoor air, which removes
// moisture and also heat (minus what the heat exchanger recovers). Heating and
// cooling come from the room unit, whose own thermostat follows the setpoint.
//
// One tick is 5 s real = 300 s simulated at 60x. Explicit Euler is only stable
// while k·dt < 2, so the tick is split into
// sub-steps of at most 30 s, which keeps k·dt around 0.2.
func (m *model) step(dtSeconds float64, occupancy int, setpointC, fanPct, outdoorC float64) {
	if dtSeconds <= 0 {
		return
	}
	fan := clamp(fanPct/100, 0, 1)
	steps := int(math.Ceil(dtSeconds / maxSubstepS))
	h := dtSeconds / float64(steps)
	capacity := heatCapacityJK * m.areaM2
	volume := roomHeightM * m.areaM2
	people := float64(max(occupancy, 0))

	for i := 0; i < steps; i++ {
		unit := unitPower(m.temperature, setpointC, m.areaM2)
		freshAir := ventilationACH * fan * volume / 3600 // m³/s brought in by the fan
		heat := envelopeWK*m.areaM2*(outdoorC-m.temperature) +
			(1-heatRecovery)*airHeatJM3K*freshAir*(outdoorC-m.temperature) +
			bodyHeatW*people + unit
		m.temperature += heat / capacity * h

		airChanges := (infiltrationACH + ventilationACH*fan) / 3600        // per second
		moisture := moistureGPerH / 3600 / (gPerM3PerRH * volume) * people // %RH per second
		m.humidity += (airChanges*(outdoorRH-m.humidity) + moisture) * h

		m.energyKWh += (unit + fanPowerW*m.areaM2*fan) * h / 3.6e6
		m.hvacPowerW = unit
	}
	m.humidity = clamp(m.humidity, 0, 100)
}

// unitPower is the room unit's own thermostat: it heats below the setpoint, with
// output proportional to the error up to its capacity. The controller sets the
// target (and the ventilation); the unit does the fast regulation - the two-loop
// structure of a real building. There is no cooling: an office in Luleå in
// October never needs it, the room cools down by itself.
func unitPower(tempC, setpointC, areaM2 float64) float64 {
	if tempC >= setpointC {
		return 0
	}
	return hvacMaxW * areaM2 * math.Min(thermostatGainK*(setpointC-tempC), 1)
}

// outdoorTemperature is a diurnal curve for Luleå in October on the model
// clock: about 1 °C at 03:00 and 9 °C at 15:00.
func outdoorTemperature(t time.Time) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	return 5 + 4*math.Sin((hour-9)/24*2*math.Pi)
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

func readFloat(ctx context.Context, bs *buildsim.Client, actuatorID string, fallback float64) (float64, error) {
	state, err := bs.ActuatorState(ctx, actuatorID)
	if err != nil {
		return fallback, err
	}
	v, err := strconv.ParseFloat(state, 64)
	if err != nil {
		return fallback, err
	}
	return v, nil
}

// simClock follows the occupancy simulator's clock, the single model clock of
// the whole system. If the occupancy simulator is unreachable we extrapolate
// from the last reading with the last known speed factor.
type simClock struct {
	url     string
	client  *http.Client
	sim     time.Time // last simulated time we read
	real    time.Time // wall-clock time when we read it
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
	log.Printf("simulator: model clock unavailable (%v), extrapolating", err)
	if c.sim.IsZero() {
		return time.Now()
	}
	if !c.running {
		return c.sim
	}
	return c.sim.Add(time.Duration(float64(time.Since(c.real)) * c.factor))
}

// status is what GET /api/status reports: the model clock, the last tick's
// duration (used by the stress test), and every room's physical state.
type status struct {
	mu         sync.RWMutex
	SimTime    time.Time             `json:"sim_time"`
	OutdoorC   float64               `json:"outdoor_c"`
	LastTickMs float64               `json:"last_tick_ms"`
	Rooms      map[string]roomStatus `json:"rooms"`
}

type roomStatus struct {
	Occupancy   int     `json:"occupancy"`
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	HVACPowerW  float64 `json:"hvac_power_w"`
	EnergyKWh   float64 `json:"energy_kwh"`
}

func (s *status) setTick(simNow time.Time, outdoor float64, took time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SimTime, s.OutdoorC, s.LastTickMs = simNow, outdoor, float64(took.Microseconds())/1000
}

func (s *status) setRoom(room string, m *model, occupancy int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rooms[room] = roomStatus{Occupancy: occupancy, Temperature: m.temperature, Humidity: m.humidity,
		HVACPowerW: m.hvacPowerW, EnergyKWh: m.energyKWh}
}

func serveStatus(addr string, st *status) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		defer st.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("simulator: status endpoint on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("simulator: status server stopped: %v", err)
	}
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
