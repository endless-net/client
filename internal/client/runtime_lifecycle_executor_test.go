package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

type testRuntimeLifecycleEngine struct {
	lock      *sync.Mutex
	suspended bool
	fail      bool
	stops     int
	resumes   int
	t         *testing.T
}

func (e *testRuntimeLifecycleEngine) assertLocked() {
	e.t.Helper()
	if e.lock.TryLock() {
		e.lock.Unlock()
		e.t.Fatal("lifecycle effect ran outside shared lock")
	}
}
func (e *testRuntimeLifecycleEngine) Down(context.Context) (WireGuardApplyResult, error) {
	e.assertLocked()
	e.stops++
	return WireGuardApplyResult{OK: !e.fail}, nil
}
func (e *testRuntimeLifecycleEngine) Suspend(ctx context.Context) (WireGuardApplyResult, error) {
	e.suspended = true
	return e.Down(ctx)
}
func (e *testRuntimeLifecycleEngine) Resume(context.Context) error {
	e.assertLocked()
	e.resumes++
	if e.fail {
		return errors.New("cleanup still pending")
	}
	e.suspended = false
	return nil
}

func TestRuntimeLifecycleExecutorHoldsWorkersUntilVerifiedResume(t *testing.T) {
	for _, scenario := range []string{"keep", "disconnect_during_suspend", "suspend_failure", "resume_failure", "source_failure"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			lock := &sync.Mutex{}
			engine := &testRuntimeLifecycleEngine{lock: lock, t: t, fail: scenario == "suspend_failure"}
			wakes := 0
			executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() { wakes++ }, func(context.Context) error { return nil }, func(context.Context, bool, error) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				if err := executor.Close(); err != nil {
					t.Fatal(err)
				}
			}()
			err = executor.Handle(ctx, RuntimeSuspend, "")
			if (err != nil) != (scenario == "suspend_failure") {
				t.Fatal(err)
			}
			if lock.TryLock() {
				lock.Unlock()
				t.Fatal("suspension released workers")
			}
			if !engine.suspended || m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
				t.Fatal("suspend lost desired intent")
			}
			if scenario == "disconnect_during_suspend" {
				if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
					t.Fatal(err)
				}
			}
			original := m.store.Read().CachedMap
			if scenario == "source_failure" {
				if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Name = "tampered"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			engine.fail = scenario == "resume_failure"
			err = executor.Handle(ctx, RuntimeResume, "")
			if scenario == "resume_failure" || scenario == "source_failure" {
				if err == nil || wakes != 0 {
					t.Fatal("failed resume woke workers", err)
				}
				if lock.TryLock() {
					lock.Unlock()
					t.Fatal("failed resume released workers")
				}
				engine.fail = false
				if scenario == "source_failure" {
					if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap = original; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				if err := executor.Handle(ctx, RuntimeResume, ""); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !lock.TryLock() {
				t.Fatal("resume did not release workers")
			}
			lock.Unlock()
			if wakes != 1 || engine.suspended {
				t.Fatal("resume did not release engine gate")
			}
			if scenario == "disconnect_during_suspend" && m.store.Read().ConnectionIntent.Reason != "user_disconnect" {
				t.Fatal("resume replaced user disconnect")
			}
		})
	}
}

func TestRuntimeLifecycleExecutorRejectsForeignLogoffAndUnsafeClose(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t}
	executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() {}, func(context.Context) error { return nil }, func(context.Context, bool, error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Handle(ctx, RuntimeUserLogoff, "other"); err == nil || engine.stops != 0 {
		t.Fatal("foreign logoff affected engine", err)
	}
	if err := executor.Handle(ctx, RuntimeSuspend, ""); err != nil {
		t.Fatal(err)
	}
	if err := executor.Close(); err == nil {
		t.Fatal("live workers released during close")
	}
	cancel()
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	if !lock.TryLock() {
		t.Fatal("shutdown retained effect lock")
	}
	lock.Unlock()
	if !engine.suspended || engine.resumes != 0 {
		t.Fatal("shutdown resumed networking")
	}
	if err := executor.Handle(ctx, RuntimeResume, ""); err == nil {
		t.Fatal("closed executor accepted event")
	}
}

