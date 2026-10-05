// Command simulator generates the physical behaviour behind the sensors: it runs a
// heat- and moisture-balance model per room, closing the loop with BuildSim by
// reading back the actuator state the HVAC actuator process applies.
package main

import (
	"context"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hvac/simulator/internal/buildsim"
)

func main() {
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1010,A1013")
	listenAddr := getenv("LISTEN_ADDR", ":8094")
	tick := getenvDuration("TICK_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bs := buildsim.New(buildsimURL)
	log.Printf("simulator: waiting for buildsim at %s", buildsimURL)
	if err := bs.WaitReady(ctx); err != nil {
		log.Fatalf("simulator: buildsim never became ready: %v", err)
	}
	models := make(map[string]*model, len(rooms))
	for _, room := range rooms {
		if err := bs.SeedEquipment(ctx, level, room); err != nil {
			log.Fatalf("simulator: failed to seed equipment for %s: %v", room, err)
		}
		models[room] = &model{temperature: 21.0, humidity: 45.0}
		log.Printf("simulator: seeded hvac-%s in %s/%s", room, level, room)
	}

	go serveHealth(listenAddr)

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	dtSeconds := tick.Seconds()

	for {
		select {
		case <-ctx.Done():
			log.Println("simulator: shutting down")
			return
		case <-ticker.C:
			outdoor := outdoorTemperature(time.Now())
			for _, room := range rooms {
				stepRoom(ctx, bs, models[room], level, room, dtSeconds, outdoor)
			}
		}
	}
}

func stepRoom(ctx context.Context, bs *buildsim.Client, m *model, level, room string, dtSeconds, outdoor float64) {
	occupancy, err := bs.OccupancyCount(ctx, level, room)
	if err != nil {
		log.Printf("simulator[%s]: read occupancy: %v", room, err)
		return
	}
	setpoint, err := readFloat(ctx, bs, room+"-setpoint", 21.0)
	if err != nil {
		log.Printf("simulator[%s]: read setpoint: %v", room, err)
	}
	fanPct, err := readFloat(ctx, bs, room+"-fan", 0.0)
	if err != nil {
		log.Printf("simulator[%s]: read fan speed: %v", room, err)
	}

	m.step(dtSeconds, occupancy, setpoint, fanPct, outdoor)

	if err := bs.SetSensorValue(ctx, room+"-temp", m.temperature); err != nil {
		log.Printf("simulator[%s]: write temperature: %v", room, err)
	}
	if err := bs.SetSensorValue(ctx, room+"-humidity", m.humidity); err != nil {
		log.Printf("simulator[%s]: write humidity: %v", room, err)
	}
	log.Printf("simulator[%s]: occ=%d setpoint=%.1f fan=%.0f%% -> temp=%.2fC humidity=%.1f%%",
		room, occupancy, setpoint, fanPct, m.temperature, m.humidity)
}

// model is a simplified heat- and moisture-balance for a single room.
type model struct {
	temperature float64 // °C
	humidity    float64 // % relative humidity
}

const (
	envelopeGain = 0.01 // fraction pulled toward outdoor temperature per second
	bodyHeatGain = 0.03 // °C per person per second
	hvacGain     = 0.08 // fraction pulled toward setpoint per second at 100% fan
	humidityEnv  = 0.01 // fraction pulled toward baseline humidity per second
	humidityOcc  = 0.4  // % added per person per second
	humidityHVAC = 0.05 // fraction of humidity removed per second at 100% fan
	baseHumidity = 40.0
)

func (m *model) step(dtSeconds float64, occupancy int, setpointC, fanPct, outdoorC float64) {
	fan := clamp(fanPct/100, 0, 1)

	dTemp := envelopeGain*(outdoorC-m.temperature) +
		bodyHeatGain*float64(occupancy) +
		hvacGain*fan*(setpointC-m.temperature)
	m.temperature += dTemp * dtSeconds

	dHumidity := humidityEnv*(baseHumidity-m.humidity) +
		humidityOcc*float64(occupancy) -
		humidityHVAC*fan*m.humidity
	m.humidity = clamp(m.humidity+dHumidity*dtSeconds, 15, 90)
}

// outdoorTemperature is a simple diurnal approximation used to drive the heat-balance model.
func outdoorTemperature(t time.Time) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	return 10 + 6*math.Sin((hour-6)/24*2*math.Pi)
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

func serveHealth(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("simulator: health endpoint on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("simulator: health server stopped: %v", err)
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
	}
	return fallback
}
