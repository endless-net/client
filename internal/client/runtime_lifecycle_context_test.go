package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func runtimeLifecycleContextFixture(t *testing.T, refresh func(context.Context) error) (*RuntimeLifecycleExecutor, *testRuntimeLifecycleEngine, *sync.Mutex, context.CancelFunc) {
	t.Helper()
	m, _, _ := rpcPreferenceFixture(t)
	lifetime, cancel := context.WithCancel(t.Context())
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t}
	executor, err := NewRuntimeLifecycleExecutor(lifetime, m, engine, lock, func() {}, refresh, func(context.Context, bool, error) error { return nil })
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if err := executor.Close(); err != nil {
			t.Error(err)
		}
	})
	return executor, engine, lock, cancel
}

func TestRuntimeLifecycleTransitionCancelsContendedLocks(t *testing.T) {
	for _, scenario := range []string{"effect", "executor"} {
		t.Run(scenario, func(t *testing.T) {
			executor, engine, lock, _ := runtimeLifecycleContextFixture(t, func(context.Context) error { return nil })
			contended := lock
			if scenario == "executor" {
				contended = &executor.mu
			}
			contended.Lock()
			defer contended.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- executor.Handle(ctx, RuntimeResume, "") }()
			select {
			case err := <-result:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("contended transition lost deadline", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("transition did not cancel lock acquisition")
			}
			if engine.stops != 0 || engine.resumes != 0 || engine.suspended {
				t.Fatal("cancelled lock waiter touched engine")
			}
			if contended.TryLock() {
				contended.Unlock()
				t.Fatal("cancelled waiter unlocked a foreign owner")
			}
		})
	}
}

func TestRuntimeLifecycleCancelledResumeRetainsPreviouslyHeldGate(t *testing.T) {
	executor, engine, lock, _ := runtimeLifecycleContextFixture(t, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err := executor.Handle(t.Context(), RuntimeSuspend, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if err := executor.Handle(ctx, RuntimeResume, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("resume did not retain transition deadline", err)
	}
	if !engine.suspended || engine.resumes != 0 || !executor.held {
		t.Fatal("cancelled resume opened a suspended runtime")
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("transition timeout released previously held gate")
	}
}

func TestRuntimeLifecycleLifetimeCancelsIndependentTransition(t *testing.T) {
	entered := make(chan struct{})
	executor, engine, lock, cancelLifetime := runtimeLifecycleContextFixture(t, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancelTransition := context.WithCancel(context.Background())
	defer cancelTransition()
	result := make(chan error, 1)
	go func() { result <- executor.Handle(ctx, RuntimeResume, "") }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("resume never entered policy refresh")
	}
	cancelLifetime()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("lifetime cancellation lost", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("independent transition outlived runtime cancellation")
	}
	if ctx.Err() != nil {
		t.Fatal("executor cancelled caller-owned context")
	}
	if !engine.suspended || engine.resumes != 0 {
		t.Fatal("lifetime cancellation resumed runtime")
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("cancelled in-flight resume released safety gate")
	}
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	if !lock.TryLock() {
		t.Fatal("shutdown failed to release held operation lock")
	}
	lock.Unlock()
}
