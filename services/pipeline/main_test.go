package main

import (
	"errors"
	"math"
	"testing"
	"time"
)

func temp(value float64, at time.Time) reading {
	return reading{Room: "level0/A1009", Type: "temperature", Value: value, Unit: "°C", Timestamp: at}
}

func TestValidateRejectsBadReadings(t *testing.T) {
	now := time.Now()
	bad := map[string]reading{
		"bare room name":  {Room: "A1009", Type: "temperature", Value: 21, Timestamp: now},
		"unknown type":    {Room: "level0/A1009", Type: "co2", Value: 600, Timestamp: now},
		"NaN":             temp(math.NaN(), now),
		"too hot":         temp(85, now),
		"negative count":  {Room: "level0/A1009", Type: "occupancy", Value: -1, Timestamp: now},
		"from the future": temp(21, now.Add(time.Hour)),
	}
	for name, r := range bad {
		if err := validate(r, now); !errors.Is(err, errInvalid) {
			t.Errorf("%s: validate = %v, want errInvalid", name, err)
		}
	}
	if err := validate(temp(21.4, now), now); err != nil {
		t.Errorf("a normal reading was rejected: %v", err)
	}
}

func TestIngestRejectsDuplicatesAndOutOfOrder(t *testing.T) {
	s := newStore()
	t0 := time.Now().Add(-time.Minute)
	if err := s.ingest(temp(21, t0)); err != nil {
		t.Fatalf("first reading: %v", err)
	}
	if err := s.ingest(temp(21, t0)); !errors.Is(err, errDuplicate) {
		t.Fatalf("same reading again (a sensor retry): %v, want errDuplicate", err)
	}
	if err := s.ingest(temp(21, t0.Add(-time.Second))); !errors.Is(err, errDuplicate) {
		t.Fatalf("older reading: %v, want errDuplicate", err)
	}
	if s.counts.Accepted != 1 || s.counts.Duplicates != 2 {
		t.Fatalf("counts = %+v, want 1 accepted, 2 duplicates", s.counts)
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-time.Minute)
	for i := 0; i < 5; i++ {
		if err := s.ingest(temp(20+float64(i), t0.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.ingest(temp(99, t0.Add(10*time.Second))) // rejected, must not be replayed
	s.file.Close()

	restarted, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(restarted.historyFor("level0/A1009", "temperature", 0)); got != 5 {
		t.Fatalf("history after restart = %d readings, want 5", got)
	}
	if restarted.latest["level0/A1009"]["temperature"].Value != 24 {
		t.Fatalf("latest after restart = %v, want 24", restarted.latest["level0/A1009"]["temperature"].Value)
	}
	// Dedup must also work across the restart: a sensor re-sending a stored reading.
	if err := restarted.ingest(temp(24, t0.Add(4*time.Second))); !errors.Is(err, errDuplicate) {
		t.Fatalf("re-sent reading after restart: %v, want errDuplicate", err)
	}
}

func TestQualityFlags(t *testing.T) {
	s := newStore()
	now := time.Now()
	for i := 0; i < stuckWindow; i++ {
		s.remember(temp(21.37, now.Add(time.Duration(i-stuckWindow)*time.Second)))
	}
	if q := s.state("level0/A1009", 30*time.Second).Quality["temperature"]; q != "stuck" {
		t.Fatalf("quality after %d identical readings = %q, want stuck", stuckWindow, q)
	}
	s.remember(temp(21.41, now))
	if q := s.state("level0/A1009", 30*time.Second).Quality["temperature"]; q != "ok" {
		t.Fatalf("quality after a changing reading = %q, want ok", q)
	}
	s.remember(reading{Room: "level0/A1009", Type: "occupancy", Value: 0, Timestamp: now.Add(-time.Hour)})
	if q := s.state("level0/A1009", 30*time.Second).Quality["occupancy"]; q != "stale" {
		t.Fatalf("quality of an hour-old reading = %q, want stale", q)
	}
}

func TestOccupancyIsNeverFlaggedStuck(t *testing.T) {
	s := newStore()
	now := time.Now()
	for i := 0; i < 3*stuckWindow; i++ {
		s.remember(reading{Room: "level0/A1009", Type: "occupancy", Value: 0, Timestamp: now.Add(time.Duration(i-3*stuckWindow) * time.Second)})
	}
	if q := s.state("level0/A1009", 30*time.Second).Quality["occupancy"]; q != "ok" {
		t.Fatalf("an empty room's counter = %q, want ok (a counter may repeat)", q)
	}
}
