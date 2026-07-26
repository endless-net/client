package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

	relayauth "github.com/unng-lab/endlessnet-relay/protocol/v1"
)

func TestBuildPathStatusesSelectsReachableRelayAndMarksDirectUntested(t *testing.T) {
	selected := relayauth.Endpoint{ID: "relay-2", Addr: "127.0.0.1:9443", Protocol: relayauth.EndpointProtocolTLS}
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:       "peer-1",
			Hostname: "peer-a",
			Endpoint: "peer-a.example.test:51820",
		}},
		Relays: []relayauth.Endpoint{{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}, selected},
	}, RelayDialResult{Selected: &selected}, nil, nil, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "untested" || status.Direct.Endpoint != "peer-a.example.test:51820" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Direct.Tier != "public_direct" || status.Direct.Priority != 20 {
		t.Fatalf("direct path priority = %#v", status.Direct)
	}
	if status.Relay.State != "reachable" || status.Relay.RelayID != "relay-2" || status.Relay.Protocol != "relay-v1-tls" {
		t.Fatalf("relay status = %#v", status.Relay)
	}
	if status.Relay.Tier != "" || status.Relay.Priority != 0 {
		t.Fatalf("relay path priority = %#v", status.Relay)
	}
	if status.SelectedPath != "relay" {
		t.Fatalf("selected_path = %q, want relay", status.SelectedPath)
	}
}

func TestBuildPathStatusesSelectsReachableDirect(t *testing.T) {
	selectedRelay := relayauth.Endpoint{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "peer-a.example.test:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
		Relays: []relayauth.Endpoint{selectedRelay},
	}, RelayDialResult{Selected: &selectedRelay}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "peer-a.example.test:51820",
			LatestHandshakeUnix: 1782260000,
			AllowedIPs:          []string{"100.64.0.3/32"},
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "reachable" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Direct.Endpoint != "peer-a.example.test:51820" || status.Direct.Tier != "public_direct" || status.Direct.Priority != 20 {
		t.Fatalf("direct path priority = %#v", status.Direct)
	}
	if status.SelectedPath != "direct" {
		t.Fatalf("selected_path = %q, want direct", status.SelectedPath)
	}
}

func TestBuildPathStatusesSelectsReachablePublicIPv6Direct(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "[2001:db8:100::20]:51820",
			AllowedIPs: []string{"fd7a:115c:a1e0::3/128"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "[2001:db8:100::20]:51820",
			LatestHandshakeUnix: 1782260000,
			AllowedIPs:          []string{"fd7a:115c:a1e0::3/128"},
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "fd7a:115c:a1e0::3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "reachable" || status.Direct.Endpoint != "[2001:db8:100::20]:51820" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Direct.Tier != "public_direct" || status.Direct.Priority != 20 {
		t.Fatalf("direct path priority = %#v", status.Direct)
	}
	if status.SelectedPath != "direct" {
		t.Fatalf("selected_path = %q, want direct", status.SelectedPath)
	}
}

func TestBuildPathStatusesClassifiesLiveLANCandidate(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:                 "peer-1",
			Hostname:           "peer-a",
			PublicKey:          "peer-public-key",
			Endpoint:           "peer-a.example.test:51820",
			EndpointCandidates: []string{"peer-a.example.test:51820", "192.168.55.7:51820"},
			AllowedIPs:         []string{"100.64.0.3/32"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "192.168.55.7:51820",
			LatestHandshakeUnix: 1782260000,
			AllowedIPs:          []string{"100.64.0.3/32"},
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "reachable" || status.Direct.Endpoint != "192.168.55.7:51820" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Direct.Tier != "lan_direct" || status.Direct.Priority != 10 {
		t.Fatalf("direct path priority = %#v", status.Direct)
	}
	if status.SelectedPath != "direct" {
		t.Fatalf("selected_path = %q, want direct", status.SelectedPath)
	}
}

func TestBuildPathStatusesClassifiesLivePunchedUDPCandidate(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:                 "peer-1",
			Hostname:           "peer-a",
			PublicKey:          "peer-public-key",
			Endpoint:           "203.0.113.20:51820",
			EndpointCandidates: []string{"203.0.113.20:51820", "198.51.100.7:51820"},
			AllowedIPs:         []string{"100.64.0.3/32"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "198.51.100.7:51820",
			LatestHandshakeUnix: 1782260000,
			AllowedIPs:          []string{"100.64.0.3/32"},
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "reachable" || status.Direct.Endpoint != "198.51.100.7:51820" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Direct.Tier != "mapped_direct" || status.Direct.Priority != 30 {
		t.Fatalf("direct path priority = %#v", status.Direct)
	}
	if status.SelectedPath != "direct" {
		t.Fatalf("selected_path = %q, want direct", status.SelectedPath)
	}
}

func TestBuildPathStatusesRejectsUnsignedLiveEndpointAsDirect(t *testing.T) {
	selectedRelay := relayauth.Endpoint{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:                 "peer-1",
			Hostname:           "peer-a",
			PublicKey:          "peer-public-key",
			Endpoint:           "peer-a.example.test:51820",
			EndpointCandidates: []string{"192.168.55.7:51820"},
			AllowedIPs:         []string{"100.64.0.3/32"},
		}},
		Relays: []relayauth.Endpoint{selectedRelay},
	}, RelayDialResult{Selected: &selectedRelay}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "127.0.0.1:62000",
			LatestHandshakeUnix: 1782260000,
			AllowedIPs:          []string{"100.64.0.3/32"},
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "failed" || !strings.Contains(status.Direct.Reason, "not a signed direct candidate") || status.SelectedPath != "relay" {
		t.Fatalf("path status = %#v", status)
	}
}

