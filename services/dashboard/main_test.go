package main

import (
	"testing"
)

func TestSeverity(t *testing.T) {
	ok := roomSummary{Mode: "occupied", State: roomState{Quality: map[string]string{"temperature": "ok"}}}
	if got := severity(ok); got != "info" {
		t.Fatalf("normal room: %s, want info", got)
	}
	stuck := roomSummary{Mode: "occupied", State: roomState{Quality: map[string]string{"temperature": "stuck"}}}
	if got := severity(stuck); got != "warning" {
		t.Fatalf("stuck sensor: %s, want warning", got)
	}
	blind := roomSummary{Mode: "failsafe", State: roomState{Quality: map[string]string{"temperature": "stale"}}}
	if got := severity(blind); got != "critical" {
		t.Fatalf("failsafe: %s, want critical", got)
	}
	watchdog := roomSummary{Mode: "occupied", Applied: appliedState{Watchdog: true}}
	if got := severity(watchdog); got != "critical" {
		t.Fatalf("watchdog: %s, want critical", got)
	}
}
