package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

func TestAgentAppliesHeartbeatAheadOfLastSnapshotBeforeStreaming(t *testing.T) {
	key := testMapSigningKey(t)
	projection := testNetworkMapWithRevision(t, key, "net-1", "node-1", 14)
	var streamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, projection.MapSignature)))
		case "/nodes/node-1/endpoint":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projection)
		case "/maps/node-1/stream":
			streamCalls.Add(1)
			http.Error(w, "stream unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, PrivateKey: "synthetic-private-key",
		NodeID: "node-1", NodeCredential: "synthetic-credential", NetworkID: "net-1",
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, projection.MapSignature))}
	cacheNetworkMap(&cfg, projection)
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	_, got, unchanged, err := agentOnlineNetworkMap(path, 200*time.Millisecond, 13)
	if err != nil {
		t.Fatal("verified pending projection was blocked on stream", err)
	}
	if unchanged || got.Network.Revision != 14 || streamCalls.Load() != 0 {
		t.Fatal("heartbeat ahead of applied snapshot was not returned immediately")
	}
}
