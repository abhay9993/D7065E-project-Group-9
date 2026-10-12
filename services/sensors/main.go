// Command sensor emulates one kind of room sensor (temperature, humidity, energy
// meter or people counter) for every managed room. SENSOR_TYPE picks the kind,
// so the same program runs as four containers, one per kind. Each poll it reads
// the true value from BuildSim, adds the measurement noise a real device of that
// kind has, and publishes the reading to the data pipeline.
//
// Readings that cannot be delivered stay in an outbox and are re-sent in order
// once the pipeline is back (store-and-forward), so a pipeline restart does not
// lose data. POST /fault makes the sensor misbehave on purpose for fault tests.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// kind is one type of sensor and how a real device of that type behaves.
type kind struct {
	unit       string
	suffix     string  // BuildSim sensor id is <room><suffix>; empty means read the occupancy map
	noiseSigma float64 // measurement noise (standard deviation)
	scale      float64 // the device reports in steps of 1/scale
	spikeValue float64 // an impossible value, sent in "spike" mode
}

var kinds = map[string]kind{
	"temperature": {unit: "°C", suffix: "-temp", noiseSigma: 0.1, scale: 100, spikeValue: 85},    // 0.01 °C steps
	"humidity":    {unit: "%", suffix: "-humidity", noiseSigma: 0.5, scale: 10, spikeValue: 150}, // 0.1 %RH steps
	"energy":      {unit: "kWh", suffix: "-energy", noiseSigma: 0, scale: 1000, spikeValue: -5},  // a meter counts exactly, in Wh
	"occupancy":   {unit: "count", suffix: "", noiseSigma: 0, scale: 1, spikeValue: -1},          // a counter reports whole people
}

const maxBuffered = 1000 // readings kept while the pipeline is down (~20 min for 4 rooms)

type reading struct {
	Room      string    `json:"room"`
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

type sensorValue struct {
	Value string `json:"value"`
}

type roomOccupancy struct {
	Persons []struct {
		ID string `json:"id"`
	} `json:"persons"`
}

func main() {
	sensorType := getenv("SENSOR_TYPE", "temperature")
	k, ok := kinds[sensorType]
	if !ok {
		log.Fatalf("unknown SENSOR_TYPE %q (use temperature, humidity, energy or occupancy)", sensorType)
	}
	buildsimURL := getenv("BUILDSIM_URL", "http://127.0.0.1:9090")
	pipelineURL := getenv("PIPELINE_URL", "http://127.0.0.1:8090")
	level := getenv("ROOM_LEVEL", "level0")
	rooms := getenvList("ROOM_NAMES", "A1009,A1010,A1013,A1015")
	listenAddr := getenv("LISTEN_ADDR", ":8095")
	poll := getenvDuration("POLL_SECONDS", 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	faults := newFault(getenv("FAULT_MODE", "none"), "", k.spikeValue)
	out := &outbox{}
	go serve(listenAddr, sensorType, faults, out)

	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println(sensorType + "-sensor: shutting down")
			return
		case <-ticker.C:
			values := readRooms(ctx, client, buildsimURL, level, rooms, sensorType, k)
			for _, room := range rooms {
				value, found := values[room]
				if !found {
					continue
				}
				value, send := faults.apply(room, k.addNoise(value))
				if !send {
					continue // "offline": the device says nothing at all
				}
				out.add(reading{Room: level + "/" + room, Type: sensorType, Value: value, Unit: k.unit, Timestamp: time.Now().UTC()})
			}
			sent, err := out.flush(func(r reading) error { return publish(ctx, client, pipelineURL, r) })
			if err != nil {
				log.Printf("%s-sensor: pipeline unavailable, %d readings buffered: %v", sensorType, out.size(), err)
			} else if sent > len(rooms) {
				log.Printf("%s-sensor: pipeline back, delivered %d buffered readings", sensorType, sent)
			}
		}
	}
}

// addNoise models the sensor's measurement error: Gaussian noise, then the
// resolution the device reports with.
func (k kind) addNoise(v float64) float64 {
	return math.Round((v+rand.NormFloat64()*k.noiseSigma)*k.scale) / k.scale
}

