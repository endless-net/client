package client

import (
	"context"
	"errors"
)

var ErrWireGuardRuntimeSuspended = errors.New("wireguard runtime is suspended")

// Suspend prevents a queued or concurrent Configure from reapplying networking
// after teardown. It changes no durable user intent. The runtime must serialize
// its lifecycle policy decisions with its ordinary effect workers as well.
func (e *WireGuardEngine) Suspend(ctx context.Context) (WireGuardApplyResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runtimeSuspended = true
	return e.downLocked(ctx)
}

// Resume only releases the gate after any failed teardown has been retried.
// It never reapplies the previous map: the runtime must re-evaluate the current
// intent, active profile and authenticated policy before a new Configure.
func (e *WireGuardEngine) Resume(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.runtimeSuspended {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := e.downLocked(ctx); err != nil {
		return err
	}
	e.runtimeSuspended = false
	return nil
}
