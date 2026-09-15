package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

type refusingControlEngine struct {
	testAgentWireGuard
	failure error
	calls   int
}

func (e *refusingControlEngine) ControlPlaneHTTPClient(client.Config) (*http.Client, error) {
	e.calls++
	return nil, e.failure
}

func TestAgentDoesNotBypassRefusedEngineControlTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	cfg := client.Config{ControlPlaneURLs: []string{"https://control.example"}, NodeID: "node", NodeCredential: "synthetic-credential", NetworkID: "network"}
	if err := client.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("protected underlay is not restored")
	engine := &refusingControlEngine{failure: failure}
	_, _, err := runAgentIteration(t.Context(), agentIterationOptions{ConfigPath: path, WireGuard: engine, Timeout: time.Second})
	if !errors.Is(err, failure) || engine.calls != 1 {
		t.Fatalf("factory calls=%d error=%v", engine.calls, err)
	}
	_, err = updatePublishedEndpoint(t.Context(), engine, path, time.Second, []string{"198.51.100.1:51820"}, false, 0, &endpointUpdateState{}, 0, time.Now())
	if !errors.Is(err, failure) || engine.calls != 2 {
		t.Fatalf("endpoint factory calls=%d error=%v", engine.calls, err)
	}
}

func TestAgentIterationCancellationStopsControlResponseBody(t *testing.T) {
	for _, blockedPath := range []string{"/server-key", "/maps/node-1/stream", "/nodes/node-1/endpoint"} {
		t.Run(blockedPath, func(t *testing.T) {
			key := testMapSigningKey(t)
			projection := testNetworkMapWithRevision(t, key, "net-1", "node-1", 14)
			started, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == blockedPath {
					if blockedPath != "/maps/node-1/stream" {
						w.Header().Set("Content-Type", "application/json")
					} else {
						w.Header().Set("Content-Type", "application/x-ndjson")
					}
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					close(stopped)
					return
				}
				if r.URL.Path == "/server-key" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, projection.MapSignature)))
					return
				}
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}))
			defer server.Close()
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}, PrivateKey: "synthetic-private-key", NodeID: "node-1", NodeCredential: "synthetic-credential", NetworkID: "net-1", MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, projection.MapSignature))}
			cacheNetworkMap(&cfg, projection)
			if blockedPath != "/nodes/node-1/endpoint" {
				cfg.NodeApprovalState = "pending"
			} // No online heartbeat before the stream.
			path := filepath.Join(t.TempDir(), "client.json")
			if err := client.SaveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if blockedPath == "/nodes/node-1/endpoint" {
					_, callErr := updatePublishedEndpoint(ctx, nil, path, 30*time.Second, []string{"198.51.100.1:51820"}, false, 0, &endpointUpdateState{}, 0, time.Now())
					result <- callErr
					return
				}
				_, _, callErr := runAgentIteration(ctx, agentIterationOptions{ConfigPath: path, Timeout: 30 * time.Second, FromRevision: 14})
				result <- callErr
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("agent did not start expected request")
			}
			cancel()
			select {
			case err = <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("agent ignored runtime cancellation")
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("old HTTP response remained active")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("cancelled map iteration changed durable configuration")
			}
		})
	}
}
