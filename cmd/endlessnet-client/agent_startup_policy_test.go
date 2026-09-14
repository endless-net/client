package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

func TestStartupPolicyFetchAuthenticatesAndRejectsChangedIntent(t *testing.T) {
	for _, scenario := range []string{"success", "tampered", "disconnect", "cancelled", "retry", "retry_disconnect"} {
		t.Run(scenario, func(t *testing.T) {
			projection := testNetworkMapWithRevision(t, testMapSigningKey(t), "net-1", "node-1", 14)
			var store *client.ConfigStore
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/server-key":
					_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, projection.MapSignature)))
				case "/maps/node-1/stream":
					setTestMapStreamResponseHeaders(w)
					if scenario == "disconnect" || scenario == "retry_disconnect" {
						if err := client.NewConnectionIntentStore(store).SetDisconnected("user_disconnect"); err != nil {
							t.Error(err)
						}
					}
					response := testMapStreamSnapshotEvent(t, projection)
					if scenario == "tampered" {
						response.Snapshot.Network.Name = "tampered"
					}
					w.Header().Set("Content-Type", "application/x-ndjson")
					_ = json.NewEncoder(w).Encode(response)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "client.json")
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NodeID: "node-1", NetworkID: "net-1", NodeCredential: "synthetic-credential", MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, projection.MapSignature)), ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"}}
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			var err error
			store, err = client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Read()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if scenario == "retry" || scenario == "retry_disconnect" {
				if err := client.NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
				err = retryAgentStartupPolicy(ctx, nil, agentIPCOptions{ConfigStore: store}, time.Second)
			} else {
				err = refreshAgentStartupPolicy(ctx, store, time.Second)
			}
			after := store.Read()
			if scenario == "success" || scenario == "retry" {
				if err != nil || after.CachedMap == nil || after.MapRevision != 14 || !reflect.DeepEqual(before.ConnectionIntent, after.ConnectionIntent) {
					t.Fatal("startup source was not adopted independently of intent", err)
				}
				if err := client.NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
				if store.Read().ConnectionIntent.DesiredState != client.ConnectionIntentDesiredConnected {
					t.Fatal("KEEP_INTENT could not use freshly authenticated map")
				}
			} else {
				if err == nil || after.CachedMap != nil || after.MapRevision != before.MapRevision {
					t.Fatal("failed or stale fetch mutated map authority", err)
				}
				if (scenario == "disconnect" || scenario == "retry_disconnect") && (after.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected || after.ConnectionIntent.Reason != "user_disconnect") {
					t.Fatal("stale fetch overwrote disconnect")
				}
			}
		})
	}
}
