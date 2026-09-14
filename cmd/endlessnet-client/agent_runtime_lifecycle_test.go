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

type agentLifecycleTestEngine struct {
	lock     *sync.Mutex
	stopped  chan struct{}
	resumed  chan struct{}
	t        *testing.T
	failOnce bool
}

func TestAgentLogoffUsesSessionOwnerBeforeDisconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, client.Config{LocalOwnerID: "owner-SID", ControlPlaneURLs: []string{"https://control.example.test"}, ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"}}); err != nil {
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
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	lock := &sync.Mutex{}
	engine := &agentLifecycleTestEngine{lock: lock, stopped: make(chan struct{}, 4), resumed: make(chan struct{}, 4), t: t}
	events := make(chan client.RuntimeLifecycleNotification, 4)
	wake := make(chan struct{}, 4)
	stop, err := startAgentRuntimeLifecycle(ctx, cancel, mutations, agentIPCOptions{ConfigStore: store, OperationMu: lock, SyncWake: wake}, events, engine)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	events <- client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "foreign-SID"}
	events <- client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "owner-SID"}
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("owner logoff did not wake disconnected reconciliation")
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	intent := store.Read().ConnectionIntent
	if len(engine.stopped) != 1 || intent.DesiredState != client.ConnectionIntentDesiredDisconnected || intent.Reason != "runtime_user_logoff" {
		t.Fatal("logoff did not apply exactly one owner-bound disconnect")
	}
}

func (e *agentLifecycleTestEngine) Suspend(context.Context) (client.WireGuardApplyResult, error) {
	if e.lock.TryLock() {
		e.lock.Unlock()
		e.t.Error("suspend outside shared operation lock")
	}
	e.stopped <- struct{}{}
	if e.failOnce {
		e.failOnce = false
		return client.WireGuardApplyResult{OK: false}, nil
	}
	return client.WireGuardApplyResult{OK: true}, nil
}
func (e *agentLifecycleTestEngine) Resume(context.Context) error {
	e.resumed <- struct{}{}
	return nil
}
func (e *agentLifecycleTestEngine) Down(ctx context.Context) (client.WireGuardApplyResult, error) {
	return e.Suspend(ctx)
}

func TestAgentRuntimeLifecycleBindingAndShutdown(t *testing.T) {
	for _, scenario := range []string{"resume", "suspend_retry", "suspend_retry_foreign_logoff", "cancel_suspended", "source_closed"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			if err := client.SaveConfig(path, client.Config{}); err != nil {
				t.Fatal(err)
			}
			store, err := client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			lock := &sync.Mutex{}
			events := make(chan client.RuntimeLifecycleNotification, 4)
			wake := make(chan struct{}, 1)
			engine := &agentLifecycleTestEngine{lock: lock, stopped: make(chan struct{}, 4), resumed: make(chan struct{}, 4), t: t}
			retries := scenario == "suspend_retry" || scenario == "suspend_retry_foreign_logoff"
			engine.failOnce = retries
			stop, err := startAgentRuntimeLifecycle(ctx, cancel, nil, agentIPCOptions{ConfigStore: store, OperationMu: lock, SyncWake: wake}, events, engine)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := stop(); err != nil {
					t.Error(err)
				}
			}()
			events <- client.RuntimeLifecycleNotification{Event: client.RuntimeSuspend}
			select {
			case <-engine.stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("suspend was not executed")
			}
			if lock.TryLock() {
				lock.Unlock()
				t.Fatal("suspend released workers")
			}
			intent := store.Read().ConnectionIntent
			if intent == nil || intent.DesiredState != client.ConnectionIntentDesiredDisconnected {
				t.Fatal("no-intent default was not persisted")
			}
			if scenario == "suspend_retry_foreign_logoff" {
				events <- client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "foreign-SID"}
			}
			if retries {
				select {
				case <-engine.stopped:
				case <-time.After(5 * time.Second):
					t.Fatal("failed suspend was not retried")
				}
			}
			switch scenario {
			case "resume", "suspend_retry", "suspend_retry_foreign_logoff":
				events <- client.RuntimeLifecycleNotification{Event: client.RuntimeResume}
				select {
				case <-wake:
				case <-time.After(5 * time.Second):
					t.Fatal("resume did not wake reconciliation")
				}
				if len(engine.resumed) != 1 {
					t.Fatal("resume not executed")
				}
			case "source_closed":
				close(events)
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("source closure did not cancel runtime")
				}
			default:
				cancel(nil)
			}
			if err := stop(); err != nil {
				t.Fatal(err)
			}
			if !lock.TryLock() {
				t.Fatal("shutdown retained worker lock")
			}
			lock.Unlock()
			if scenario != "resume" && !retries && len(engine.resumed) != 0 {
				t.Fatal("shutdown resumed networking")
			}
		})
	}
}
