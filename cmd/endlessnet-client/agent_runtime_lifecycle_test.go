package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

type agentLifecycleTestEngine struct {
	lock     *sync.Mutex
	stopped  chan struct{}
	resumed  chan struct{}
	t        *testing.T
	failOnce bool
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
	for _, scenario := range []string{"resume", "suspend_retry", "cancel_suspended", "source_closed"} {
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
			events := make(chan client.RuntimeLifecycleEvent, 4)
			wake := make(chan struct{}, 1)
			engine := &agentLifecycleTestEngine{lock: lock, stopped: make(chan struct{}, 4), resumed: make(chan struct{}, 4), t: t}
			engine.failOnce = scenario == "suspend_retry"
			stop, err := startAgentRuntimeLifecycle(ctx, cancel, nil, agentIPCOptions{ConfigStore: store, OperationMu: lock, SyncWake: wake}, events, engine)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := stop(); err != nil {
					t.Error(err)
				}
			}()
			events <- client.RuntimeSuspend
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
			if scenario == "suspend_retry" {
				select {
				case <-engine.stopped:
				case <-time.After(5 * time.Second):
					t.Fatal("failed suspend was not retried")
				}
			}
			switch scenario {
			case "resume", "suspend_retry":
				events <- client.RuntimeResume
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
			if scenario != "resume" && scenario != "suspend_retry" && len(engine.resumed) != 0 {
				t.Fatal("shutdown resumed networking")
			}
		})
	}
}
