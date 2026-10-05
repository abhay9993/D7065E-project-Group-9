package server

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

// TestStressBoundedAPI is deliberately part of the normal suite. It exercises
// mixed reads and writes with enough overlap for the race detector to inspect
// the store, handlers, snapshots, and broadcast paths without slowing student
// laptops down like a soak test would.
func TestStressBoundedAPI(t *testing.T) {
	runAPIStress(t, 12, 120, time.Time{})
}

// TestStressSoakAPI is opt-in for release testing:
//
//	BUILDSIM_STRESS=1 BUILDSIM_STRESS_DURATION=30s go test -race ./pkg/server -run TestStressSoakAPI
func TestStressSoakAPI(t *testing.T) {
	if os.Getenv("BUILDSIM_STRESS") != "1" {
		t.Skip("set BUILDSIM_STRESS=1 to run the soak test")
	}
	duration := 30 * time.Second
	if configured := os.Getenv("BUILDSIM_STRESS_DURATION"); configured != "" {
		parsed, err := time.ParseDuration(configured)
		if err != nil || parsed <= 0 {
			t.Fatalf("invalid BUILDSIM_STRESS_DURATION %q", configured)
		}
		duration = parsed
	}
	workers := runtime.NumCPU() * 2
	if workers < 8 {
		workers = 8
	}
	if workers > 64 {
		workers = 64
	}
	runAPIStress(t, workers, 0, time.Now().Add(duration))
}

func runAPIStress(t *testing.T, workers, iterations int, deadline time.Time) {
	t.Helper()
	router, memory := setupTestRouter()
	items := make([]model.Equipment, workers)
	for worker := range items {
		items[worker] = model.Equipment{
			ID: "stress-equipment-" + strconv.Itoa(worker), Name: "Stress", Type: "test",
			Level: "level0", Room: "R001", Status: "running",
			Sensors: []model.Sensor{{
				ID: "stress-sensor-" + strconv.Itoa(worker), Name: "Value", Type: "temperature", DataType: "text", Value: "0",
			}},
			Actuators: []model.Actuator{{
				ID: "stress-actuator-" + strconv.Itoa(worker), Name: "Switch", Type: "switch", State: "off",
			}},
		}
	}
	if response := doRequest(router, http.MethodPost, "/api/equipment/bulk", items); response.Code != http.StatusCreated {
		t.Fatalf("stress setup: %d %s", response.Code, response.Body.String())
	}
	var session model.Session
	parseJSON(doRequest(router, http.MethodPost, "/api/sessions", nil), &session)

	var operations atomic.Int64
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; ; iteration++ {
				if iterations > 0 && iteration >= iterations {
					return
				}
				if !deadline.IsZero() && time.Now().After(deadline) {
					return
				}
				var responseCode int
				switch iteration % 8 {
				case 0:
					responseCode = doRequest(router, http.MethodPut,
						"/api/sensors/stress-sensor-"+strconv.Itoa(worker)+"/value",
						model.SensorValue{DataType: "text", Value: strconv.Itoa(iteration)}).Code
				case 1:
					responseCode = doRequest(router, http.MethodGet,
						"/api/equipment/stress-equipment-"+strconv.Itoa(worker), nil).Code
				case 2:
					state := "off"
					if iteration%2 == 0 {
						state = "on"
					}
					responseCode = doRequest(router, http.MethodPut,
						"/api/actuators/stress-actuator-"+strconv.Itoa(worker)+"/state",
						model.ActuatorState{State: state}).Code
				case 3:
					responseCode = doRequest(router, http.MethodGet, "/api/equipment?level=level0&room=R001", nil).Code
				case 4:
					responseCode = doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/viewport",
						model.Viewport{Mode: "2d", Floor: "level0", Room: "R002", Zoom: float64(iteration%5 + 1)}).Code
				case 5:
					responseCode = doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/highlights",
						[]model.RoomHighlight{{Level: "level0", Room: "R003", Color: "#ffcc00", Opacity: 0.6}}).Code
				case 6:
					responseCode = doRequest(router, http.MethodPut, "/api/sessions/"+session.ID+"/occupancy",
						map[string]model.RoomOccupancy{"level0/R002": {Persons: []model.Person{{ID: fmt.Sprintf("%d-%d", worker, iteration)}}}}).Code
				case 7:
					responseCode = doRequest(router, http.MethodPut, "/api/occupancy",
						map[string]model.RoomOccupancy{"level0/R001": {Persons: []model.Person{{ID: fmt.Sprintf("%d-%d", worker, iteration)}}}}).Code
				}
				if responseCode != http.StatusOK {
					errorsFound <- fmt.Errorf("worker %d operation %d returned HTTP %d", worker, iteration, responseCode)
					return
				}
				operations.Add(1)
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if operations.Load() == 0 {
		t.Fatal("stress test performed no operations")
	}
	if got := len(memory.ListEquipment("", "", "", "")); got != workers {
		t.Fatalf("equipment count=%d, want %d", got, workers)
	}
	for worker := 0; worker < workers; worker++ {
		if _, ok := memory.GetSensor("stress-sensor-" + strconv.Itoa(worker)); !ok {
			t.Fatalf("sensor %d disappeared", worker)
		}
		if _, ok := memory.GetActuator("stress-actuator-" + strconv.Itoa(worker)); !ok {
			t.Fatalf("actuator %d disappeared", worker)
		}
	}
}
