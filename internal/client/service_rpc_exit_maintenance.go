package client

import (
	"context"
	"reflect"
	"sync"
	"time"
)

// Maintenance owns no admission or durable intent. Each check reads the store
// only after acquiring the same lock as native apply; a busy effect is checked
// on the next tick, never with a snapshot captured before that effect.
func (m *ClientRPCMutations) startExitMaintenance(ctx context.Context, executor clientRPCExitExecutor) func() {
	if executor.Maintain == nil || executor.Lock == nil {
		return func() {}
	}
	ticker := time.NewTicker(time.Second)
	return m.startExitMaintenanceTicks(ctx, executor, ticker.C, ticker.Stop)
}

func (m *ClientRPCMutations) startExitMaintenanceTicks(ctx context.Context, executor clientRPCExitExecutor, ticks <-chan time.Time, stopTicks func()) func() {
	lifetime, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer stopTicks()
		for {
			select {
			case <-lifetime.Done():
				return
			case _, ok := <-ticks:
				if !ok {
					return
				}
				m.maintainExitRuntime(lifetime, executor)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(cancel); <-done }
}

func (m *ClientRPCMutations) maintainExitRuntime(ctx context.Context, executor clientRPCExitExecutor) {
	if ctx.Err() != nil || executor.Maintain == nil || executor.Lock == nil || !executor.Lock.TryLock() {
		return
	}
	defer executor.Lock.Unlock()
	if ctx.Err() != nil {
		return
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Native failures already withdraw packet policy and attempt independent,
	// bounded containment. Retry on future ticks, including failed containment;
	// an error is neither evidence of blocking nor permission to stop monitoring.
	before := m.store.Read()
	_ = executor.Maintain(check, before)
	if check.Err() != nil {
		return
	}
	// Admission can persist new intent while native effects are locked. Recheck
	// that context immediately instead of waiting a tick after an old observation.
	// This remains periodic observation, not an atomic lease on external OS state.
	after := m.store.Read()
	if !reflect.DeepEqual(before, after) {
		_ = executor.Maintain(check, after)
	}
}
