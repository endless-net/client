package client

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/tailscale/wireguard-go/device"
)

func TestResourceHostRelayCollectorKeepsNativeRouteAndFilterGates(t *testing.T) {
	for _, scenario := range []string{"confirmed", "missing_route", "withdrawn_acl", "ended_during_routes", "old_path_handshake"} {
		t.Run(scenario, func(t *testing.T) {
			n, cfg, _ := nativeExitResumeFixture(t)
			signing, _, key := signedApplicationFixture(t, false)
			cfg.MapSigningTrust = signing.MapSigningTrust
			selected := relay.Endpoint{ID: "relay", Addr: "192.0.2.10:443", Protocol: relay.EndpointProtocolTLS}
			cfg.CachedMap.Relays = []relay.Endpoint{selected}
			// No credential means Configure cannot initiate any relay network activity.
			// The local bridge below is injected only after actual device/filter apply.
			cfg.CachedMap.RelayCredential = nil
			resignApplicationMap(t, cfg.CachedMap, key)
			cfg = clonePersistentConfig(cfg)
			n.engine.pathCancel = func() {}
			if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
				t.Fatal(err)
			}
			e := n.engine
			now := time.Now().UTC()
			e.mu.Lock()
			inspection, err := resourceObservedUAPI(e)
			if err != nil {
				e.mu.Unlock()
				t.Fatal(err)
			}
			peer := cfg.CachedMap.Peers[0]
			live, ok := wireGuardPeerForMapPeer(inspection, peer)
			if !ok {
				e.mu.Unlock()
				t.Fatal("missing applied peer")
			}
			generation := newWireGuardRelayGeneration(*cfg.CachedMap)
			generation.readyAt = now.Add(-2 * time.Second)
			e.relayBridge = &wireGuardRelayBridge{generation: generation, cancel: func() {}, statusOK: true, status: RelayDataplaneBridgeStatus{Relay: RelayDialResult{Selected: &selected}, PeerEndpoints: map[string]string{peer.ID: live.Endpoint}}}
			transition := now.Add(-time.Second)
			if scenario == "old_path_handshake" {
				transition = now.Add(time.Nanosecond)
			}
			e.relayPaths.statuses = []PeerPathStatus{{PeerID: peer.ID, SelectedPath: "relay", SelectedEndpoint: selected.Addr, LastTransitionAt: transition.Format(time.RFC3339Nano), Relay: PathCandidateStatus{State: "reachable", Endpoint: selected.Addr, RelayID: selected.ID, Protocol: selected.Protocol}}}
			if scenario == "withdrawn_acl" {
				e.peerACLFilter.mu.Lock()
				e.peerACLFilter.closed = true
				e.peerACLFilter.mu.Unlock()
			}
			iface, local := e.interface_, e.routerCfg.Addresses[0].Addr().String()
			e.mu.Unlock()
			ended := false
			runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "rule show") {
					return []byte(`[{"priority":32766,"src":"all","table":"254"}]`), nil
				}
				if strings.Contains(joined, "address show") {
					return []byte(fmt.Sprintf(`[{"ifindex":7,"ifname":%q,"flags":["UP"],"addr_info":[{"local":%q}]}]`, iface, local)), nil
				}
				if scenario == "missing_route" {
					return []byte(`[]`), nil
				}
				if scenario == "ended_during_routes" && !ended {
					close(generation.ended)
					ended = true
				}
				return []byte(fmt.Sprintf(`[{"dst":%q,"dev":%q,"prefsrc":%q,"flags":[]}]`, args[5], iface, local)), nil
			}
			inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
				result, err := resourceObservedUAPI(e)
				for i := range result.Peers {
					result.Peers[i].LatestHandshakeUnix = now.Unix()
					result.Peers[i].latestHandshakeNanos = int64(now.Nanosecond())
					result.Peers[i].handshakeTimeComplete = true
				}
				return result, err
			}
			proof, err := e.observeResourceHostsWithInspection(t.Context(), cfg, runner, now, inspect)
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)
			confirmed := err == nil && proof.HostConfirmed(id) && proof.Current(cfg, time.Now())
			if confirmed != (scenario == "confirmed") {
				t.Fatal("incorrect relay collector evidence", scenario, err)
			}
		})
	}
}

