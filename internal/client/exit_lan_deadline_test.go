package client

import (
	"math"
	"testing"
	"time"
)

func TestExitLANBootDeadlineIsAbsoluteAcrossDelaySuspendAndClockChanges(t *testing.T) {
	wall := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	anchor := exitLANClockSample{bootBefore: 100e9, bootAfter: 100e9 + 10, wall: wall}
	d, err := newExitLANBootDeadline(anchor, wall.Add(20*time.Second))
	if err != nil || d.bootExpires != 120e9 {
		t.Fatal("deadline did not use conservative first boot sample", err)
	}
	for _, scenario := range []struct {
		name   string
		sample exitLANClockSample
		want   bool
	}{
		{"current", exitLANClockSample{101e9, 101e9 + 10, wall.Add(time.Second)}, true},
		{"delayed_publish", exitLANClockSample{121e9, 121e9 + 10, wall.Add(2 * time.Second)}, false},
		{"suspend", exitLANClockSample{125e9, 125e9 + 10, wall.Add(25 * time.Second)}, false},
		{"boot_equal", exitLANClockSample{120e9 - 1, 120e9, wall.Add(time.Second)}, false},
		{"wall_equal", exitLANClockSample{101e9, 101e9 + 10, wall.Add(20 * time.Second)}, false},
		{"wall_back_before_anchor", exitLANClockSample{101e9, 101e9 + 10, wall.Add(-time.Second)}, false},
		{"rollback_within_window", exitLANClockSample{119e9, 119e9 + 10, wall.Add(time.Second)}, true},
		{"rollback_cannot_extend", exitLANClockSample{120e9, 120e9 + 10, wall.Add(time.Second)}, false},
		{"boot_regression", exitLANClockSample{99e9, 99e9 + 10, wall.Add(time.Second)}, false},
		{"overlap", exitLANClockSample{100e9 + 9, 100e9 + 20, wall.Add(time.Second)}, false},
		{"inverted", exitLANClockSample{102e9, 101e9, wall.Add(time.Second)}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if d.current(scenario.sample) != scenario.want {
				t.Fatal("unexpected fixed clock gate")
			}
			if d.bootExpires != 120e9 {
				t.Fatal("read renewed deadline")
			}
		})
	}
}

func TestExitLANBootDeadlineRejectsInvalidArithmetic(t *testing.T) {
	wall := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		name    string
		anchor  exitLANClockSample
		expires time.Time
	}{
		{"missing", exitLANClockSample{}, wall},
		{"inverted", exitLANClockSample{2, 1, wall}, wall.Add(time.Second)},
		{"expired", exitLANClockSample{1, 2, wall}, wall},
		{"overflow", exitLANClockSample{math.MaxUint64 - 5, math.MaxUint64 - 4, wall}, wall.Add(time.Second)},
		{"saturated_duration", exitLANClockSample{1, 2, wall}, wall.AddDate(500, 0, 0)},
		{"capture_crosses_deadline", exitLANClockSample{1, 1e9 + 1, wall}, wall.Add(time.Second)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if d, err := newExitLANBootDeadline(scenario.anchor, scenario.expires); err == nil || d != nil {
				t.Fatal("invalid clock produced deadline")
			}
		})
	}
}
