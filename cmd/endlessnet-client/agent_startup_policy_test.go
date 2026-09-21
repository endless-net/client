package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

type startupOrderEngine struct {
	testAgentWireGuard
	order   []string
	failure error
	cancel  context.CancelFunc
}

func (e *startupOrderEngine) RestoreExitProtection(context.Context, client.Config) error {
	e.order = append(e.order, "guard")
	return e.failure
}

func (e *startupOrderEngine) ControlPlaneHTTPClient(client.Config) (*http.Client, error) {
	e.order = append(e.order, "policy")
	if e.cancel != nil {
		e.cancel()
	}
	return nil, errors.New("test policy source unavailable")
}

func TestAgentStartupRestoresGuardBeforePolicyAndIntent(t *testing.T) {
	for _, phase := range []string{"ready", "guard_failure", "recovery_error_text", "cancelled"} {
		path := filepath.Join(t.TempDir(), "client.json")
		cfg := client.Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"}}
		if err := client.SaveConfig(path, cfg); err != nil {
			t.Fatal(err)
		}
		store, err := client.OpenConfigStore(path)
		if err != nil {
			t.Fatal(err)
		}
		before := store.Read()
		engine := &startupOrderEngine{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if phase == "cancelled" {
			engine.cancel = cancel
		}
		if phase == "guard_failure" {
			engine.failure = errors.New("guard failed")
		}
		if phase == "recovery_error_text" {
			// Text alone is not proof of observed native containment.
			engine.failure = errors.New("exit protection restored without control authority")
		}
		err = initializeAgentStartup(ctx, engine, store, time.Second, false)
		if phase == "guard_failure" || phase == "recovery_error_text" {
			if !errors.Is(err, engine.failure) || !reflect.DeepEqual(engine.order, []string{"guard"}) || !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("failed guard allowed policy or intent effects")
			}
		} else if phase == "cancelled" {
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("cancelled startup changed intent")
			}
		} else if err != nil || !reflect.DeepEqual(engine.order, []string{"guard", "policy"}) {
			t.Fatalf("startup order=%v error=%v", engine.order, err)
		}
	}
}

func TestStartupAndResumePolicyDoNotBypassEngineRefusal(t *testing.T) {
	for _, phase := range []string{"startup", "retry", "resume"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			cfg := client.Config{ControlPlaneURLs: []string{"https://control.example"}, NodeID: "node", NetworkID: "network", NodeCredential: "synthetic-credential", ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect"}}
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			store, err := client.OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "retry" {
				if err := client.NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
			}
			before := store.Read()
			failure := errors.New("exit guard recovery required")
			engine := &refusingControlEngine{failure: failure}
			switch phase {
			case "startup":
				err = refreshAgentStartupPolicy(t.Context(), engine, store, time.Second)
			case "retry":
				err = retryAgentStartupPolicy(t.Context(), nil, agentIPCOptions{ConfigStore: store, WireGuard: engine}, time.Second)
			case "resume":
				err = refreshAgentPolicySnapshot(t.Context(), engine, store, time.Second, func(client.Config, client.Config) error { t.Error("refused fetch committed policy"); return nil })
			}
			if !errors.Is(err, failure) || engine.calls != 1 {
				t.Fatalf("factory calls=%d error=%v", engine.calls, err)
			}
			if !reflect.DeepEqual(before, store.Read()) {
				t.Fatal("refused fetch changed durable state")
			}
		})
	}
}

func TestStartupPolicyFetchAuthenticatesAndRejectsChangedIntent(t *testing.T) {
	for _, scenario := range []string{"success", "tampered", "disconnect", "cancelled", "retry", "retry_disconnect", "resume_disconnected"} {
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
			if scenario == "resume_disconnected" {
				cfg.ConnectionIntent = &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
			}
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
			switch scenario {
			case "resume_disconnected":
				mutations, mutationErr := client.NewClientRPCMutations(store)
				if mutationErr != nil {
					t.Fatal(mutationErr)
				}
				err = refreshAgentPolicySnapshot(ctx, nil, store, time.Second, func(before, candidate client.Config) error {
					return mutations.RefreshRuntimeLifecyclePolicy(ctx, before, candidate)
				})
			case "retry", "retry_disconnect":
				if err := client.NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
				err = retryAgentStartupPolicy(ctx, nil, agentIPCOptions{ConfigStore: store}, time.Second)
			default:
				err = refreshAgentStartupPolicy(ctx, nil, store, time.Second)
			}
			after := store.Read()
			if scenario == "success" || scenario == "retry" || scenario == "resume_disconnected" {
				if err != nil || after.CachedMap == nil || after.MapRevision != 14 || !reflect.DeepEqual(before.ConnectionIntent, after.ConnectionIntent) {
					t.Fatal("startup source was not adopted independently of intent", err)
				}
				if err := client.NewConnectionIntentStore(store).InitializeRuntimeIntent(); err != nil {
					t.Fatal(err)
				}
				if store.Read().ConnectionIntent.DesiredState != before.ConnectionIntent.DesiredState {
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
