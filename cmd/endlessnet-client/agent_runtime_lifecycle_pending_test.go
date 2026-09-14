package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// Pause confirmed teardown so the test can change authority/ownership before
// resume resolves a previously unavailable logoff decision.
type pendingLogoffTestEngine struct {
	t         *testing.T
	lock      *sync.Mutex
	store     *client.ConfigStore
	suspended chan struct{}
	allow     chan struct{}
	resumed   chan client.Config
}

func (e *pendingLogoffTestEngine) Suspend(ctx context.Context) (client.WireGuardApplyResult, error) {
	select {
	case e.suspended <- struct{}{}:
	case <-ctx.Done():
		return client.WireGuardApplyResult{}, ctx.Err()
	}
	select {
	case <-e.allow:
		return client.WireGuardApplyResult{OK: true}, nil
	case <-ctx.Done():
		return client.WireGuardApplyResult{}, ctx.Err()
	}
}

func (e *pendingLogoffTestEngine) Resume(context.Context) error {
	if e.lock.TryLock() {
		e.lock.Unlock()
		e.t.Error("resume opened engine outside worker barrier")
	}
	e.resumed <- e.store.Read()
	return nil
}

func (e *pendingLogoffTestEngine) Down(context.Context) (client.WireGuardApplyResult, error) {
	e.t.Error("unresolved logoff performed teardown before policy resolution")
	return client.WireGuardApplyResult{OK: true}, nil
}

func TestAgentResumeResolvesPendingLogoffBeforeOpeningEngine(t *testing.T) {
	for _, scenario := range []string{"recovered_policy", "replaced_owner", "failed_refresh_then_recovery"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			if err := client.SaveConfig(path, client.Config{
				LocalOwnerID: "owner-SID", NodeID: "test-node",
				ControlPlaneURLs: []string{"https://control.example.test"},
				ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"},
			}); err != nil {
				t.Fatal(err)
			}
			store, err := client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			mutations, err := client.NewClientRPCMutations(store)
			if err != nil {
				t.Fatal(err)
			}
			if err := mutations.AdoptInitialProfile(); err != nil {
				t.Fatal(err)
			}
			if err := store.Update(func(cfg *client.Config) error {
				profile := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
				profile.UserLogoff = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
				cfg.RPCState.Profiles[profile.ID] = profile
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			deadline, end := context.WithTimeout(t.Context(), 5*time.Second)
			defer end()
			ctx, cancel := context.WithCancelCause(deadline)
			defer cancel(nil)
			lock := &sync.Mutex{}
			engine := &pendingLogoffTestEngine{t: t, lock: lock, store: store, suspended: make(chan struct{}), allow: make(chan struct{}), resumed: make(chan client.Config, 2)}
			events := make(chan client.RuntimeLifecycleNotification, 4)
			wake := make(chan struct{}, 2)
			stop, err := startAgentRuntimeLifecycle(ctx, cancel, mutations, agentIPCOptions{ConfigStore: store, OperationMu: lock, SyncWake: wake, Offline: true}, events, engine)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := stop(); err != nil {
					t.Error(err)
				}
			}()
			awaitSuspend := func() {
				t.Helper()
				select {
				case <-engine.suspended:
				case <-ctx.Done():
					t.Fatal("resume did not enter teardown", context.Cause(ctx))
				}
				if lock.TryLock() {
					lock.Unlock()
					t.Fatal("resume released worker barrier")
				}
			}
			release := func() {
				t.Helper()
				select {
				case engine.allow <- struct{}{}:
				case <-ctx.Done():
					t.Fatal("teardown not released", context.Cause(ctx))
				}
			}
			// No signed map: logoff cannot decide intent and must remain pending.
			events <- client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "owner-SID"}
			events <- client.RuntimeLifecycleNotification{Event: client.RuntimeResume}
			awaitSuspend()
			if store.Read().ConnectionIntent.DesiredState != client.ConnectionIntentDesiredConnected {
				t.Fatal("unavailable policy changed intent")
			}
			if scenario == "failed_refresh_then_recovery" {
				release()
				events <- client.RuntimeLifecycleNotification{Event: client.RuntimeResume}
				awaitSuspend() // Previous refresh completed with unavailable authority.
				if len(engine.resumed) != 0 || len(wake) != 0 {
					t.Fatal("failed refresh opened networking")
				}
			}
			// Return to a local-only profile while stopped. Its explicit logoff
			// preference becomes resolvable without a network request.
			if err := store.Update(func(cfg *client.Config) error {
				cfg.NodeID = ""
				if scenario == "replaced_owner" {
					cfg.LocalOwnerID = "new-owner-SID"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			release()
			var resumed client.Config
			select {
			case resumed = <-engine.resumed:
			case <-ctx.Done():
				t.Fatal("resolved resume did not open engine", context.Cause(ctx))
			}
			wantState, wantReason := client.ConnectionIntentDesiredDisconnected, "runtime_user_logoff"
			if scenario == "replaced_owner" {
				wantState, wantReason = client.ConnectionIntentDesiredConnected, "user_connect"
			}
			if resumed.ConnectionIntent.DesiredState != wantState || resumed.ConnectionIntent.Reason != wantReason {
				t.Fatal("engine opened before current owner logoff decision was committed")
			}
			select {
			case <-wake:
			case <-ctx.Done():
				t.Fatal("resolved resume did not wake reconciliation")
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			if !lock.TryLock() {
				t.Fatal("shutdown retained worker barrier")
			}
			lock.Unlock()
		})
	}
}
