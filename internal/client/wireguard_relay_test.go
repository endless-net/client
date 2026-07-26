package client

import (
	"testing"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

	relayauth "github.com/unng-lab/endlessnet-relay/protocol/v1"
)

func TestWireGuardRelayPathManagerStartsOnRelayAndFallsBackAfterDirectFailure(t *testing.T) {
	manager := newWireGuardRelayPathManager(3 * time.Second)
	peer := clientapi.Peer{
		ID:                 "peer-1",
		Hostname:           "peer-a",
		PublicKey:          "peer-public-key",
		Endpoint:           "198.51.100.10:51820",
		EndpointCandidates: []string{"198.51.100.10:51820", "192.168.55.10:51820"},
		AllowedIPs:         []string{"100.64.0.3/32"},
	}
	networkMap := clientapi.RegisterNodeResponse{
		Peers:  []clientapi.Peer{peer},
		Relays: []relayauth.Endpoint{{ID: "relay-1", Addr: "relay.example.test:443", Protocol: relayauth.EndpointProtocolTLS}},
	}
	selectedRelay := networkMap.Relays[0]
	relayResult := RelayDialResult{Selected: &selectedRelay}
	relayOverrides := map[string]string{"peer-1": "127.0.0.1:62000"}
	now := time.Unix(100, 0).UTC()

	overrides := manager.Bootstrap(networkMap, relayOverrides, relayResult, nil, now)
	if overrides["peer-1"] != relayOverrides["peer-1"] {
		t.Fatalf("bootstrap overrides = %#v, want relay", overrides)
	}
	if status := manager.Statuses()[0]; status.SelectedPath != "relay" {
		t.Fatalf("bootstrap status = %#v", status)
	}

	reachable := DirectEndpointProbe{
		PeerID:    peer.ID,
		Endpoint:  peer.Endpoint,
		Reachable: true,
		RTT:       20 * time.Millisecond,
		CheckedAt: now.Add(time.Second),
	}
	overrides, triggers := manager.Reconcile(networkMap, relayOverrides, relayResult, nil, map[string][]DirectEndpointProbe{peer.ID: {reachable}}, now.Add(time.Second))
	if overrides[peer.ID] != peer.Endpoint || len(triggers) != 1 || triggers[0] != "100.64.0.3" {
		t.Fatalf("direct upgrade overrides/triggers = %#v / %#v", overrides, triggers)
	}
	if status := manager.Statuses()[0]; status.SelectedPath != "direct" || status.SelectedEndpoint != peer.Endpoint || status.Direct.State != "reachable" {
		t.Fatalf("direct status = %#v", status)
	}

	failed := DirectEndpointProbe{PeerID: peer.ID, Endpoint: peer.Endpoint, CheckedAt: now.Add(2 * time.Second), Error: "timeout"}
	overrides, _ = manager.Reconcile(networkMap, relayOverrides, relayResult, nil, map[string][]DirectEndpointProbe{peer.ID: {failed}}, now.Add(2*time.Second))
	if overrides[peer.ID] != peer.Endpoint {
		t.Fatalf("single failed probe should retain direct path: %#v", overrides)
	}
	failed.CheckedAt = now.Add(3 * time.Second)
	overrides, _ = manager.Reconcile(networkMap, relayOverrides, relayResult, nil, map[string][]DirectEndpointProbe{peer.ID: {failed}}, now.Add(3*time.Second))
	if overrides[peer.ID] != relayOverrides[peer.ID] {
		t.Fatalf("consecutive failed probes should fall back to relay: %#v", overrides)
	}
	status := manager.Statuses()[0]
	if status.SelectedPath != "relay" || status.Direct.State != "failed" || status.Direct.ConsecutiveFailures != wireGuardDirectFailureThreshold {
		t.Fatalf("relay fallback status = %#v", status)
	}
}

func TestWireGuardRelayPathManagerUsesRTTHysteresis(t *testing.T) {
	manager := newWireGuardRelayPathManager(3 * time.Second)
	peer := clientapi.Peer{
		ID:                 "peer-1",
		Hostname:           "peer-a",
		EndpointCandidates: []string{"198.51.100.10:51820", "198.51.100.11:51820"},
	}
	networkMap := clientapi.RegisterNodeResponse{Peers: []clientapi.Peer{peer}}
	now := time.Unix(200, 0).UTC()
	manager.Bootstrap(networkMap, nil, RelayDialResult{}, nil, now)
	probes := map[string][]DirectEndpointProbe{peer.ID: {
		{PeerID: peer.ID, Endpoint: peer.EndpointCandidates[0], Reachable: true, RTT: 100 * time.Millisecond, CheckedAt: now},
		{PeerID: peer.ID, Endpoint: peer.EndpointCandidates[1], Reachable: true, RTT: 120 * time.Millisecond, CheckedAt: now},
	}}
	overrides, _ := manager.Reconcile(networkMap, nil, RelayDialResult{}, nil, probes, now)
	if overrides[peer.ID] != peer.EndpointCandidates[0] {
		t.Fatalf("initial selected endpoint = %#v", overrides)
	}

	for index := 1; index <= 3; index++ {
		checkedAt := now.Add(time.Duration(index) * time.Second)
		probes[peer.ID] = []DirectEndpointProbe{
			{PeerID: peer.ID, Endpoint: peer.EndpointCandidates[0], Reachable: true, RTT: 100 * time.Millisecond, CheckedAt: checkedAt},
			{PeerID: peer.ID, Endpoint: peer.EndpointCandidates[1], Reachable: true, RTT: 40 * time.Millisecond, CheckedAt: checkedAt},
		}
		overrides, _ = manager.Reconcile(networkMap, nil, RelayDialResult{}, nil, probes, checkedAt)
	}
	if overrides[peer.ID] != peer.EndpointCandidates[1] {
		t.Fatalf("materially lower-latency endpoint was not selected: %#v", overrides)
	}
	if status := manager.Statuses()[0]; status.SelectionReason != "switched to a materially lower-latency direct candidate" {
		t.Fatalf("hysteresis selection status = %#v", status)
	}
}
