package main

import (
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeDiagnosticPeersJoinVerifiedIdentityWithoutInferringHealth(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{Network: clientapi.Network{ID: "network", CIDR: "100.64.0.0/24", IPv6CIDR: "fd00::/64"},
		Peers: []clientapi.Peer{{ID: "peer-a", NetworkID: "network", Hostname: "host-a", PublicKey: "key-a", AllowedIPs: []string{"100.64.0.2/32", "fd00::2/128", "100.64.0.2/32", "192.0.2.1/32", "10.0.0.0/8"}}}}
	inspection := client.WireGuardInspection{OK: true, Peers: []client.WireGuardPeerInspection{{PublicKey: "key-a", Endpoint: "192.0.2.1:1234", AllowedIPs: []string{"100.64.0.2/32"}, TransferRXBytes: 12, TransferTXBytes: 34, LatestHandshakeUnix: 100, PersistentKeepaliveSeconds: 25}}}
	peers, live, failures := nativeDiagnosticPeers(networkMap, inspection)
	if len(failures) != 0 || len(peers) != 1 || len(live) != 1 || live[0].PeerId != "peer-a" {
		t.Fatal("verified identity join failed")
	}
	if len(peers[0].OverlayAddresses) != 2 || peers[0].OverlayAddresses[0] != "100.64.0.2" || peers[0].OverlayAddresses[1] != "fd00::2" {
		t.Fatal("subnet/foreign host routes became overlay addresses")
	}
	if peers[0].SelectedPath != ipc.PathKind_PATH_KIND_UNSPECIFIED || peers[0].SelectedEndpoint != "" || len(peers[0].Candidates) != 0 {
		t.Fatal("handshake inferred a selected/reachable path")
	}
	if live[0].ReceivedBytes != 12 || live[0].TransmittedBytes != 34 || live[0].LatestHandshake.Seconds != 100 || live[0].PersistentKeepalive.Seconds != 25 {
		t.Fatal("live observation fields lost")
	}
	inspection.Peers[0].AllowedIPs[0] = "changed"
	if live[0].AllowedIps[0] != "100.64.0.2/32" {
		t.Fatal("live output aliases input")
	}
	inspection.Peers[0].LatestHandshakeUnix = 0
	_, live, _ = nativeDiagnosticPeers(networkMap, inspection)
	if live[0].LatestHandshake != nil {
		t.Fatal("absent handshake became Unix epoch observation")
	}
	inspection.OK = false
	peers, live, _ = nativeDiagnosticPeers(networkMap, inspection)
	if len(peers) != 1 || len(live) != 0 {
		t.Fatal("failed engine inspection exposed live values")
	}
}

func TestNativeDiagnosticPeersRejectAmbiguousOrUnknownIdentity(t *testing.T) {
	for _, mode := range []string{"duplicate-id", "duplicate-key", "foreign-network", "unknown-key", "invalid-handshake", "invalid-keepalive"} {
		t.Run(mode, func(t *testing.T) {
			networkMap := clientapi.RegisterNodeResponse{Network: clientapi.Network{ID: "network"}, Peers: []clientapi.Peer{{ID: "a", PublicKey: "key-a"}}}
			inspection := client.WireGuardInspection{OK: true, Peers: []client.WireGuardPeerInspection{{PublicKey: "key-a"}}}
			switch mode {
			case "duplicate-id":
				networkMap.Peers = append(networkMap.Peers, clientapi.Peer{ID: "a", PublicKey: "key-b"})
			case "duplicate-key":
				networkMap.Peers = append(networkMap.Peers, clientapi.Peer{ID: "b", PublicKey: "key-a"})
			case "foreign-network":
				networkMap.Peers[0].NetworkID = "other"
			case "unknown-key":
				inspection.Peers[0].PublicKey = "unmapped-key"
			case "invalid-handshake":
				inspection.Peers[0].LatestHandshakeUnix = -1
			case "invalid-keepalive":
				inspection.Peers[0].PersistentKeepaliveSeconds = 65536
			}
			_, live, failures := nativeDiagnosticPeers(networkMap, inspection)
			if len(live) != 0 || len(failures) != 1 {
				t.Fatal("invalid identity/observation was projected")
			}
		})
	}
}
