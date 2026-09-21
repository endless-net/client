package client

import (
	"errors"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	relay "github.com/endless-net/relay/protocol/v1"
)

func relayObservationFixture(t *testing.T) (*wireGuardRelayBridge, api.RegisterNodeResponse) {
	t.Helper()
	_, networkMap, _ := signedApplicationFixture(t, false)
	networkMap.Node.PublicKey = testWireGuardEnginePublicKey(1)
	networkMap.Relays = []relay.Endpoint{{ID: "relay", Addr: "relay.example:443", Protocol: "https"}}
	generation := newWireGuardRelayGeneration(networkMap)
	generation.readyAt = time.Now().UTC().Add(-time.Minute)
	return &wireGuardRelayBridge{generation: generation, cancel: func() {}, done: make(chan error, 1), statusOK: true,
		status: RelayDataplaneBridgeStatus{Relay: RelayDialResult{Selected: &networkMap.Relays[0]}, PeerEndpoints: map[string]string{networkMap.Peers[0].ID: "127.0.0.1:12345"}}}, networkMap
}

func TestRelayPeerObservationBindsMapAndIdentity(t *testing.T) {
	b, networkMap := relayObservationFixture(t)
	peer := networkMap.Peers[0]
	snapshot, ok := b.ObservePeer(networkMap, peer.ID)
	if !ok || snapshot.PublicKey != peer.PublicKey || snapshot.MapHash != networkMap.MapSignature.PayloadHash || snapshot.Endpoint != "127.0.0.1:12345" || snapshot.RelayAddress != networkMap.Relays[0].Addr || snapshot.ReadyAt.IsZero() || !b.ObservationCurrent(snapshot) {
		t.Fatal("current generation did not produce exact private evidence")
	}
	for name, change := range map[string]func(*api.RegisterNodeResponse){
		"hash":             func(m *api.RegisterNodeResponse) { m.MapSignature.PayloadHash = "changed" },
		"global":           func(m *api.RegisterNodeResponse) { m.Revision.Global++ },
		"network_revision": func(m *api.RegisterNodeResponse) { m.Network.Revision++ },
		"recipient":        func(m *api.RegisterNodeResponse) { m.Node.ID = "other" },
		"recipient_key":    func(m *api.RegisterNodeResponse) { m.Node.PublicKey = testWireGuardEnginePublicKey(3) },
		"network":          func(m *api.RegisterNodeResponse) { m.Network.ID = "other" },
		"peer_key":         func(m *api.RegisterNodeResponse) { m.Peers[0].PublicKey = testWireGuardEnginePublicKey(3) },
		"peer_network":     func(m *api.RegisterNodeResponse) { m.Peers[0].NetworkID = "other" },
		"duplicate_peer":   func(m *api.RegisterNodeResponse) { m.Peers = append(m.Peers, m.Peers[0]) },
		"relay":            func(m *api.RegisterNodeResponse) { m.Relays[0].Addr = "replacement.example:443" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *clonePersistentConfig(Config{CachedMap: &networkMap}).CachedMap
			change(&changed)
			if _, ok := b.ObservePeer(changed, peer.ID); ok {
				t.Fatal("different map/recipient/peer reused bridge evidence")
			}
		})
	}
	status, ok, err := b.Status()
	if !ok || err != nil {
		t.Fatal(err)
	}
	status.PeerEndpoints[peer.ID] = "127.0.0.1:54321"
	status.Relay.Selected.Addr = "foreign.example:443"
	if !b.ObservationCurrent(snapshot) {
		t.Fatal("public status alias mutated private bridge evidence")
	}
}

func TestRelayPeerObservationRejectsEndedReplacedAndTamperedGeneration(t *testing.T) {
	for _, scenario := range []string{"ended", "result_pending", "replacement", "endpoint", "external_relay", "unready", "lease"} {
		t.Run(scenario, func(t *testing.T) {
			b, networkMap := relayObservationFixture(t)
			snapshot, ok := b.ObservePeer(networkMap, networkMap.Peers[0].ID)
			if !ok {
				t.Fatal("missing initial evidence")
			}
			switch scenario {
			case "ended":
				close(b.generation.ended)
			case "result_pending":
				b.done <- errors.New("bridge ended before goroutine cleanup")
			case "replacement":
				b.generation = newWireGuardRelayGeneration(networkMap)
				b.generation.readyAt = time.Now().UTC()
			case "endpoint":
				b.status.PeerEndpoints[snapshot.PeerID] = "127.0.0.1:54321"
			case "external_relay":
				selected := *b.status.Relay.Selected
				selected.Addr = "foreign.example:443"
				b.status.Relay.Selected = &selected
			case "unready":
				b.statusOK = false
			case "lease":
				b.lease = newUnderlayDNSLease(nil)
				b.lease.Close()
			}
			if b.ObservationCurrent(snapshot) {
				t.Fatal("stale bridge receipt remained current")
			}
			if scenario == "replacement" {
				fresh, ok := b.ObservePeer(networkMap, snapshot.PeerID)
				if !ok || fresh.generation == snapshot.generation || !fresh.ReadyAt.After(snapshot.ReadyAt) {
					t.Fatal("replacement did not require its own generation and Ready time")
				}
			}
		})
	}
}

func TestRelayBridgeReuseKeyIncludesAuthorityAndPeerKeys(t *testing.T) {
	_, networkMap := relayObservationFixture(t)
	before := wireGuardRelayBridgeKey(networkMap, "127.0.0.1:51820", 0)
	for _, change := range []func(*api.RegisterNodeResponse){
		func(m *api.RegisterNodeResponse) { m.MapSignature.PayloadHash = "changed" },
		func(m *api.RegisterNodeResponse) { m.Revision.Global++ },
		func(m *api.RegisterNodeResponse) { m.Node.PublicKey = testWireGuardEnginePublicKey(3) },
		func(m *api.RegisterNodeResponse) { m.Peers[0].PublicKey = testWireGuardEnginePublicKey(3) },
	} {
		changed := *clonePersistentConfig(Config{CachedMap: &networkMap}).CachedMap
		change(&changed)
		if before == wireGuardRelayBridgeKey(changed, "127.0.0.1:51820", 0) {
			t.Fatal("changed authority would reuse previous Ready generation")
		}
	}
}