// readRooms returns the true value of each room this poll. A room whose value
// could not be read is left out, so it simply misses this poll.
func readRooms(ctx context.Context, client *http.Client, baseURL, level string, rooms []string, sensorType string, k kind) map[string]float64 {
	values := map[string]float64{}
	if k.suffix == "" { // people counter: one call returns the whole building
		occupancy, err := fetchOccupancy(ctx, client, baseURL)
		if err != nil {
			log.Printf("%s-sensor: read buildsim: %v", sensorType, err)
			return values
		}
		for _, room := range rooms {
			values[room] = float64(len(occupancy[level+"/"+room].Persons))
		}
		return values
	}
	for _, room := range rooms {
		value, err := fetchSensorValue(ctx, client, baseURL, room+k.suffix)
		if err != nil {
			log.Printf("%s-sensor[%s]: read buildsim: %v", sensorType, room, err)
			continue
		}
		values[room] = value
	}
	return values
}

// fault is the failure mode injected through POST /fault, for testing how the
// rest of the system copes with a sensor that lies or goes silent.
type fault struct {
	mu    sync.Mutex
	Mode  string `json:"mode"` // none | stuck | offline | spike
	Room  string `json:"room"` // empty means every room
	spike float64
	last  map[string]float64
}

func newFault(mode, room string, spike float64) *fault {
	return &fault{Mode: mode, Room: room, spike: spike, last: map[string]float64{}}
}

func (f *fault) set(mode, room string) error {
	switch mode {
	case "none", "stuck", "offline", "spike":
	default:
		return fmt.Errorf("unknown fault mode %q", mode)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Mode, f.Room = mode, room
	return nil
}

// apply turns the honest reading into what the faulty device would report, and
// says whether it reports anything at all.
func (f *fault) apply(room string, value float64) (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Room == "" || f.Room == room {
		switch f.Mode {
		case "offline":
			return 0, false
		case "stuck":
			if v, ok := f.last[room]; ok {
				return v, true // frozen at a plausible value
			}
		case "spike":
			value = f.spike
		}
	}
	f.last[room] = value
	return value, true
}

// outbox keeps readings in order until the pipeline accepts them.
type outbox struct {
	mu       sync.Mutex
	readings []reading
	dropped  int
}

func (o *outbox) add(r reading) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.readings = append(o.readings, r)
	if len(o.readings) > maxBuffered {
		o.readings = o.readings[1:] // oldest goes first when the buffer is full
		o.dropped++
	}
}

// flush sends the oldest reading first and stops at the first failure, so the
// pipeline always receives each room's readings in time order.
func (o *outbox) flush(send func(reading) error) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	sent := 0
	for len(o.readings) > 0 {
		if err := send(o.readings[0]); err != nil {
			return sent, err
		}
		o.readings = o.readings[1:]
		sent++
	}
	return sent, nil
}

func (o *outbox) size() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.readings)
}

func fetchSensorValue(ctx context.Context, client *http.Client, baseURL, sensorID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/sensors/"+sensorID, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	var s sensorValue
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(s.Value, 64)
}

func fetchOccupancy(ctx context.Context, client *http.Client, baseURL string) (map[string]roomOccupancy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/occupancy", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("buildsim returned HTTP %d", resp.StatusCode)
	}
	var occ map[string]roomOccupancy
	if err := json.NewDecoder(resp.Body).Decode(&occ); err != nil {
		return nil, err
	}
	return occ, nil
}

// publish delivers one reading. A duplicate (409) was already stored, and a
// rejected reading (422) will never be accepted, so both count as delivered;
// only a network error or a 5xx keeps the reading in the outbox.
func publish(ctx context.Context, client *http.Client, pipelineURL string, r reading) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pipelineURL+"/api/readings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnprocessableEntity:
		log.Printf("%s-sensor[%s]: pipeline rejected value %.2f", r.Type, r.Room, r.Value)
		return nil
	case resp.StatusCode == http.StatusConflict, resp.StatusCode < 300:
		return nil
	}
	return fmt.Errorf("pipeline returned HTTP %d", resp.StatusCode)
}

func serve(addr, sensorType string, faults *fault, out *outbox) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /fault", func(w http.ResponseWriter, r *http.Request) {
		faults.mu.Lock()
		state := map[string]any{"mode": faults.Mode, "room": faults.Room}
		faults.mu.Unlock()
		out.mu.Lock()
		state["buffered"], state["dropped"] = len(out.readings), out.dropped
		out.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})
	mux.HandleFunc("POST /fault", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Mode, Room string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "invalid fault: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := faults.set(in.Mode, in.Room); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("%s-sensor: fault mode now %q (room %q)", sensorType, in.Mode, in.Room)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("%s-sensor: health server stopped: %v", sensorType, err)
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
