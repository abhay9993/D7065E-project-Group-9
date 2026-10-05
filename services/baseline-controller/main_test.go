package main

import (
	"testing"
	"time"
)

func TestScheduleWeekdayOccupiedHours(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) // Monday 10:00
	cmd := schedule("A1015", now)
	if cmd.Setpoint != scheduleSetpoint || cmd.FanSpeed != scheduleFanSpeed {
		t.Fatalf("command = %+v, want occupied-hours schedule (%v/%v)", cmd, scheduleSetpoint, scheduleFanSpeed)
	}
}

func TestScheduleWeekdayOutsideHours(t *testing.T) {
	now := time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC) // Monday 20:00
	cmd := schedule("A1015", now)
	if cmd.Setpoint != setbackSetpoint || cmd.FanSpeed != setbackFanSpeed {
		t.Fatalf("command = %+v, want setback (%v/%v)", cmd, setbackSetpoint, setbackFanSpeed)
	}
}

func TestScheduleWeekend(t *testing.T) {
	now := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC) // Saturday 10:00
	cmd := schedule("A1015", now)
	if cmd.Setpoint != setbackSetpoint {
		t.Fatalf("command = %+v, want setback on a weekend regardless of hour", cmd)
	}
}
