package main

import (
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
	relay "github.com/endless-net/relay/protocol/v1"
)

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