func TestRuntimeLifecycleSourceLossGatesWithoutChangingIntent(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
		profile := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
		profile.Suspend = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
		profile.Resume = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
		cfg.RPCState.Profiles[profile.ID] = profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t}
	executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() {}, func(context.Context) error { return nil }, func(context.Context, bool, error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = executor.Close() }()
	if err := executor.Handle(ctx, RuntimeSourceLost, ""); err != nil {
		t.Fatal(err)
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("source loss opened worker gate")
	}
	if intent := m.store.Read().ConnectionIntent; intent.DesiredState != ConnectionIntentDesiredConnected || intent.Reason != "user_connect" {
		t.Fatal("source health applied suspend preference", intent)
	}
	if err := executor.Handle(ctx, RuntimeSourceRecovered, ""); err != nil {
		t.Fatal(err)
	}
	if intent := m.store.Read().ConnectionIntent; intent.DesiredState != ConnectionIntentDesiredConnected || intent.Reason != "user_connect" {
		t.Fatal("source recovery applied resume preference", intent)
	}
	if !lock.TryLock() {
		t.Fatal("verified recovery did not release worker gate")
	}
	lock.Unlock()
}

func TestRuntimeResumeRefreshRunsInsideClosedGateAndRetries(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t}
	available := false
	wakes := 0
	executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() { wakes++ }, func(context.Context) error {
		engine.assertLocked()
		if !engine.suspended {
			t.Fatal("policy fetch ran before closing engine gate")
		}
		if !available {
			return errors.New("source temporarily unavailable")
		}
		return m.store.Update(func(cfg *Config) error {
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "newer_user_disconnect"}
			return nil
		})
	}, func(context.Context, bool, error) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := executor.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := executor.Handle(ctx, RuntimeResume, ""); err == nil || wakes != 0 || engine.resumes != 0 {
		t.Fatal("unavailable source released an unsolicited resume", err)
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("source failure released worker gate")
	}
	available = true
	if err := executor.Handle(ctx, RuntimeResume, ""); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || engine.resumes != 1 || m.store.Read().ConnectionIntent.Reason != "newer_user_disconnect" {
		t.Fatal("resume did not use the latest intent after refresh")
	}
}

func TestRuntimeLifecycleObservationPrecedesWorkerRelease(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	lock := &sync.Mutex{}
	engine := &testRuntimeLifecycleEngine{lock: lock, t: t, fail: true}
	wakes := 0
	observations := 0
	reject := false
	executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() { wakes++ }, func(context.Context) error { return nil }, func(_ context.Context, stopped bool, failure error) error {
		engine.assertLocked()
		observations++
		if stopped == engine.fail || (failure != nil) != engine.fail {
			t.Fatal("observation did not reflect confirmed teardown")
		}
		if reject {
			return errors.New("observation persistence unavailable")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := executor.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := executor.Handle(ctx, RuntimeSuspend, ""); err == nil || observations != 1 {
		t.Fatal("failed teardown not observed")
	}
	engine.fail = false
	if err := executor.Handle(ctx, RuntimeSuspend, ""); err != nil || observations != 2 {
		t.Fatal("successful teardown not observed", err)
	}
	reject = true
	if err := executor.Handle(ctx, RuntimeResume, ""); err == nil || wakes != 0 {
		t.Fatal("failed observation released resume")
	}
	if lock.TryLock() {
		lock.Unlock()
		t.Fatal("failed observation released workers")
	}
	reject = false
	if err := executor.Handle(ctx, RuntimeResume, ""); err != nil || wakes != 1 || observations != 4 {
		t.Fatal("observation recovery did not wake runtime", err)
	}
}
