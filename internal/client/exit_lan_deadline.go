package client

import (
	"context"
	"errors"
	"math"
	"time"
)

var errExitLANClock = errors.New("LAN clock evidence is unavailable")

// wall is sampled between the two CLOCK_BOOTTIME readings. Neither this sample
// nor a deadline derived from it is serializable authority across restart.
type exitLANClockSample struct {
	bootBefore, bootAfter uint64
	wall                  time.Time
}

func (s exitLANClockSample) valid() bool {
	return !s.wall.IsZero() && s.bootBefore <= s.bootAfter
}

// Immutable absolute cap for one preparation. A delayed kernel publication
// must copy bootExpires verbatim, never translate it back to a relative TTL.
// This does not implement cross-preparation evidence caps or kernel enforcement.
type exitLANBootDeadline struct {
	anchor      exitLANClockSample
	bootExpires uint64
	wallExpires time.Time
}

func newExitLANBootDeadline(anchor exitLANClockSample, expires time.Time) (*exitLANBootDeadline, error) {
	if !anchor.valid() || expires.IsZero() || !anchor.wall.Before(expires) {
		return nil, errExitLANClock
	}
	remaining := expires.Sub(anchor.wall)
	// time.Sub saturates for distant timestamps. Reject rather than silently
	// substituting another expiry, and check uint64 nanosecond arithmetic.
	if remaining <= 0 || !anchor.wall.Add(remaining).Equal(expires) || uint64(remaining) > math.MaxUint64-anchor.bootBefore {
		return nil, errExitLANClock
	}
	cap := anchor.bootBefore + uint64(remaining)
	if cap <= anchor.bootAfter {
		return nil, errExitLANClock
	}
	return &exitLANBootDeadline{anchor: anchor, bootExpires: cap, wallExpires: expires}, nil
}

func (d *exitLANBootDeadline) current(sample exitLANClockSample) bool {
	return d != nil && sample.valid() && sample.bootBefore >= d.anchor.bootAfter &&
		!sample.wall.Before(d.anchor.wall) && sample.wall.Before(d.wallExpires) && sample.bootAfter < d.bootExpires
}

func (e *WireGuardEngine) prepareExitLANDeadlineWithClock(ctx context.Context, cfg Config, topology *exitLANSource, clock func(context.Context) (exitLANClockSample, error), inspect func(*WireGuardEngine) (WireGuardInspection, error)) (*exitLANPlan, *exitLANPeerHealth, *exitLANBootDeadline, error) {
	if clock == nil {
		return nil, nil, nil, errExitLANClock
	}
	anchor, err := clock(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	plan, health, err := e.prepareExitLANWithInspection(ctx, cfg, topology, anchor.wall, inspect)
	if err != nil {
		return nil, nil, nil, err
	}
	deadline, err := newExitLANBootDeadline(anchor, plan.expires)
	if err != nil {
		return nil, nil, nil, err
	}
	after, err := clock(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if !deadline.current(after) || !plan.topologyCurrent(after.wall) {
		return nil, nil, nil, errExitLANClock
	}
	// Topology readback can wait on its source. Sample the absolute clock again
	// afterwards; a realtime rollback cannot hide time spent in that readback.
	finished, err := clock(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if !deadline.current(finished) {
		return nil, nil, nil, errExitLANClock
	}
	return plan, health, deadline, nil
}
