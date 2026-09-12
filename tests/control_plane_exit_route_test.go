package tests

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-036: a real native Client consumes an approved IPv4 or IPv6 default route,
// sends application traffic through the selected WireGuard peer, withdraws the
// route without losing control connectivity, and recovers when it is restored.
// The reference peer proves the client egress hop; it is not a public Internet
// service and therefore does not claim a production public-address observation.
func TestControlPlaneNativeExitRoute(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("native-exit-route", "198.18.92.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			clientIP := netip.MustParseAddr(status.OverlayIP)
			resourceIP := netip.MustParseAddr("203.0.113.20")
			peerHost, exitRoute := "198.18.92.20/32", "0.0.0.0/0"
			if family == "ipv6" {
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR = "fd92::/64"
					m.Node.AssignedIPv6 = "fd92::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd92::1" && v.CachedMapValid })
				clientIP = netip.MustParseAddr(status.OverlayIPv6)
				resourceIP = netip.MustParseAddr("2001:db8:ffff::20")
				peerHost, exitRoute = "fd92::20/128", "::/0"
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), resourceIP)
			snapshot, err := s.Snapshot(status.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewRoutedResource(t, snapshot.Node.PublicKey, clientIP, resourceIP, underlay)
			peer := api.Peer{
				ID: "exit-router", Hostname: "exit-router", PublicKey: reference.PublicKey,
				Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint},
				AllowedIPs: []string{peerHost},
			}
			apply := func(allowed []string) {
				t.Helper()
				peer.AllowedIPs = append([]string(nil), allowed...)
				if err := s.UpdatePeers(status.NodeID, []api.Peer{peer}); err != nil {
					t.Fatal(err)
				}
				current, err := s.Snapshot(status.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.MapRevision >= current.Revision.Network && v.PeerCount == 1 &&
						v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK
				})
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			address := net.JoinHostPort(resourceIP.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			blocked := func() {
				t.Helper()
				if probe("tcp") || probe("udp") {
					t.Fatal("egress target remained reachable without an approved default route")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				var tcpOK, udpOK bool
				beforeTo, beforeFrom := reference.ForwardedPacketCounts()
				if err := testclient.Await(ctx, func() bool {
					tcpOK = probe("tcp")
					udpOK = probe("udp")
					return tcpOK && udpOK
				}); err != nil {
					toResource, fromResource := reference.ForwardedPacketCounts()
					t.Fatalf("approved default route did not carry TCP and UDP through the exit peer: tcp=%t udp=%t forwarded=%d/%d before=%d/%d",
						tcpOK, udpOK, toResource, fromResource, beforeTo, beforeFrom)
				}
				toResource, fromResource := reference.ForwardedPacketCounts()
				if toResource <= beforeTo || fromResource <= beforeFrom {
					t.Fatal("egress traffic bypassed the reference forwarding hop")
				}
			}

			apply([]string{peerHost})
			blocked()
			apply([]string{peerHost, exitRoute})
			reachable()
			apply([]string{peerHost})
			blocked()
			apply([]string{peerHost, exitRoute})
			reachable()
		})
	}
}