func TestBuildPathStatusesIncludesDirectRTT(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "peer-a.example.test:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			LatestHandshakeUnix: 1782260000,
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, map[string]DirectRTTProbe{
		"100.64.0.3": {Target: "100.64.0.3", RTTMS: 1.25},
	})
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "reachable" || status.Direct.RTTMS != 1.25 || status.SelectedPath != "direct" {
		t.Fatalf("path status = %#v", status)
	}
}

func TestBuildPathStatusesRequiresRTTWhenProbed(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "peer-a.example.test:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			LatestHandshakeUnix: 1782260000,
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, map[string]DirectRTTProbe{
		"100.64.0.3": {Target: "100.64.0.3", Error: "ping failed"},
	})
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "failed" || status.Direct.Reason != "ping failed" || status.SelectedPath != "none" {
		t.Fatalf("path status = %#v", status)
	}
}

func TestBuildPathStatusesFallsBackToRelayWhenDirectHasNoHandshake(t *testing.T) {
	selectedRelay := relayauth.Endpoint{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "peer-a.example.test:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
		Relays: []relayauth.Endpoint{selectedRelay},
	}, RelayDialResult{Selected: &selectedRelay}, nil, &WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		Peers: []WireGuardPeerInspection{{
			PublicKey: "peer-public-key",
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "failed" || status.SelectedPath != "relay" {
		t.Fatalf("path status = %#v", status)
	}
}

func TestBuildPathStatusesReportsMissingAndFailedPaths(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers:  []clientapi.Peer{{ID: "peer-1", Hostname: "peer-a"}},
		Relays: []relayauth.Endpoint{{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}},
	}, RelayDialResult{}, errors.New("relay unavailable"), nil, nil)
	if len(statuses) != 1 {
		t.Fatalf("statuses = %#v", statuses)
	}
	status := statuses[0]
	if status.Direct.State != "missing" {
		t.Fatalf("direct status = %#v", status.Direct)
	}
	if status.Relay.State != "failed" || status.Relay.Reason != "relay unavailable" {
		t.Fatalf("relay status = %#v", status.Relay)
	}
	if status.SelectedPath != "none" {
		t.Fatalf("selected_path = %q, want none", status.SelectedPath)
	}
}

func TestBuildPathStatusesUsesSignedEndpointCandidateWithoutPrimaryEndpoint(t *testing.T) {
	statuses := BuildPathStatusesWithDirectProbes(clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:                 "peer-1",
			Hostname:           "peer-a",
			PublicKey:          "peer-public-key",
			EndpointCandidates: []string{"198.51.100.7:51820"},
			AllowedIPs:         []string{"100.64.0.3/32"},
		}},
	}, RelayDialResult{}, nil, &WireGuardInspection{
		OK: true,
		Peers: []WireGuardPeerInspection{{
			PublicKey:           "peer-public-key",
			Endpoint:            "198.51.100.7:51820",
			LatestHandshakeUnix: time.Now().Unix(),
		}},
		Routes: []WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "endlessnet",
			UsesInterface: true,
		}},
	}, nil)
	if len(statuses) != 1 || statuses[0].Direct.State != "reachable" || statuses[0].SelectedPath != "direct" {
		t.Fatalf("candidate-only path status = %#v", statuses)
	}
}

func TestProbeDirectRTTParsesPingOutput(t *testing.T) {
	probe := ProbeDirectRTT(context.Background(), DirectRTTProbeOptions{
		Target:      "100.64.0.3",
		PingCommand: "ping",
		Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			got := name + " " + strings.Join(args, " ")
			if got != "ping -c 1 -W 1 100.64.0.3" {
				return nil, fmt.Errorf("unexpected command %s", got)
			}
			return []byte("64 bytes from 100.64.0.3: seq=0 ttl=64 time=0.421 ms\n"), nil
		},
	})
	if probe.Error != "" || probe.RTTMS != 0.421 {
		t.Fatalf("probe = %#v", probe)
	}
}

func TestWireGuardRouteTargetsForPeers(t *testing.T) {
	targets := WireGuardRouteTargetsForPeers([]clientapi.Peer{
		{AllowedIPs: []string{"100.64.0.3/32", "10.0.0.0/24"}},
		{AllowedIPs: []string{"100.64.0.3/32"}},
		{AllowedIPs: []string{"2001:db8::3/128"}},
	})
	if len(targets) != 2 || targets[0] != "100.64.0.3" || targets[1] != "2001:db8::3" {
		t.Fatalf("targets = %#v", targets)
	}
}
