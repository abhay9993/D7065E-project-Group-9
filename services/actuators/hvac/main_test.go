package main

import "testing"

func TestClampSetpointAndFanRanges(t *testing.T) {
	cases := []struct {
		v, lo, hi, want float64
	}{
		{10, minSetpointC, maxSetpointC, minSetpointC},
		{30, minSetpointC, maxSetpointC, maxSetpointC},
		{22, minSetpointC, maxSetpointC, 22},
		{-5, minFanSpeed, maxFanSpeed, minFanSpeed},
		{150, minFanSpeed, maxFanSpeed, maxFanSpeed},
	}
	for _, c := range cases {
		if got := clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clamp(%v,%v,%v) = %v, want %v", c.v, c.lo, c.hi, got, c.want)
		}
	}
}
