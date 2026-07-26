package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

	relayauth "github.com/unng-lab/endlessnet-relay/protocol/v1"
)

func TestBuildAgentSnapshotReportsStateWithoutSecrets(t *testing.T) {
	secretCredential := "secret-node-credential"
	secretRelaySignature := "secret-relay-signature"
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", Revision: 9},
		Node: clientapi.Node{
			ID:           "node-1",
			NetworkID:    "net-1",
			Hostname:     "node-a",
			AssignedIP:   "100.64.0.2",
			AssignedIPv6: "fd7a:115c:a1e0::2",
		},
		NodeCredential: secretCredential,
		RelayCredential: &relayauth.Credential{
			NetworkID: "net-1",
			NodeID:    "node-1",
			Signature: secretRelaySignature,
		},
		Peers: []clientapi.Peer{{
			ID:       "peer-1",
			Hostname: "peer-a",
			Endpoint: "peer-a.example.test:51820",
		}},
		Relays: []relayauth.Endpoint{{
			ID:       "relay-1",
			Addr:     "127.0.0.1:1",
			Protocol: "udp",
		}},
	}

	snapshot := BuildAgentSnapshot(context.Background(), networkMap, AgentProbeOptions{
		GeneratedAt:  time.Unix(100, 0).UTC(),
		STUNTimeout:  10 * time.Millisecond,
		RelayTimeout: 10 * time.Millisecond,
	})
	if snapshot.NodeID != "node-1" || snapshot.NetworkID != "net-1" || snapshot.MapRevision != 9 {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if snapshot.OverlayIPv6 != "fd7a:115c:a1e0::2" {
		t.Fatalf("snapshot overlay_ipv6 = %q", snapshot.OverlayIPv6)
	}
	if snapshot.STUN.OK || snapshot.STUN.Error == "" {
		t.Fatalf("STUN snapshot = %#v", snapshot.STUN)
	}
	if snapshot.STUN.NAT.Classification != "unreachable" && snapshot.STUN.NAT.Classification != "no_endpoints" {
		t.Fatalf("STUN NAT snapshot = %#v", snapshot.STUN.NAT)
	}
	if snapshot.Relay.OK || snapshot.Relay.Error == "" {
		t.Fatalf("relay snapshot = %#v", snapshot.Relay)
	}
	if len(snapshot.Paths) != 1 || snapshot.Paths[0].SelectedPath != "none" {
		t.Fatalf("paths = %#v", snapshot.Paths)
	}

	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, secret := range []string{secretCredential, secretRelaySignature} {
		if strings.Contains(out, secret) {
			t.Fatalf("agent snapshot leaked %q: %s", secret, out)
		}
	}
}

func TestBuildAgentSnapshotIncludesLiveDirectPath(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", Revision: 10},
		Node: clientapi.Node{
			ID:         "node-1",
			NetworkID:  "net-1",
			Hostname:   "node-a",
			AssignedIP: "100.64.0.2",
		},
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  "peer-public-key",
			Endpoint:   "198.51.100.10:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got := name + " " + strings.Join(args, " ")
		switch got {
		case "wg show wg0 dump":
			return []byte(strings.Join([]string{
				"private-key local-public-key 51820 off",
				"peer-public-key (none) 198.51.100.10:51820 100.64.0.3/32 1782260000 12 34 25",
			}, "\n")), nil
		case "ip -o link show dev wg0":
			return []byte("2: wg0: <POINTOPOINT> mtu 1420 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n"), nil
		case "ip route get 100.64.0.3":
			return []byte("100.64.0.3 dev wg0 src 100.64.0.2\n"), nil
		case "ping -c 1 -W 1 100.64.0.3":
			return []byte("64 bytes from 100.64.0.3: seq=0 ttl=64 time=0.511 ms\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command %s", got)
		}
	}

	snapshot := BuildAgentSnapshot(context.Background(), networkMap, AgentProbeOptions{
		GeneratedAt:        time.Unix(100, 0).UTC(),
		STUNTimeout:        10 * time.Millisecond,
		RelayTimeout:       10 * time.Millisecond,
		WireGuardInterface: "wg0",
		ProbeRTT:           true,
		Runner:             runner,
	})
	if snapshot.WireGuard == nil || !snapshot.WireGuard.OK || snapshot.WireGuard.Interface != "wg0" || snapshot.WireGuard.MTU != 1420 {
		t.Fatalf("wireguard snapshot = %#v", snapshot.WireGuard)
	}
	if len(snapshot.Paths) != 1 {
		t.Fatalf("paths = %#v", snapshot.Paths)
	}
	path := snapshot.Paths[0]
	if path.Direct.State != "reachable" || path.Direct.RTTMS != 0.511 || path.SelectedPath != "direct" {
		t.Fatalf("path = %#v", path)
	}
}
