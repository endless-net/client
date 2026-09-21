package client

import (
	"context"
	"reflect"
	"time"
)

// Resume is distinct from maintenance: it may restore effects after a process
// restart, but it cannot turn a pending mutation into a committed selection.
func (m *ClientRPCMutations) reconcileSavedExit(ctx context.Context, executor clientRPCExitExecutor) error {
	if executor.ResumeSaved == nil || executor.Maintain == nil || executor.Lock == nil {
		return nil
	}
	if !lockExitRuntime(ctx, executor.Lock) {
		return ctx.Err()
	}
	defer executor.Lock.Unlock()
	before := m.store.Read()
	if !nativeExitResumeContext(before, executor.InterfaceName) {
		return ctx.Err()
	}
	apply, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _ = executor.ResumeSaved(apply, before)
	// Admission is not serialized by the native effect lock. A Disconnect,
	// identity transition or revoked authority committed during resume must be
	// checked before this effect is handed back to the ordinary agent loop.
	after := m.store.Read()
	if !reflect.DeepEqual(before, after) {
		recovery, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finish()
		_ = executor.Maintain(recovery, after)
	}
	// The adapter retains protection on failure. No callback result is cached as
	// applied state, and no native error is exported or made a terminal worker error.
	return ctx.Err()
}
