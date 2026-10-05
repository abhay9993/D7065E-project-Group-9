package buildsim_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eislab-cps/D7065E/occupancysim/internal/buildsim"
	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

// fakeBuildSim serves a two-floor building from one drawing and validates
// published snapshots the way BuildSim does: every entity must name a level of
// the building, every room key must exist on its floor, and every position must
// be inside the page.
func fakeBuildSim(t *testing.T, floor []byte, fail *atomic.Bool, writes ...*atomic.Int64) *httptest.Server {
	entityWrites, occupancyWrites := &atomic.Int64{}, &atomic.Int64{}
	if len(writes) == 2 {
		entityWrites, occupancyWrites = writes[0], writes[1]
	}
	t.Helper()
	plan, err := buildsim.ParseFloor(floor, "level0")
	if err != nil {
		t.Fatal(err)
	}
	rooms := map[string]bool{}
	for _, room := range plan.Rooms {
		rooms[room.Name] = true
	}
	levels := map[string]bool{"level0": true, "level1": true}
	inside := func(p [2]float64) bool {
		return p[0] >= 0 && p[1] >= 0 && p[0] <= plan.Page[0] && p[1] <= plan.Page[1]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/building", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"fake","levels":[{"id":"level0"},{"id":"level1"}]}`))
	})
	mux.HandleFunc("GET /api/building/floors/{level}", func(w http.ResponseWriter, r *http.Request) {
		if !levels[r.PathValue("level")] {
			http.Error(w, "no such floor", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(floor)
	})
	mux.HandleFunc("PUT /api/entities", func(w http.ResponseWriter, r *http.Request) {
		entityWrites.Add(1)
		if fail.Load() {
			http.Error(w, `{"error":"injected failure"}`, http.StatusInternalServerError)
			return
		}
		var entities []sim.Entity
		if err := json.NewDecoder(r.Body).Decode(&entities); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, e := range entities {
			if !levels[e.Level] || !inside(e.Position) || (e.Room != "" && !rooms[e.Room]) {
				http.Error(w, "invalid entity "+e.ID, http.StatusBadRequest)
				return
			}
		}
		_, _ = w.Write([]byte(`{"status":"updated"}`))
	})
	mux.HandleFunc("PUT /api/occupancy", func(w http.ResponseWriter, r *http.Request) {
		occupancyWrites.Add(1)
		var occupancy map[string]sim.RoomOccupancy
		if err := json.NewDecoder(r.Body).Decode(&occupancy); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for key := range occupancy {
			level, room, ok := strings.Cut(key, "/")
			if !ok || !levels[level] || !rooms[room] {
				http.Error(w, "invalid room key "+key, http.StatusBadRequest)
				return
			}
		}
		_, _ = w.Write([]byte(`{"status":"updated"}`))
	})
	return httptest.NewServer(mux)
}

func TestPublisherAgainstFakeBuildSim(t *testing.T) {
	floor, err := os.ReadFile("../../testdata/level0.json")
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	server := fakeBuildSim(t, floor, &fail)
	defer server.Close()

	client := buildsim.New(server.URL)
	plans, err := allFloors(client)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("fake building has %d floors, want 2", len(plans))
	}
	simulation, err := sim.New(plans, sim.DefaultParameters(), time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	simulation.Advance(5 * time.Minute)

	publisher := buildsim.NewPublisher(client, simulation, server.URL)
	publisher.PublishOnce(context.Background())
	status := publisher.Status()
	if !status.Connected || status.Entities == 0 || status.Rooms == 0 {
		t.Fatalf("publish failed: %+v", status)
	}

	fail.Store(true)
	publisher.PublishOnce(context.Background())
	if status := publisher.Status(); status.Connected || status.LastError == "" {
		t.Fatalf("failure was not reported: %+v", status)
	}
	fail.Store(false)
	publisher.PublishOnce(context.Background())
	if status := publisher.Status(); !status.Connected {
		t.Fatalf("did not recover: %+v", status)
	}
}

// allFloors loads every floor of the building the client points at.
func allFloors(client *buildsim.Client) ([]sim.FloorPlan, error) {
	levels, err := client.Levels(context.Background())
	if err != nil {
		return nil, err
	}
	var plans []sim.FloorPlan
	for _, level := range levels {
		plan, err := client.Floor(context.Background(), level)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// TestAgainstLiveBuildSim publishes to a real server when BUILDSIM_URL is set.
func TestAgainstLiveBuildSim(t *testing.T) {
	url := os.Getenv("BUILDSIM_URL")
	if url == "" {
		t.Skip("BUILDSIM_URL not set")
	}
	client := buildsim.New(url)
	plans, err := allFloors(client)
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.New(plans, sim.DefaultParameters(), time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	publisher := buildsim.NewPublisher(client, simulation, url)
	publisher.PublishOnce(context.Background())
	if status := publisher.Status(); !status.Connected {
		t.Fatalf("live publish failed: %+v", status)
	}
	_ = client.PutEntities(context.Background(), nil)
	_ = client.PutOccupancy(context.Background(), nil)
}

// TestOccupancyIsWrittenOnlyWhenItChanges covers the throttle: entities are
// written on every publish because they move, room occupancy only when it says
// something new.
func TestOccupancyIsWrittenOnlyWhenItChanges(t *testing.T) {
	floor, err := os.ReadFile("../../testdata/level0.json")
	if err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	var entityWrites, occupancyWrites atomic.Int64
	server := fakeBuildSim(t, floor, &fail, &entityWrites, &occupancyWrites)
	defer server.Close()

	client := buildsim.New(server.URL)
	plans, err := allFloors(client)
	if err != nil {
		t.Fatal(err)
	}
	params := sim.DefaultParameters()
	params.OccupancyIntervalMs = 200
	simulation, err := sim.New(plans, params, time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	publisher := buildsim.NewPublisher(client, simulation, server.URL)

	publisher.PublishOnce(context.Background()) // the first publish always writes both
	if entityWrites.Load() != 1 || occupancyWrites.Load() != 1 {
		t.Fatalf("first publish wrote %d entity and %d occupancy documents", entityWrites.Load(), occupancyWrites.Load())
	}
	publisher.PublishOnce(context.Background()) // too soon, and nothing moved
	if entityWrites.Load() != 2 || occupancyWrites.Load() != 1 {
		t.Fatalf("second publish wrote %d entity and %d occupancy documents", entityWrites.Load(), occupancyWrites.Load())
	}
	time.Sleep(250 * time.Millisecond) // past the occupancy interval
	publisher.PublishOnce(context.Background())
	if occupancyWrites.Load() != 1 {
		t.Fatalf("unchanged occupancy was written again (%d writes)", occupancyWrites.Load())
	}
	simulation.Advance(20 * time.Minute) // people have moved between rooms
	publisher.PublishOnce(context.Background())
	if occupancyWrites.Load() != 2 {
		t.Fatalf("changed occupancy was not written (%d writes)", occupancyWrites.Load())
	}
	if status := publisher.Status(); !status.Connected || status.LastOccupancy.IsZero() {
		t.Fatalf("status does not report the occupancy write: %+v", status)
	}
}
