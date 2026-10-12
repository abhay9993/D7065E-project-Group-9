package main

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)

// feed records one sample every 10 model minutes.
func feed(a *roomAcc, from time.Time, samples []sample) time.Time {
	now := from
	for _, s := range samples {
		a.record(now, s)
		now = now.Add(10 * time.Minute)
	}
	return now
}

func TestComfortUsesAFixedBandNotTheSetpoint(t *testing.T) {
	a := &roomAcc{}
	feed(a, t0, []sample{
		{Occupancy: 0, Temperature: 18, EnergyKWh: 0},   // first sample only starts the clock
		{Occupancy: 1, Temperature: 19, EnergyKWh: 0.1}, // cold
		{Occupancy: 1, Temperature: 21, EnergyKWh: 0.2}, // fine
		{Occupancy: 1, Temperature: 23.5, EnergyKWh: 0.3},
		{Occupancy: 0, Temperature: 30, EnergyKWh: 0.4}, // too hot, but nobody is there
	})
	if math.Abs(a.OccupiedHours-0.5) > 1e-9 || math.Abs(a.ComfortHours-1.0/3) > 1e-9 {
		t.Fatalf("occupied %.3f h, comfortable %.3f h, want 0.5 and 0.333", a.OccupiedHours, a.ComfortHours)
	}
	if math.Abs(a.EnergyKWh-0.4) > 1e-9 {
		t.Fatalf("energy = %.3f kWh, want the 0.4 kWh the meter advanced", a.EnergyKWh)
	}
}

func TestArrivalWindow(t *testing.T) {
	a := &roomAcc{}
	samples := []sample{{Occupancy: 0, Temperature: 18}}
	for i := 0; i < 6; i++ { // one hour occupied, cold for the first 20 minutes
		temp := 21.0
		if i < 2 {
			temp = 18.5
		}
		samples = append(samples, sample{Occupancy: 1, Temperature: temp})
	}
	feed(a, t0, samples)
	if math.Abs(a.ArrivalHours-0.5) > 1e-9 || math.Abs(a.ArrivalComfort-1.0/6) > 1e-9 {
		t.Fatalf("arrival window %.3f h, comfortable %.3f h, want 0.5 and 0.167", a.ArrivalHours, a.ArrivalComfort)
	}
}

func TestMeterResetAndClockJumpsAreNotCounted(t *testing.T) {
	a := &roomAcc{}
	now := feed(a, t0, []sample{{EnergyKWh: 5}, {EnergyKWh: 5.2}})
	a.record(now, sample{EnergyKWh: 0.1}) // simulator + BuildSim restarted, meter back near 0
	a.record(now.Add(10*time.Minute), sample{EnergyKWh: 0.3})
	if math.Abs(a.EnergyKWh-0.4) > 1e-9 {
		t.Fatalf("energy = %.3f, want 0.2 before the reset + 0.2 after = 0.4", a.EnergyKWh)
	}
	glitch := &roomAcc{AreaM2: 15}
	feed(glitch, t0, []sample{{EnergyKWh: 50}, {EnergyKWh: 0}, {EnergyKWh: 50.2}}) // re-seeded meter read as 0 for one sample
	if glitch.EnergyKWh != 0 || glitch.MeterGlitches != 2 {
		t.Fatalf("glitch: energy %.2f kWh, glitches %d, want 0 and 2 (a 0 -> 50 kWh jump in 10 min is impossible for 15 m²)", glitch.EnergyKWh, glitch.MeterGlitches)
	}
	hours := a.Hours
	a.record(now.Add(10*time.Hour), sample{EnergyKWh: 9}) // the clock jumped 10 hours
	if a.Hours != hours || a.EnergyKWh > 0.41 {
		t.Fatalf("a 10 h jump was credited: hours %.2f -> %.2f, energy %.2f", hours, a.Hours, a.EnergyKWh)
	}
}

func TestUsableSkipsBadData(t *testing.T) {
	rs := roomState{
		Readings: map[string]reading{"occupancy": {}, "temperature": {}, "energy": {}},
		Quality:  map[string]string{"occupancy": "ok", "temperature": "stuck", "energy": "ok"},
	}
	if usable(rs, "occupancy", "temperature", "energy") {
		t.Fatalf("a stuck temperature must not be scored")
	}
	rs.Quality["temperature"] = "ok"
	if !usable(rs, "occupancy", "temperature", "energy") {
		t.Fatalf("good data was skipped")
	}
}

func TestReportComparesGroupsPerSquareMetre(t *testing.T) {
	s := loadStore(filepath.Join(t.TempDir(), "evaluation.json"))
	s.ByRoom["A1009"] = &roomAcc{Group: "adaptive", AreaM2: 10, Hours: 24, OccupiedHours: 8, ComfortHours: 7.6, EnergyKWh: 8}
	s.ByRoom["A1010"] = &roomAcc{Group: "baseline", AreaM2: 20, Hours: 24, OccupiedHours: 8, ComfortHours: 7.2, EnergyKWh: 20}
	rep := buildReport(s)
	ad, bl := rep.Groups["adaptive"], rep.Groups["baseline"]
	if ad.KWhPerM2PerDay != 0.8 || bl.KWhPerM2PerDay != 1.0 {
		t.Fatalf("kWh/m²/day adaptive %.2f baseline %.2f, want 0.8 and 1.0", ad.KWhPerM2PerDay, bl.KWhPerM2PerDay)
	}
	if ad.ComfortPct != 95 {
		t.Fatalf("comfort = %.1f, want 95", ad.ComfortPct)
	}
	for _, c := range rep.Checks {
		if c.ID == "NFR-04" && (math.Abs(c.Measured-20) > 1e-9 || !c.Pass) {
			t.Fatalf("NFR-04 = %+v, want a 20 %% saving that passes", c)
		}
	}

	s.save()
	again := loadStore(s.path)
	if again.ByRoom["A1009"].EnergyKWh != 8 {
		t.Fatalf("evaluation did not survive a restart")
	}
}
