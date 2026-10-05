package main

import "testing"

func TestModelStepMovesTowardSetpointWithFanOn(t *testing.T) {
	m := &model{temperature: 18, humidity: 40}
	for i := 0; i < 200; i++ {
		m.step(5, 0, 22, 100, 18)
	}
	if m.temperature <= 18 {
		t.Fatalf("temperature = %v, want it to have risen toward the 22C setpoint", m.temperature)
	}
	if m.temperature > 22.5 {
		t.Fatalf("temperature = %v, overshot the setpoint by more than expected", m.temperature)
	}
}

func TestModelStepDriftsTowardOutdoorWithFanOff(t *testing.T) {
	m := &model{temperature: 22, humidity: 40}
	for i := 0; i < 500; i++ {
		m.step(5, 0, 22, 0, 10)
	}
	if m.temperature >= 22 {
		t.Fatalf("temperature = %v, want it to have drifted down toward the 10C outdoor temperature", m.temperature)
	}
}

func TestModelStepHumidityStaysWithinBounds(t *testing.T) {
	m := &model{temperature: 22, humidity: 89}
	for i := 0; i < 100; i++ {
		m.step(5, 50, 22, 0, 22)
	}
	if m.humidity > 90 || m.humidity < 15 {
		t.Fatalf("humidity = %v, want it clamped to [15, 90]", m.humidity)
	}
}

func TestClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want float64 }{
		{5, 0, 1, 1},
		{-5, 0, 1, 0},
		{0.5, 0, 1, 0.5},
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%v,%v,%v) = %v, want %v", c.v, c.lo, c.hi, got, c.want)
		}
	}
}
