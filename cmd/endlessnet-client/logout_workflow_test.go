package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestNativeLogoutResumesAfterSessionFailureWithoutRepeatingNodeRevocation(t *testing.T) {
	var nodeCalls, sessionCalls atomic.Int32
	var failSession atomic.Bool
	failSession.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/nodes/node-1":
			nodeCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/auth/logout":
			sessionCalls.Add(1)
			if failSession.Load() {
				http.Error(w, "synthetic session failure", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			t.Error("unexpected remote cleanup request")
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node-1", Token: "synthetic-session", LocalOwnerID: "owner"}
	before := cfg
	before.ControlPlaneURLs = append([]string(nil), cfg.ControlPlaneURLs...)
	var persisted []byte
	checkpoint := func(progress client.ClientRPCLogoutProgress) error {
		var err error
		persisted, err = json.Marshal(progress)
		return err
	}
	_, err := agentRPCLogout(t.Context(), cfg, client.ClientRPCLogoutProgress{}, checkpoint)
	if err == nil || nodeCalls.Load() != 1 || sessionCalls.Load() != 1 {
		t.Fatal("session failure did not follow exactly one node revocation")
	}
	// Recreate worker progress from the serialized checkpoint, as on restart.
	var resumed client.ClientRPCLogoutProgress
	if err := json.Unmarshal(persisted, &resumed); err != nil {
		t.Fatal(err)
	}
	if !resumed.NodeRevoked || resumed.SessionRevoked {
		t.Fatal("failure lost confirmed node progress or falsely confirmed session cleanup")
	}
	failSession.Store(false)
	if _, err := agentRPCLogout(t.Context(), cfg, resumed, checkpoint); err != nil {
		t.Fatal("session cleanup did not resume")
	}
	if err := json.Unmarshal(persisted, &resumed); err != nil {
		t.Fatal(err)
	}
	if !resumed.NodeRevoked || !resumed.SessionRevoked || nodeCalls.Load() != 1 || sessionCalls.Load() != 2 {
		t.Fatal("resumed cleanup repeated node revocation or lost session confirmation")
	}
	// A resumed fully confirmed provider must perform no further remote effects.
	if _, err := agentRPCLogout(t.Context(), cfg, resumed, checkpoint); err != nil || nodeCalls.Load() != 1 || sessionCalls.Load() != 2 {
		t.Fatal("confirmed cleanup repeated a remote effect")
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Fatal("remote provider changed local configuration before caller commit")
	}
}

func TestTypedRemoteLogoutResumesConfirmedNodeAndStopsOnCheckpointFailure(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkpoint failure", true: "resume session"}[resumed], func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"ok"}`))
			}))
			defer server.Close()
			failure := errors.New("checkpoint rejected")
			err := revokeConfiguredClient(t.Context(), client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node-1", Token: "synthetic-session"}, client.ClientRPCLogoutProgress{NodeRevoked: resumed}, func(progress client.ClientRPCLogoutProgress) error {
				if !resumed {
					return failure
				}
				if !progress.NodeRevoked || !progress.SessionRevoked {
					t.Fatal("resume checkpoint incomplete")
				}
				return nil
			})
			if !resumed && !errors.Is(err, failure) || resumed && err != nil {
				t.Fatal("unexpected checkpoint result", err)
			}
			want := "/nodes/node-1"
			if resumed {
				want = "/auth/logout"
			}
			if len(calls) != 1 || calls[0] != want {
				t.Fatal("confirmed or unpersisted step was repeated/advanced")
			}
		})
	}
}

func TestTypedRemoteLogoutPreservesLocalStateUntilCallerCommit(t *testing.T) {
	for _, stage := range []string{"node failure", "session failure", "confirmed"} {
		t.Run(stage, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.URL.Path)
				if r.Method == http.MethodDelete && r.URL.Path == "/nodes/node-1" {
					if stage == "node failure" {
						writeRecoveryPublicError(t, w, clientapi.ErrorCodeTemporarilyUnavailable, "revoke-correlation")
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/auth/logout" {
					if stage == "session failure" {
						http.Error(w, "synthetic failure", http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"status":"ok"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "client.json")
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node-1", Token: "synthetic-session", LocalOwnerID: "owner"}
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			_, err := agentRPCLogout(t.Context(), cfg, client.ClientRPCLogoutProgress{}, func(client.ClientRPCLogoutProgress) error { return nil })
			if (err == nil) != (stage == "confirmed") {
				t.Fatalf("unexpected cleanup result: %v", err)
			}
			if stage == "node failure" {
				var remote remoteCleanupError
				if !errors.As(err, &remote) || remote.RequestID != "revoke-correlation" || len(calls) != 1 {
					t.Fatal("revocation failure lost correlation or attempted session logout")
				}
			} else if len(calls) != 2 || calls[0] != "/nodes/node-1" || calls[1] != "/auth/logout" {
				t.Fatal("remote cleanup order changed")
			}
			saved, loadErr := client.LoadConfig(path)
			if loadErr != nil || saved.Token != cfg.Token || saved.NodeID != cfg.NodeID || saved.LocalOwnerID != cfg.LocalOwnerID {
				t.Fatal("remote workflow changed local state")
			}
		})
	}
}

func TestTypedRemoteLogoutCancelsInflightSessionRequest(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- revokeConfiguredClient(ctx, client.Config{ControlPlaneURLs: []string{server.URL}, Token: "synthetic-session"}, client.ClientRPCLogoutProgress{}, func(client.ClientRPCLogoutProgress) error { return nil })
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("logout request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("runtime cancellation not preserved", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not stop")
	}
}
