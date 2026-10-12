package main

import (
	"math"
	"testing"
	"time"
)

// runFor steps the model for the given simulated duration in 5-minute ticks,
// the size of one 5 s real tick at the default 60x clock.
func runFor(m *model, d time.Duration, occupancy int, setpoint, fan, outdoor float64) {
	for t := time.Duration(0); t < d; t += 5 * time.Minute {
		m.step(300, occupancy, setpoint, fan, outdoor)
	}
}

func TestOccupiedRoomReachesComfortBandOnAColdDay(t *testing.T) {
	// A unit that only pulls the room towards the setpoint gets stuck near 20 °C on a cold day. With the room
	// unit's own thermostat the occupied setpoint must be reachable at 1 °C
	// outdoor while ventilating for one person.
	m := newModel(15.3)
	m.temperature = 18
	runFor(m, 3*time.Hour, 1, 22, 50, 1)
	if m.temperature < 21 || m.temperature > 22.5 {
		t.Fatalf("temperature = %.2f, want it held near the 22 °C setpoint (21–22.5)", m.temperature)
	}
}

func TestSetbackHoldsEighteenDegrees(t *testing.T) {
	// Setback must hold 18 °C, not let an empty room fall to the outdoor temperature.
	m := newModel(27.2)
	runFor(m, 12*time.Hour, 0, 18, 10, 1)
	if m.temperature < 17.3 || m.temperature > 18.5 {
		t.Fatalf("temperature = %.2f, want the setback of 18 °C to hold", m.temperature)
	}
}

func TestBodyHeatWarmsAnUnheatedRoom(t *testing.T) {
	empty, busy := newModel(15.3), newModel(15.3)
	runFor(empty, 2*time.Hour, 0, 0, 0, 5)
	runFor(busy, 2*time.Hour, 3, 0, 0, 5)
	if busy.temperature <= empty.temperature {
		t.Fatalf("3 people = %.2f °C, empty = %.2f °C, want people to add heat", busy.temperature, empty.temperature)
	}
}

func TestLargeStepIsStable(t *testing.T) {
	// One 2-hour step (a clock jump) must land close to many small steps,
	// instead of oscillating or blowing up like plain Euler would.
	big, small := newModel(15.3), newModel(15.3)
	big.temperature, small.temperature = 16, 16
	big.step(2*3600, 1, 22, 100, 1)
	runFor(small, 2*time.Hour, 1, 22, 100, 1)
	if math.Abs(big.temperature-small.temperature) > 0.3 {
		t.Fatalf("one big step = %.2f °C, small steps = %.2f °C, want them to agree", big.temperature, small.temperature)
	}
}

func TestVentilationCostsHeat(t *testing.T) {
	// Fresh air at 1 °C has to be heated, so ventilating an empty room is not free
	// (an earlier model forgot this: the fan changed humidity but not temperature).
	low, high := newModel(15.3), newModel(15.3)
	runFor(low, 6*time.Hour, 0, 22, 10, 1)
	runFor(high, 6*time.Hour, 0, 22, 100, 1)
	if high.energyKWh <= low.energyKWh*1.1 {
		t.Fatalf("energy at 100 %% fan = %.2f kWh, at 10 %% = %.2f kWh, want ventilation to cost clearly more", high.energyKWh, low.energyKWh)
	}
	if math.Abs(high.temperature-low.temperature) > 0.5 {
		t.Fatalf("temperatures %.2f / %.2f °C, want the unit to hold the setpoint in both", high.temperature, low.temperature)
	}
}

func TestHumidityRisesWithPeopleAndStaysInRange(t *testing.T) {
	m := newModel(13.8)
	runFor(m, 6*time.Hour, 1, 22, 0, 5)
	if m.humidity <= outdoorRH {
		t.Fatalf("humidity = %.1f, want a person to raise it above the %.0f%% outdoor level", m.humidity, outdoorRH)
	}
	runFor(m, 6*time.Hour, 50, 22, 0, 5)
	if m.humidity < 0 || m.humidity > 100 {
		t.Fatalf("humidity = %.1f, want it inside 0–100", m.humidity)
	}
	runFor(m, 6*time.Hour, 0, 22, 100, 5)
	if math.Abs(m.humidity-outdoorRH) > 1 {
		t.Fatalf("humidity = %.1f, want ventilation to bring an empty room back to ~%.0f%%", m.humidity, outdoorRH)
	}
}

func TestEnergyMeterOnlyCountsWhenTheUnitRuns(t *testing.T) {
	m := newModel(15.3)
	m.temperature = 23 // inside the deadband: no heating, no cooling
	m.step(300, 0, 22, 0, 23)
	if m.energyKWh != 0 {
		t.Fatalf("energy = %.4f kWh with fan off, want 0", m.energyKWh)
	}
	m.temperature = 15
	m.step(300, 0, 22, 100, 5)
	if m.energyKWh <= 0 {
		t.Fatalf("energy = %.4f kWh after heating, want > 0", m.energyKWh)
	}
}
