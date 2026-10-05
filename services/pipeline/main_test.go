package main

import (
	"math"
	"testing"
	"time"
)

func TestTrendInsufficientSamples(t *testing.T) {
	if f := trend(nil); f.Samples != 0 || f.Predicted5m != 0 || f.SlopePerMin != 0 {
		t.Fatalf("trend(nil) = %+v, want zero forecast", f)
	}
	f := trend([]reading{{Value: 5, Timestamp: time.Now()}})
	if f.Samples != 1 || f.Predicted5m != 5 || f.SlopePerMin != 0 {
		t.Fatalf("trend(1 sample) = %+v, want flat forecast at the sample value", f)
	}
}

func TestTrendRisingSlopeExtrapolates(t *testing.T) {
	t0 := time.Now()
	history := []reading{
		{Value: 0, Timestamp: t0},
		{Value: 1, Timestamp: t0.Add(1 * time.Minute)},
		{Value: 2, Timestamp: t0.Add(2 * time.Minute)},
	}
	f := trend(history)
	if f.Samples != 3 {
		t.Fatalf("samples = %d, want 3", f.Samples)
	}
	if f.SlopePerMin <= 0 {
		t.Fatalf("slope_per_min = %v, want > 0 for a rising trend", f.SlopePerMin)
	}
	want := 2 + f.SlopePerMin*5
	if math.Abs(f.Predicted5m-want) > 1e-9 {
		t.Fatalf("predicted_5m = %v, want %v (current + slope*5)", f.Predicted5m, want)
	}
}

func TestTrendWindowCapsHistory(t *testing.T) {
	t0 := time.Now()
	history := make([]reading, 0, forecastWindow+10)
	for i := 0; i < forecastWindow+10; i++ {
		history = append(history, reading{Value: 0, Timestamp: t0.Add(time.Duration(i) * time.Minute)})
	}
	// Make only the samples inside the window rise, to prove older ones were dropped.
	for i := len(history) - forecastWindow; i < len(history); i++ {
		history[i].Value = float64(i)
	}
	f := trend(history)
	if f.Samples != forecastWindow {
		t.Fatalf("samples = %d, want capped at forecastWindow=%d", f.Samples, forecastWindow)
	}
}

func TestStoreIngestAndState(t *testing.T) {
	s := newStore()
	s.ingest(reading{Room: "level0/A1009", Type: "temperature", Value: 21.5, Timestamp: time.Now()})

	st := s.state("level0/A1009", 30*time.Second)
	r, ok := st.Readings["temperature"]
	if !ok || r.Value != 21.5 {
		t.Fatalf("readings[temperature] = %+v, ok=%v, want value 21.5", r, ok)
	}
	if st.Stale["temperature"] {
		t.Fatalf("fresh reading reported as stale")
	}
	if _, ok := st.Forecast["temperature"]; !ok {
		t.Fatalf("expected a forecast entry for temperature")
	}
}

func TestStoreStaleness(t *testing.T) {
	s := newStore()
	s.ingest(reading{Room: "level0/A1009", Type: "temperature", Value: 20, Timestamp: time.Now().Add(-time.Hour)})

	st := s.state("level0/A1009", 30*time.Second)
	if !st.Stale["temperature"] {
		t.Fatalf("hour-old reading should be stale with a 30s threshold")
	}
}

func TestHistoryForRespectsLimit(t *testing.T) {
	s := newStore()
	for i := 0; i < 10; i++ {
		s.ingest(reading{Room: "level0/A1009", Type: "temperature", Value: float64(i), Timestamp: time.Now()})
	}
	h := s.historyFor("level0/A1009", "temperature", 3)
	if len(h) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(h))
	}
	if h[len(h)-1].Value != 9 {
		t.Fatalf("last history value = %v, want the most recent (9)", h[len(h)-1].Value)
	}
}
