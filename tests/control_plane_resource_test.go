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

// HC-032: a real native Client consumes a signed resource prefix through an
// encrypted routing peer, with a distinct resource stack behind an IP hop.
func TestControlPlaneRoutedResource(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("routed-resource", "198.18.94.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			initial := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			resource := netip.MustParseAddr("198.18.96.20")
			prefix := "198.18.96.0/24"
			peerHost := "198.18.94.20/32"
			clientIP := netip.MustParseAddr(initial.OverlayIP)
			if family == "ipv6" {
				if err := s.UpdateMap(initial.NodeID, func(m *api.NetworkMapSnapshot) { m.Network.IPv6CIDR = "fd94::/64"; m.Node.AssignedIPv6 = "fd94::1" }); err != nil {
					t.Fatal(err)
				}
				n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd94::1" && v.CachedMapValid })
				clientIP, resource = netip.MustParseAddr("fd94::1"), netip.MustParseAddr("fd96::20")
				prefix, peerHost = "fd96::/64", "fd94::20/128"
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(initial.OverlayIP), resource)
			snapshot, err := s.Snapshot(initial.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewRoutedResource(t, snapshot.Node.PublicKey, clientIP, resource, underlay)
			peer := api.Peer{ID: "resource-router", Hostname: "resource-router", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{peerHost}}
			routeTable := "auto"
			apply := func(p api.Peer) {
				t.Helper()
				if err := s.UpdatePeers(initial.NodeID, []api.Peer{p}); err != nil {
					t.Fatal(err)
				}
				m, err := s.Snapshot(initial.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				v := n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.NodeID == initial.NodeID && v.RouteTable == routeTable && v.MapRevision >= m.Revision.Network && v.PeerCount == 1 && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK && v.WireGuard.ListenPort > 0 && v.WireGuard.ListenPort <= 65535 && v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == ""
				})
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(v.WireGuard.ListenPort)))
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			address := net.JoinHostPort(resource.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			blocked := func() {
				t.Helper()
				if probe("tcp") || probe("udp") {
					t.Fatal("resource reachable while routing is unavailable or disabled")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool { return probe("tcp") && probe("udp") }); err != nil {
					t.Fatal("routed TCP/UDP resource did not become reachable")
				}
				toResource, fromResource := reference.ForwardedPacketCounts()
				if toResource == 0 || fromResource == 0 {
					t.Fatal("resource traffic bypassed the IP-forwarding hop")
				}
			}
			apply(peer)
			blocked()
			routed := peer
			routed.AllowedIPs = []string{peerHost, prefix}
			apply(routed)
			reachable()

			// HC-019/HC-033: the signed resource prefix remains available while
			// the operator disables OS routes through the public CLI. Change
			// offline preferences with the agent stopped, then test durability.
			setRouteTable := func(value string) {
				t.Helper()
				n.Stop()
				n.MustRun("sync", "--config", n.Config, "--offline", "--route-table", value)
				routeTable = value
				n.Start()
				apply(routed)
			}
			setRouteTable("off")
			blocked()
			n.Stop()
			n.Start()
			apply(routed)
			blocked()
			setRouteTable("auto")
			reachable()
			registrations := 0
			for _, event := range s.Events() {
				if event.Kind == "registered" {
					registrations++
				}
			}
			if registrations != 1 {
				t.Fatal("route preference changes unexpectedly created a new registration")
			}
			tcp := startApplicationSession(t, binary, "", "tcp", address)
			udp := startApplicationSession(t, binary, "", "udp", address)
			tcp("ok")
			udp("ok")
			apply(peer)
			tcp("blocked")
			udp("blocked")
			blocked()
			apply(routed)
			reachable()
		})
	}
}
