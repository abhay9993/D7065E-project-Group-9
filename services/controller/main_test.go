package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T, state roomState) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDecideOccupiedNow(t *testing.T) {
	srv := testServer(t, roomState{
		Readings: map[string]reading{"temperature": {Value: 20.0}, "occupancy": {Value: 1}},
		Stale:    map[string]bool{"temperature": false, "occupancy": false},
	})
	cmd, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1009")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if safe {
		t.Fatalf("expected failsafe=false for occupied room with fresh data")
	}
	if cmd.Setpoint != occupiedSetpoint {
		t.Fatalf("setpoint = %v, want occupiedSetpoint=%v", cmd.Setpoint, occupiedSetpoint)
	}
	if cmd.FanSpeed <= 0 {
		t.Fatalf("fan speed = %v, want > 0 since temperature is below setpoint", cmd.FanSpeed)
	}
}

func TestDecideEmptyRoomSetsBack(t *testing.T) {
	srv := testServer(t, roomState{
		Readings: map[string]reading{"temperature": {Value: 21.0}, "occupancy": {Value: 0}},
		Stale:    map[string]bool{"temperature": false, "occupancy": false},
	})
	cmd, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1010")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if safe {
		t.Fatalf("expected failsafe=false for a normally-empty room")
	}
	if cmd.Setpoint != setbackSetpoint || cmd.FanSpeed != minFanSpeed {
		t.Fatalf("command = %+v, want setback (%v/%v)", cmd, setbackSetpoint, minFanSpeed)
	}
}

func TestDecideAnticipatesRisingForecast(t *testing.T) {
	srv := testServer(t, roomState{
		Readings: map[string]reading{"temperature": {Value: 19.0}, "occupancy": {Value: 0}},
		Stale:    map[string]bool{"temperature": false, "occupancy": false},
		Forecast: map[string]forecast{"occupancy": {SlopePerMin: 0.2, Predicted5m: 1.0}},
	})
	cmd, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1013")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if safe {
		t.Fatalf("expected failsafe=false when anticipating arrival")
	}
	if cmd.Setpoint != occupiedSetpoint {
		t.Fatalf("setpoint = %v, want occupiedSetpoint=%v when anticipating arrival", cmd.Setpoint, occupiedSetpoint)
	}
	if cmd.FanSpeed > anticipateMaxFanSpeed {
		t.Fatalf("fan speed = %v, want capped at anticipateMaxFanSpeed=%v", cmd.FanSpeed, anticipateMaxFanSpeed)
	}
}

func TestDecideMissingDataFailsSafe(t *testing.T) {
	srv := testServer(t, roomState{
		Readings: map[string]reading{"temperature": {Value: 21.0}},
		Stale:    map[string]bool{},
	})
	cmd, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1009")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !safe {
		t.Fatalf("expected failsafe=true when occupancy reading is missing")
	}
	if cmd.Setpoint != fallbackSetpoint || cmd.FanSpeed != fallbackFanSpeed {
		t.Fatalf("command = %+v, want fallback (%v/%v)", cmd, fallbackSetpoint, fallbackFanSpeed)
	}
}

func TestDecideStaleDataFailsSafe(t *testing.T) {
	srv := testServer(t, roomState{
		Readings: map[string]reading{"temperature": {Value: 21.0}, "occupancy": {Value: 1}},
		Stale:    map[string]bool{"temperature": true, "occupancy": false},
	})
	_, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1009")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !safe {
		t.Fatalf("expected failsafe=true when temperature is marked stale")
	}
}

func TestDecidePipelineUnreachableFailsSafe(t *testing.T) {
	srv := testServer(t, roomState{})
	srv.Close() // close immediately so requests fail with a connection error

	cmd, safe, err := decide(context.Background(), srv.Client(), srv.URL, "level0/A1009")
	if err != nil {
		t.Fatalf("decide should absorb the transport error and fail safe, got err=%v", err)
	}
	if !safe {
		t.Fatalf("expected failsafe=true when the pipeline is unreachable")
	}
	if cmd.Setpoint != fallbackSetpoint || cmd.FanSpeed != fallbackFanSpeed {
		t.Fatalf("command = %+v, want fallback (%v/%v)", cmd, fallbackSetpoint, fallbackFanSpeed)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want float64 }{
		{10, 0, 100, 10},
		{-5, 0, 100, 0},
		{150, 0, 100, 100},
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%v,%v,%v) = %v, want %v", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

func TestAbs(t *testing.T) {
	if abs(-3.5) != 3.5 || abs(3.5) != 3.5 {
		t.Fatalf("abs(-3.5)=%v abs(3.5)=%v, want 3.5 both", abs(-3.5), abs(3.5))
	}
}
