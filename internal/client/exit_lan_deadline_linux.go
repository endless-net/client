//go:build linux

package client

import (
	"context"
	"math"
	"time"

	"golang.org/x/sys/unix"
)

func captureExitLANClock(ctx context.Context) (exitLANClockSample, error) {
	return captureExitLANClockWithRead(ctx, unix.ClockGettime)
}

func captureExitLANClockWithRead(ctx context.Context, read func(int32, *unix.Timespec) error) (exitLANClockSample, error) {
	var empty exitLANClockSample
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if read == nil {
		return empty, errExitLANClock
	}
	var before, wall, after unix.Timespec
	for _, item := range []struct {
		id    int32
		value *unix.Timespec
	}{{unix.CLOCK_BOOTTIME, &before}, {unix.CLOCK_REALTIME, &wall}, {unix.CLOCK_BOOTTIME, &after}} {
		if err := read(item.id, item.value); err != nil {
			return empty, errExitLANClock
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if item.value.Sec < 0 || item.value.Nsec < 0 || item.value.Nsec >= 1e9 {
			return empty, errExitLANClock
		}
	}
	bootNanos := func(value unix.Timespec) (uint64, bool) {
		seconds := uint64(value.Sec)
		if seconds > (math.MaxUint64-uint64(value.Nsec))/1e9 {
			return 0, false
		}
		return seconds*1e9 + uint64(value.Nsec), true
	}
	start, ok := bootNanos(before)
	if !ok {
		return empty, errExitLANClock
	}
	end, ok := bootNanos(after)
	if !ok {
		return empty, errExitLANClock
	}
	// Authority timestamps use RFC3339's four-digit year range. Reject values
	// outside it before time.Unix can overflow its internal epoch conversion.
	if wall.Sec > 253402300799 {
		return empty, errExitLANClock
	}
	sample := exitLANClockSample{bootBefore: start, bootAfter: end, wall: time.Unix(int64(wall.Sec), int64(wall.Nsec)).UTC()}
	if !sample.valid() {
		return empty, errExitLANClock
	}
	return sample, nil
}

// No kernel rule is opened by this preparation. Before publication an adapter
// still needs persistent evidence caps (including across restart), exact route
// ownership, live pinned attachment readback and interface-instance binding.
// Caller holds engine.mu and owns the topology stream for the full lifetime.
func (e *WireGuardEngine) prepareNativeExitLANDeadlineLocked(ctx context.Context, cfg Config, topology *exitLANSource) (*exitLANPlan, *exitLANPeerHealth, *exitLANBootDeadline, error) {
	return e.prepareExitLANDeadlineWithClock(ctx, cfg, topology, captureExitLANClock, resourceObservedUAPI)
}
