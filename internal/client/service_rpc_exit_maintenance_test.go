package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitMaintenanceSkipsBusyEffectsAndReadsFreshContext(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	lock := &sync.Mutex{}
	calls := 0
	executor := clientRPCExitExecutor{Lock: lock, Maintain: func(_ context.Context, cfg Config) error {
		calls++
		if cfg.NodeID != "replacement" {
			t.Fatal("tick reused context from before native effect")
		}
		if lock.TryLock() {
			lock.Unlock()
			t.Fatal("maintenance ran outside effect lock")
		}
		if !m.mu.TryLock() {
			t.Fatal("native maintenance ran under mutation lock")
		}
		m.mu.Unlock()
		return nil
	}}
	lock.Lock()
	m.maintainExitRuntime(t.Context(), executor)
	if calls != 0 {
		t.Fatal("busy effect was inspected")
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "replacement"; return nil }); err != nil {
		t.Fatal(err)
	}
	lock.Unlock()
	m.maintainExitRuntime(t.Context(), executor)
	if calls != 1 {
		t.Fatal("next tick did not inspect updated context")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m.maintainExitRuntime(ctx, executor)
	if calls != 1 {
		t.Fatal("cancelled maintenance reached native effects")
	}
}

func TestExitMaintenanceRechecksAdmissionDuringObservation(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	before := m.store.Read().NodeID
	var seen []string
	executor := clientRPCExitExecutor{Lock: &sync.Mutex{}, Maintain: func(_ context.Context, cfg Config) error {
		seen = append(seen, cfg.NodeID)
		if len(seen) == 1 {
			if err := m.store.Update(func(current *Config) error { current.NodeID = "changed-during-observation"; return nil }); err != nil {
				t.Fatal(err)
			}
		}
		return nil
	}}
	m.maintainExitRuntime(t.Context(), executor)
	if !reflect.DeepEqual(seen, []string{before, "changed-during-observation"}) {
		t.Fatal("new durable context was not rechecked", seen)
	}
}

func TestExitMaintenanceRetriesFailureAndJoinsCancelledObservation(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	before := m.store.Read()
	ticks := make(chan time.Time, 2)
	first, second := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var stopped atomic.Bool
	executor := clientRPCExitExecutor{Lock: &sync.Mutex{}, Maintain: func(ctx context.Context, _ Config) error {
		if calls.Add(1) == 1 {
			close(first)
			return errors.New("private native failure")
		}
		close(second)
		<-ctx.Done()
		return ctx.Err()
	}}
	stop := m.startExitMaintenanceTicks(t.Context(), executor, ticks, func() { stopped.Store(true) })
	t.Cleanup(stop)
	ticks <- time.Now()
	awaitExitMaintenanceTest(t, first)
	ticks <- time.Now()
	awaitExitMaintenanceTest(t, second)
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	awaitExitMaintenanceTest(t, done)
	if calls.Load() != 2 || !stopped.Load() {
		t.Fatal("maintenance stopped retrying or did not join")
	}
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("maintenance rewrote durable intent")
	}
}

func TestExitWorkerCancellationDoesNotWaitForHeldEffectOrReconcileLock(t *testing.T) {
	for _, held := range []string{"effect", "reconcile"} {
		t.Run(held, func(t *testing.T) {
			m, _, _ := rpcConnectFixture(t)
			service := NewClientRPCService(m, nil)
			lock := &sync.Mutex{}
			blocked := lock
			if held == "reconcile" {
				blocked = &m.exitWorker
			}
			blocked.Lock()
			defer blocked.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: lock, Release: releaseExitTestCallback,
				Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
					t.Error("cancelled worker applied state")
					return nil, 0, nil
				},
				Contain: func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error) {
					t.Error("cancelled worker changed protection")
					return clientRPCExitContainment{}, nil
				},
			}
			done, err := service.startExitWorker(ctx, executor)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("worker lost cancellation", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown waited for a lock held by lifecycle or another reconcile")
			}
		})
	}
}

func awaitExitMaintenanceTest(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("maintenance did not progress")
	}
}
