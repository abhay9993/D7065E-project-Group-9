package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFaultModes(t *testing.T) {
	spike := kinds["temperature"].spikeValue
	f := newFault("none", "", spike)
	if v, ok := f.apply("A1009", 21.3); !ok || v != 21.3 {
		t.Fatalf("none: got %v %v, want the honest value", v, ok)
	}

	_ = f.set("stuck", "A1009")
	if v, _ := f.apply("A1009", 25); v != 21.3 {
		t.Fatalf("stuck: got %v, want the last value before the fault (21.3)", v)
	}
	if v, _ := f.apply("A1013", 25); v != 25 {
		t.Fatalf("stuck on A1009 must not affect A1013, got %v", v)
	}

	_ = f.set("offline", "")
	if _, ok := f.apply("A1009", 21); ok {
		t.Fatalf("offline: the sensor must not report")
	}

	_ = f.set("spike", "")
	if v, _ := f.apply("A1009", 21); v != spike {
		t.Fatalf("spike: got %v, want %v", v, spike)
	}

	if err := f.set("melting", ""); err == nil {
		t.Fatalf("unknown modes must be refused")
	}
}

func TestOutboxKeepsReadingsUntilDelivered(t *testing.T) {
	o := &outbox{}
	for i := 0; i < 3; i++ {
		o.add(reading{Room: "level0/A1009", Value: float64(i), Timestamp: time.Unix(int64(i), 0)})
	}
	down := errors.New("connection refused")
	if sent, err := o.flush(func(reading) error { return down }); sent != 0 || err == nil || o.size() != 3 {
		t.Fatalf("pipeline down: sent=%d err=%v buffered=%d, want 0, error, 3", sent, err, o.size())
	}
	var got []float64
	sent, err := o.flush(func(r reading) error { got = append(got, r.Value); return nil })
	if err != nil || sent != 3 || o.size() != 0 {
		t.Fatalf("pipeline back: sent=%d err=%v buffered=%d, want 3, nil, 0", sent, err, o.size())
	}
	if got[0] != 0 || got[2] != 2 {
		t.Fatalf("delivery order = %v, want oldest first", got)
	}
}

func TestEachKindReadsItsOwnValue(t *testing.T) {
	// A fake BuildSim: one temperature sensor and the occupancy map.
	buildsim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sensors/A1009-temp":
			_, _ = w.Write([]byte(`{"value":"21.37"}`))
		case "/api/occupancy":
			_, _ = w.Write([]byte(`{"level0/A1009":{"persons":[{"id":"p1"},{"id":"p2"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer buildsim.Close()
	ctx, client := context.Background(), buildsim.Client()

	temp := readRooms(ctx, client, buildsim.URL, "level0", []string{"A1009", "A1013"}, "temperature", kinds["temperature"])
	if temp["A1009"] != 21.37 {
		t.Fatalf("temperature = %v, want 21.37 from the A1009-temp sensor", temp["A1009"])
	}
	if _, found := temp["A1013"]; found {
		t.Fatalf("a room BuildSim does not know must miss the poll, got %v", temp["A1013"])
	}

	people := readRooms(ctx, client, buildsim.URL, "level0", []string{"A1009", "A1013"}, "occupancy", kinds["occupancy"])
	if people["A1009"] != 2 || people["A1013"] != 0 {
		t.Fatalf("occupancy = %v, want A1009: 2 and A1013: 0 from the occupancy map", people)
	}
}
