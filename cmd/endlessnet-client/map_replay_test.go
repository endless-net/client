package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
	relay "github.com/endless-net/relay/protocol/v1"
)

func TestAgentRepeatedDeltaRefreshesRelayCredential(t *testing.T) {
	key := testMapSigningKey(t)
	credential, err := relay.Sign(key, "network", "node", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	m := api.RegisterNodeResponse{
		Revision: api.MapRevision{Network: 1},
		Network:  api.Network{ID: "network", Name: "network", CIDR: "198.18.94.0/24", Revision: 1},
		Node:     api.Node{ID: "node", NetworkID: "network", Hostname: "client", PublicKey: testWireGuardPublicKey("replay-client"), AssignedIP: "198.18.94.1"},
		Peers:    []api.Peer{{ID: "peer", Hostname: "peer", PublicKey: testWireGuardPublicKey("replay-peer"), AllowedIPs: []string{"198.18.94.20/32"}}},
		Relays:   []relay.Endpoint{{ID: "relay", Addr: "127.0.0.1:443", Protocol: relay.EndpointProtocolTLS}}, RelayCredential: credential,
	}
	m.MapSignature, err = api.SignNetworkMap(key, m)
	if err != nil {
		t.Fatal(err)
	}
	event := testMapStreamSnapshotEvent(t, m)
	m = networkMapResponseFromSnapshot(*event.Snapshot)
	m.MapSignature = event.ResultSignature
	publicKey := testMapSigningPublicKey(t, event.ResultSignature)
	var deltaRequests, snapshotRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, publicKey))
		case "/nodes/node/endpoint":
			_ = json.NewEncoder(w).Encode(m)
		case "/maps/node/stream":
			setTestMapStreamResponseHeaders(w)
			if r.URL.Query().Get("from_network_revision") == "1" {
				deltaRequests.Add(1)
				delta := event
				delta.Type, delta.Snapshot = "delta", nil
				delta.Delta = &api.MapDelta{Network: &m.Network}
				delta.BaseHash = event.ResultSignature.PayloadHash
				_ = json.NewEncoder(w).Encode(delta)
			} else {
				snapshotRequests.Add(1)
				_ = json.NewEncoder(w).Encode(event)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := client.Config{ControlPlaneURLs: []string{server.URL}, PrivateKey: "private-key", NodeCredential: "fixture-node-credential", MapSigningTrust: testSigningTrustBundle(t, publicKey)}
	if _, _, err := cacheNetworkMapFromEvent(&cfg, event); err != nil {
		t.Fatal("initial snapshot rejected")
	}
	path := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	gotConfig, got, _, err := agentOnlineNetworkMap(path, 100*time.Millisecond, 1)
	if err != nil {
		t.Fatal("agent failed repeated delta refresh")
	}
	if deltaRequests.Load() != 1 || snapshotRequests.Load() != 1 {
		t.Fatal("agent did not replace repeated delta with one full projection")
	}
	if got.Node.ID != m.Node.ID || len(got.Peers) != 1 || got.RelayCredential == nil || got.RelayCredential.Signature != credential.Signature {
		t.Fatal("repeated delta refresh lost runtime Relay configuration")
	}
	if gotConfig.CachedMap == nil || gotConfig.CachedMap.RelayCredential != nil {
		t.Fatal("repeated delta refresh persisted Relay credential")
	}
}

func TestRepeatedSnapshotRetainsRuntimeRelayConfiguration(t *testing.T) {
	key := testMapSigningKey(t)
	credential, err := relay.Sign(key, "network", "node", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	m := api.RegisterNodeResponse{
		Network: api.Network{ID: "network", Name: "network", CIDR: "198.18.94.0/24", Revision: 1},
		Node:    api.Node{ID: "node", NetworkID: "network", Hostname: "client", PublicKey: testWireGuardPublicKey("replay-client"), AssignedIP: "198.18.94.1"},
		Peers:   []api.Peer{{ID: "peer", Hostname: "peer", PublicKey: testWireGuardPublicKey("replay-peer"), AllowedIPs: []string{"198.18.94.20/32"}}},
		Relays:  []relay.Endpoint{{ID: "relay", Addr: "127.0.0.1:443", Protocol: relay.EndpointProtocolTLS}}, RelayCredential: credential,
	}
	m.MapSignature, err = api.SignNetworkMap(key, m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, m.MapSignature))}
	event := testMapStreamSnapshotEvent(t, m)
	for i := range 2 {
		got, action, err := cacheNetworkMapFromEvent(&cfg, event)
		if err != nil {
			t.Fatal("valid repeated snapshot rejected")
		}
		if i == 1 && action != "unchanged" {
			t.Fatal("replay was not recognized")
		}
		if got.Node.ID != m.Node.ID || len(got.Peers) != 1 || len(got.Relays) != 1 || got.RelayCredential == nil || got.RelayCredential.Signature != credential.Signature {
			t.Fatalf("snapshot application %d lost runtime network or Relay configuration", i+1)
		}
		if cfg.CachedMap == nil || cfg.CachedMap.RelayCredential != nil {
			t.Fatal("cache persisted ephemeral Relay credential")
		}
	}
	// Matching revision/hash must not bypass validation of a replayed payload.
	event.Snapshot.Peers[0].AllowedIPs = []string{"198.18.94.99/32"}
	if _, _, err := cacheNetworkMapFromEvent(&cfg, event); err == nil {
		t.Fatal("tampered repeated snapshot accepted")
	}
}
