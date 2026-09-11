package tests

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testrelay"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
	relay "github.com/endless-net/relay/protocol/v1"
)

// HC-028/HC-030: no direct endpoint is published. All real native application
// traffic must cross the public TLS Relay contract before reaching WireGuard.
func TestControlPlaneNativeRelayTraffic(t *testing.T) {
	requireControlScenario(t)
	binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	for _, ipv6 := range []bool{false, true} {
		name := "ipv4"
		if ipv6 {
			name = "ipv6"
		}
		t.Run(name, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("relay-traffic", "198.18.94.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			initial := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			peerIP := netip.MustParseAddr("198.18.94.20")
			clientIP := netip.MustParseAddr(initial.OverlayIP)
			if ipv6 {
				if err := s.UpdateMap(initial.NodeID, func(m *api.NetworkMapSnapshot) { m.Network.IPv6CIDR = "fd94::/64"; m.Node.AssignedIPv6 = "fd94::1" }); err != nil {
					t.Fatal(err)
				}
				n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd94::1" && v.CachedMapValid })
				clientIP, peerIP = netip.MustParseAddr("fd94::1"), netip.MustParseAddr("fd94::20")
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(initial.OverlayIP), peerIP)
			m, err := s.Snapshot(initial.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewTCP(t, m.Node.PublicKey, clientIP, peerIP, underlay)
			transport := testrelay.New(t, network.ID, initial.NodeID, "relay-peer", reference.Endpoint)
			caFile := filepath.Join(t.TempDir(), "relay-ca.pem")
			if err := os.WriteFile(caFile, transport.CertificatePEM, 0o600); err != nil {
				t.Fatal("could not write public Relay test CA")
			}
			n.Stop()
			n.AgentArgs = append(n.AgentArgs, "--relay-ca-cert", caFile)
			if err := s.UpdateMap(initial.NodeID, func(m *api.NetworkMapSnapshot) {
				m.Peers = []api.Peer{{ID: "relay-peer", NetworkID: network.ID, Hostname: "relay-peer", PublicKey: reference.PublicKey, AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()}}}
				m.Relays = []relay.Endpoint{transport.Endpoint}
				m.RelayCredential = &transport.Credential
			}); err != nil {
				t.Fatal(err)
			}
			n.Start()
			selected := func() {
				t.Helper()
				n.AwaitStatus(func(v ipc.StatusResponse) bool {
					if v.NodeID != initial.NodeID || !v.CachedMapValid || v.Agent == nil || !v.Agent.RelayOK || v.Agent.SelectedRelay.ID != transport.Endpoint.ID || v.WireGuard == nil || !v.WireGuard.OK {
						return false
					}
					for _, p := range v.Agent.Peers {
						if p.PeerID == "relay-peer" && p.SelectedPath == "relay" {
							return true
						}
					}
					return false
				})
			}
			address := net.JoinHostPort(peerIP.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			reachable := func() {
				t.Helper()
				selected()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool { return probe("tcp") && probe("udp") }); err != nil {
					t.Fatal("native TCP/UDP did not traverse Relay")
				}
				authenticated, sent, received := transport.Counts()
				if authenticated == 0 || sent == 0 || received == 0 {
					t.Fatal("application traffic bypassed Relay contract participant")
				}
			}
			reachable()
			tcp := startApplicationSession(t, binary, "", "tcp", address)
			udp := startApplicationSession(t, binary, "", "udp", address)
			tcp("ok")
			udp("ok")
			transport.SetUnavailable(true)
			tcp("blocked")
			udp("blocked")
			if probe("tcp") || probe("udp") {
				t.Fatal("application remained reachable without its only Relay path")
			}
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == initial.NodeID && v.NodeCredentialPresent && v.CachedMapPresent
			})
			transport.SetUnavailable(false)
			reachable()
			n.Stop()
			n.Start()
			reachable()
			registrations := 0
			for _, event := range s.Events() {
				if event.Kind == "registered" {
					registrations++
				}
			}
			if registrations != 1 {
				t.Fatal("Relay recovery unexpectedly registered a new Client identity")
			}
		})
	}
}
