package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestPowerRetryPreservesNewIntentAfterDecisionCommitted(t *testing.T) {
	for _, scenario := range []string{"suspend_down", "suspend_observation", "resume_observation"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			event, opposite := RuntimeSuspend, RuntimeResume
			if scenario == "resume_observation" {
				event, opposite = opposite, event
			}
			if err := m.store.Update(func(cfg *Config) error {
				profile := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
				if event == RuntimeSuspend {
					profile.Suspend = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
				} else {
					profile.Resume = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
				}
				cfg.RPCState.Profiles[profile.ID] = profile
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "first_connect"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			lock := &sync.Mutex{}
			engine := &testRuntimeLifecycleEngine{lock: lock, t: t, fail: scenario == "suspend_down"}
			observationFailed := false
			executor, err := NewRuntimeLifecycleExecutor(ctx, m, engine, lock, func() {}, func(context.Context) error { return nil }, func(context.Context, bool, error) error {
				if scenario != "suspend_down" && !observationFailed {
					observationFailed = true
					return errors.New("observation temporarily unavailable")
				}
				return nil
			})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			defer func() {
				cancel()
				if err := executor.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := executor.Handle(ctx, event, ""); err == nil {
				t.Fatal("missing initial effect failure")
			}
			if m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("policy was not committed before effect failure")
			}
			if lock.TryLock() {
				lock.Unlock()
				t.Fatal("failed power transition released workers")
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "new_user_connect"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			engine.fail = false
			if err := executor.Handle(ctx, event, ""); err != nil {
				t.Fatal(err)
			}
			if intent := m.store.Read().ConnectionIntent; intent.DesiredState != ConnectionIntentDesiredConnected || intent.Reason != "new_user_connect" {
				t.Fatal("power retry replayed committed decision over newer intent")
			}
			// A completed cycle must not suppress policy for the next OS event.
			if err := executor.Handle(ctx, opposite, ""); err != nil {
				t.Fatal(err)
			}
			if err := executor.Handle(ctx, event, ""); err != nil {
				t.Fatal(err)
			}
			if m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("next power transition skipped its new decision")
			}
		})
	}
}
