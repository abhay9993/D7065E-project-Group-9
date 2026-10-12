package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tuesday 08:00 on the model clock.
var tuesday8 = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)

func room(temp, occ, humidity float64) roomState {
	return roomState{
		Readings: map[string]reading{"temperature": {Value: temp}, "occupancy": {Value: occ}, "humidity": {Value: humidity}},
		Quality:  map[string]string{"temperature": "ok", "occupancy": "ok", "humidity": "ok"},
	}
}

func TestOccupiedRoomHeldAtComfortAndVentilated(t *testing.T) {
	d := decide(room(20, 1, 35), tuesday8, tuesday8, -1, 0.5)
	if d.Mode != "occupied" || d.Setpoint != occupiedSetpoint {
		t.Fatalf("decision = %+v, want occupied at %.0f°C", d, occupiedSetpoint)
	}
	if d.FanSpeed < 50 {
		t.Fatalf("fan = %.0f, want at least 50 %% fresh air while someone is in (REG-01)", d.FanSpeed)
	}
}

func TestShortAbsenceKeepsComfortHysteresis(t *testing.T) {
	left := tuesday8.Add(-20 * time.Minute) // gone for coffee
	d := decide(room(21.5, 0, 35), tuesday8, left, -1, 0.5)
	if d.Mode != "occupied" {
		t.Fatalf("20 min after leaving: mode = %s, want occupied (grace %s)", d.Mode, emptyGrace)
	}
	future := tuesday8.Add(3 * time.Hour) // the model clock was set back after this
	if d := decide(room(21.5, 0, 35), tuesday8, future, -1, 0.5); d.Mode != "setback" {
		t.Fatalf("clock set back: mode = %s, want setback (a negative 'empty for' is not a short break)", d.Mode)
	}
	left = tuesday8.Add(-45 * time.Minute) // gone for lunch
	if d := decide(room(21.5, 0, 35), tuesday8, left, -1, 0.5); d.Mode != "setback" {
		t.Fatalf("45 min after leaving: mode = %s, want setback", d.Mode)
	}
}

func TestEmptyRoomSetsBackUnlessArrivalIsLikely(t *testing.T) {
	if d := decide(room(19, 0, 35), tuesday8, time.Time{}, 0.2, 0.5); d.Mode != "setback" || d.Setpoint != setbackSetpoint || d.FanSpeed != emptyFan {
		t.Fatalf("unlikely arrival: %+v, want setback at %.0f°C with minimum ventilation", d, setbackSetpoint)
	}
	if d := decide(room(19, 0, 35), tuesday8, time.Time{}, -1, 0.5); d.Mode != "setback" {
		t.Fatalf("nothing learned yet: mode = %s, want setback (no pre-heat without evidence)", d.Mode)
	}
	d := decide(room(18, 0, 35), tuesday8, time.Time{}, 0.8, 0.5)
	if d.Mode != "preheat" || d.Setpoint != occupiedSetpoint || d.FanSpeed != emptyFan {
		t.Fatalf("likely arrival: %+v, want pre-heat to %.0f°C without ventilating the empty room", d, occupiedSetpoint)
	}
}

func TestBadDataFailsSafe(t *testing.T) {
	stuck := room(21, 0, 35)
	stuck.Quality["occupancy"] = "stuck"
	stale := room(21, 1, 35)
	stale.Quality["temperature"] = "stale"
	missing := room(21, 1, 35)
	delete(missing.Readings, "occupancy")

	for name, st := range map[string]roomState{"stuck": stuck, "stale": stale, "missing": missing} {
		d := decide(st, tuesday8, time.Time{}, 0.9, 0.5)
		if d.Mode != "failsafe" || d.Setpoint != failsafeSetpoint || d.FanSpeed != failsafeFan {
			t.Errorf("%s input: %+v, want the failsafe command", name, d)
		}
	}
}

func TestHumidityRaisesVentilationButIsNotSafetyCritical(t *testing.T) {
	d := decide(room(19, 0, 70), tuesday8, time.Time{}, 0.1, 0.5)
	if d.FanSpeed != humidFan || !strings.Contains(d.Reason, "humidity") {
		t.Fatalf("humid empty room: %+v, want fan %.0f with a reason", d, humidFan)
	}
	broken := room(19, 0, 70)
	broken.Quality["humidity"] = "stale"
	if d := decide(broken, tuesday8, time.Time{}, 0.1, 0.5); d.Mode != "setback" || d.FanSpeed != emptyFan {
		t.Fatalf("stale humidity: %+v, want the rule skipped, not a failsafe", d)
	}
}

func TestPriorLearnsWhenARoomIsUsuallyOccupied(t *testing.T) {
	p := loadPrior(filepath.Join(t.TempDir(), "prior.json"))
	// Three weekdays: the room is occupied from 09:00, empty before.
	for day := 0; day < 3; day++ {
		for m := 0; m < 24*60; m += 10 {
			t0 := time.Date(2026, 10, 12+day, 0, 0, 0, 0, time.UTC).Add(time.Duration(m) * time.Minute)
			p.observe("A1009", t0, t0.Hour() >= 9 && t0.Hour() < 17)
		}
	}
	if got := p.ahead("A1009", tuesday8.Add(10*time.Minute)); got < 0.99 {
		t.Fatalf("at 08:10 P(occupied within the hour) = %.2f, want ~1 (people arrive at 09:00)", got)
	}
	if got := p.ahead("A1009", time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)); got != 0 {
		t.Fatalf("at 03:00 P = %.2f, want 0", got)
	}
	if got := p.ahead("A1009", time.Date(2026, 10, 17, 8, 10, 0, 0, time.UTC)); got != -1 {
		t.Fatalf("on a Saturday nothing is learned yet: P = %.2f, want -1", got)
	}

	// It must survive a controller restart.
	p.save()
	again := loadPrior(p.path)
	if got := again.ahead("A1009", tuesday8.Add(10*time.Minute)); got < 0.99 {
		t.Fatalf("after reload P = %.2f, want the learned value back", got)
	}
}

func TestForecastStatsCountHitsAndFalseAlarms(t *testing.T) {
	st := newStatus()
	st.preheatEnded("A1009", "setback", "preheat")
	st.arrival("A1009", "preheat") // walked into a warm room
	st.preheatEnded("A1009", "preheat", "occupied")

	st.preheatEnded("A1009", "setback", "preheat")
	st.preheatEnded("A1009", "preheat", "setback") // nobody came

	st.arrival("A1009", "setback")  // walked into a cold room
	st.arrival("A1009", "occupied") // back from coffee: not a new arrival

	f := st.forecast["A1009"]
	if f.Arrivals != 2 || f.Preheated != 1 || f.FalsePreheats != 1 {
		t.Fatalf("stats = %+v, want 2 arrivals, 1 pre-heated, 1 false pre-heat", f)
	}
}
