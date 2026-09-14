package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestLifecycleObservationReplacesOldPathsWithoutControlProbe(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, client.Config{NodeID: "node", NetworkID: "network", ControlPlaneURLs: []string{server.URL}, ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"}}); err != nil {
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
	state := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(state, []byte(`{"paths":[{"selected_path":"direct"}],"last_error":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := agentIPCOptions{ConfigStore: store, ConfigPath: path, StateOutput: state}
	for _, stopped := range []bool{false, true} {
		var failure error
		if !stopped {
			failure = errors.New("sensitive source body")
		}
		if err := observeAgentRuntimeLifecycle(t.Context(), mutations, opts, stopped, failure); err != nil {
			t.Fatal(err)
		}
		snapshot, err := client.LoadAgentSnapshot(state)
		if err != nil || len(snapshot.Paths) != 0 || snapshot.Apply != nil || snapshot.WireGuard != nil {
			t.Fatal("old dataplane results survived", err)
		}
		raw, err := os.ReadFile(state)
		if err != nil || strings.Contains(string(raw), "sensitive") {
			t.Fatal("raw failure body persisted", err)
		}
		if stopped && snapshot.LastError != "" {
			t.Fatal("successful retry retained failure")
		}
	}
	if calls.Load() != 0 || store.Read().ConnectionIntent.DesiredState != client.ConnectionIntentDesiredConnected {
		t.Fatal("observation probed control or changed intent")
	}
	status := buildAgentRPCStatusWithProbe(t.Context(), opts, store.Read(), ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, false)
	if status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || status.UserDisconnected {
		t.Fatal("stopped dataplane was confused with disconnected intent")
	}
}
