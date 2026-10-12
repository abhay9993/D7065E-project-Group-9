package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBuildSim records every actuator state written to it.
func fakeBuildSim(t *testing.T) (*httptest.Server, map[string]string) {
	t.Helper()
	var mu sync.Mutex
	written := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ State string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/actuators/"), "/state")
		mu.Lock()
		written[id] = body.State
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, written
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/commands", strings.NewReader(body)))
	return rec
}

func TestCommandIsClampedAndApplied(t *testing.T) {
	bs, written := fakeBuildSim(t)
	a := newActuator(bs.URL, map[string]string{"A1009": "controller"}, time.Minute)
	rec := post(t, a.routes(), `{"room":"A1009","setpoint":35,"fan_speed":150,"reason":"test","source":"controller"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if written["A1009-setpoint"] != "28.0" || written["A1009-fan"] != "100.0" {
		t.Fatalf("buildsim got %v, want setpoint clamped to 28.0 and fan to 100.0", written)
	}
}

func TestOnlyTheOwnerMayCommandARoom(t *testing.T) {
	bs, written := fakeBuildSim(t)
	a := newActuator(bs.URL, map[string]string{"A1009": "controller", "A1010": "baseline"}, time.Minute)
	for _, body := range []string{
		`{"room":"A1010","setpoint":22,"fan_speed":50,"source":"controller"}`, // someone else's room
		`{"room":"A9999","setpoint":22,"fan_speed":50,"source":"controller"}`, // not a managed room
		`{"room":"A1009","setpoint":22,"fan_speed":50}`,                       // no source at all
	} {
		if rec := post(t, a.routes(), body); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", body, rec.Code)
		}
	}
	if len(written) != 0 {
		t.Fatalf("refused commands reached buildsim: %v", written)
	}
}

func TestWatchdogAppliesSafeStateWhenControllerGoesQuiet(t *testing.T) {
	bs, written := fakeBuildSim(t)
	a := newActuator(bs.URL, map[string]string{"A1009": "controller"}, time.Minute)
	post(t, a.routes(), `{"room":"A1009","setpoint":22,"fan_speed":81,"source":"controller"}`)

	a.checkLeases(context.Background(), time.Now().Add(30*time.Second))
	if written["A1009-fan"] != "81.0" {
		t.Fatalf("lease still valid: fan = %s, want the controller's 81.0", written["A1009-fan"])
	}

	a.checkLeases(context.Background(), time.Now().Add(2*time.Minute))
	if written["A1009-fan"] != "50.0" || written["A1009-setpoint"] != "21.0" || !a.byRoom["A1009"].Watchdog {
		t.Fatalf("lease expired: buildsim=%v state=%+v, want the safe state 21.0/50.0", written, a.byRoom["A1009"])
	}

	post(t, a.routes(), `{"room":"A1009","setpoint":22,"fan_speed":70,"source":"controller"}`)
	if a.byRoom["A1009"].Watchdog || written["A1009-fan"] != "70.0" {
		t.Fatalf("controller back: state=%+v, want normal control to resume", a.byRoom["A1009"])
	}
}
