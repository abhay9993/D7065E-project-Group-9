package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScheduleFollowsOfficeHours(t *testing.T) {
	cases := []struct {
		name     string
		at       time.Time
		setpoint float64
	}{
		{"Monday 10:00", time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC), scheduleSetpoint},
		{"Monday 07:00 (early start)", time.Date(2026, 10, 5, 7, 0, 0, 0, time.UTC), scheduleSetpoint},
		{"Monday 18:00 (end)", time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC), setbackSetpoint},
		{"Monday 20:00", time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC), setbackSetpoint},
		{"Saturday 10:00", time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC), setbackSetpoint},
	}
	for _, c := range cases {
		if cmd := schedule("A1015", c.at); cmd.Setpoint != c.setpoint || cmd.Source != "baseline" {
			t.Errorf("%s: %+v, want setpoint %.0f from source baseline", c.name, cmd, c.setpoint)
		}
	}
}

func TestScheduleFollowsTheModelClock(t *testing.T) {
	// The occupancy simulator says it is Tuesday 09:00; the wall clock can be
	// anything. The baseline must follow the model clock.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sim": map[string]any{"clock": map[string]any{
			"time": "2026-10-13T09:00:00Z", "factor": 60, "running": true}}})
	}))
	defer srv.Close()
	clock := &simClock{url: srv.URL, client: srv.Client()}
	now := clock.now(context.Background())
	if now.Weekday() != time.Tuesday || now.Hour() != 9 {
		t.Fatalf("clock = %s, want the model time Tuesday 09:00", now)
	}
	if cmd := schedule("A1015", now); cmd.Setpoint != scheduleSetpoint {
		t.Fatalf("command = %+v, want occupied hours at model 09:00", cmd)
	}

	srv.Close() // occupancy simulator goes away: keep extrapolating, do not fall back to the wall clock
	later := clock.now(context.Background())
	if later.Before(now) || later.Sub(now) > time.Hour {
		t.Fatalf("extrapolated clock = %s, want just after %s", later, now)
	}
}