func TestResourceHostRelayRequiresPeerHandshakeOnSelectedBridge(t *testing.T) {
	now := time.Now().UTC()
	peer := api.Peer{ID: "peer", PublicKey: testWireGuardEnginePublicKey(2)}
	live := WireGuardPeerInspection{Endpoint: "127.0.0.1:1234", LatestHandshakeUnix: now.Unix(), latestHandshakeNanos: int64(now.Nanosecond()), handshakeTimeComplete: true}
	bridge := wireGuardRelayPeerObservation{ReadyAt: now.Add(-2 * time.Second), Endpoint: live.Endpoint, PeerID: peer.ID, PublicKey: peer.PublicKey, RelayID: "relay", RelayAddress: "relay.example:443", RelayProtocol: "relay-v1-tls"}
	path := PeerPathStatus{PeerID: peer.ID, SelectedPath: "relay", SelectedEndpoint: bridge.RelayAddress, LastTransitionAt: now.Add(-time.Second).Format(time.RFC3339Nano), Relay: PathCandidateStatus{State: "reachable", Endpoint: bridge.RelayAddress, RelayID: bridge.RelayID, Protocol: bridge.RelayProtocol}}
	for _, scenario := range []string{"confirmed", "ready_after_handshake", "ready_equal_handshake", "transition_after_handshake", "transition_equal_handshake", "missing_transition", "missing_ready", "old_handshake", "future_handshake", "incomplete_handshake", "wrong_local_port", "not_loopback", "wrong_peer_key", "wrong_peer_id", "external_endpoint", "relay_id", "relay_protocol", "relay_failed", "direct", "duplicate_path"} {
		t.Run(scenario, func(t *testing.T) {
			p, b, s := live, bridge, path
			switch scenario {
			case "ready_after_handshake":
				b.ReadyAt = now.Add(time.Nanosecond)
			case "ready_equal_handshake":
				b.ReadyAt = now
			case "transition_after_handshake":
				s.LastTransitionAt = now.Add(time.Nanosecond).Format(time.RFC3339Nano)
			case "transition_equal_handshake":
				s.LastTransitionAt = now.Format(time.RFC3339Nano)
			case "missing_transition":
				s.LastTransitionAt = ""
			case "missing_ready":
				b.ReadyAt = time.Time{}
			case "old_handshake":
				p.LatestHandshakeUnix = now.Add(-device.RejectAfterTime).Unix()
			case "future_handshake":
				p.latestHandshakeNanos++
				if p.latestHandshakeNanos == int64(time.Second) {
					p.LatestHandshakeUnix++
					p.latestHandshakeNanos = 0
				}
			case "incomplete_handshake":
				p.handshakeTimeComplete = false
			case "wrong_local_port":
				p.Endpoint = "127.0.0.1:1235"
			case "not_loopback":
				b.Endpoint = "192.0.2.1:1234"
				p.Endpoint = b.Endpoint
			case "wrong_peer_key":
				b.PublicKey = testWireGuardEnginePublicKey(3)
			case "wrong_peer_id":
				b.PeerID = "other"
			case "external_endpoint":
				s.SelectedEndpoint = "other.example:443"
			case "relay_id":
				s.Relay.RelayID = "other"
			case "relay_protocol":
				s.Relay.Protocol = "other"
			case "relay_failed":
				s.Relay.State = "failed"
			case "direct":
				s.SelectedPath = "direct"
			}
			paths := []PeerPathStatus{s}
			if scenario == "duplicate_path" {
				paths = append(paths, s)
			}
			if resourceHostRelayPathObserved(paths, peer, p, b, now) != (scenario == "confirmed") {
				t.Fatal("incorrect relay transport evidence", scenario)
			}
		})
	}
}

// Like resourceHostProjectionFixture, this is an injected private proof used to
// test lifetime invalidation. It does not pretend to establish a relay session.
func TestResourceHostCurrentWithdrawsRelayGeneration(t *testing.T) {
	for _, scenario := range []string{"current", "ended", "replacement", "bridge_replacement", "endpoint_replaced", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, proof := resourceHostProjectionFixture(t)
			network := *cfg.CachedMap
			selected := relay.Endpoint{ID: "relay", Addr: "relay.example:443", Protocol: relay.EndpointProtocolTLS}
			network.Relays = []relay.Endpoint{selected}
			generation := newWireGuardRelayGeneration(network)
			generation.readyAt = time.Now().Add(-time.Second)
			bridge := &wireGuardRelayBridge{generation: generation, cancel: func() {}, statusOK: true, status: RelayDataplaneBridgeStatus{Relay: RelayDialResult{Selected: &selected}, PeerEndpoints: map[string]string{network.Peers[0].ID: "127.0.0.1:1234"}}}
			snapshot, ok := bridge.ObservePeer(network, network.Peers[0].ID)
			if !ok {
				t.Fatal("missing injected bridge snapshot")
			}
			e.mu.Lock()
			e.relayBridge = bridge
			proof.relayBridge = bridge
			proof.relays = []wireGuardRelayPeerObservation{snapshot}
			switch scenario {
			case "ended":
				close(generation.ended)
			case "replacement":
				next := newWireGuardRelayGeneration(network)
				next.readyAt = generation.readyAt
				bridge.generation = next
			case "bridge_replacement":
				e.relayBridge = newWireGuardRelayBridge(time.Second, nil)
			case "endpoint_replaced":
				bridge.status.PeerEndpoints[network.Peers[0].ID] = "127.0.0.1:1235"
			case "stopped":
				bridge.cancel = nil
			}
			e.mu.Unlock()
			if proof.Current(cfg, time.Now()) != (scenario == "current") {
				t.Fatal("incorrect bridge generation lifetime", scenario)
			}
		})
	}
}
