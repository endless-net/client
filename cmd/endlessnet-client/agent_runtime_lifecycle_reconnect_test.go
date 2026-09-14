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

func TestAgentFailedLogoffTeardownDoesNotReplayOverNewConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, client.Config{LocalOwnerID: "owner-SID", ControlPlaneURLs: []string{"https://control.example.test"}, ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "original_connect"}}); err != nil {
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
	engine := &agentLifecycleTestEngine{lock: lock, stopped: make(chan struct{}, 8), resumed: make(chan struct{}, 8), t: t, failOnce: true}
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
	awaitWake := func() {
		t.Helper()
		select {
		case <-wake:
		case <-ctx.Done():
			t.Fatal("reconciliation was not woken", context.Cause(ctx))
		}
	}
	events <- client.RuntimeLifecycleNotification{Event: client.RuntimeUserLogoff, SessionOwner: "owner-SID"}
	awaitWake() // Intent committed; first Down failed and yielded to reconciliation.
	if intent := store.Read().ConnectionIntent; intent.DesiredState != client.ConnectionIntentDesiredDisconnected || intent.Reason != "runtime_user_logoff" {
		t.Fatal("logoff did not commit before failed teardown")
	}
	// Model a newer successful Connect intent commit after logoff yielded.
	if err := store.Update(func(cfg *client.Config) error {
		cfg.ConnectionIntent = &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "new_user_connect"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	events <- client.RuntimeLifecycleNotification{Event: client.RuntimeResume}
	awaitWake()
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if intent := store.Read().ConnectionIntent; intent.DesiredState != client.ConnectionIntentDesiredConnected || intent.Reason != "new_user_connect" {
		t.Fatal("failed logoff effect replayed its committed policy over newer connect")
	}
	if len(engine.stopped) != 2 || len(engine.resumed) != 1 {
		t.Fatal("resume did not confirm teardown exactly once before opening engine")
	}
}
