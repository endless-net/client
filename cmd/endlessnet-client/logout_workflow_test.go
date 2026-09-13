package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

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
