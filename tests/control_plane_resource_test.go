package tests

import (
	"context"
	"net"
	"net/netip"

	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
)

// HC-032: a real native Client consumes a signed resource prefix through an
// encrypted routing peer, with a distinct resource stack behind an IP hop.
func TestControlPlaneRoutedResource(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.NewTLS(t)
			network, token, err := s.AddNetwork("routed-resource", "198.18.94.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.TrustControlTLS(s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			runNativeControlMutation(t, n, "connect", "6b130000-0000-4000-8000-000000000001")
			initial := n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid() && nativeOverlayAddress(v, false).IsValid()
			})
			resource := netip.MustParseAddr("198.18.96.20")
			prefix := "198.18.96.0/24"
			peerHost := "198.18.94.20/32"
			clientIP := nativeOverlayAddress(initial, false)
			if family == "ipv6" {
				if err := s.UpdateMap(initial.NodeId, func(m *api.NetworkMapSnapshot) { m.Network.IPv6CIDR = "fd94::/64"; m.Node.AssignedIPv6 = "fd94::1" }); err != nil {
					t.Fatal(err)
				}
				initial = n.AwaitNativeStatus(func(v *ipc.Status) bool {
					return v.NodeId == initial.NodeId && v.ActiveProfileId == initial.ActiveProfileId && nativeOverlayAddress(v, true).String() == "fd94::1" && v.GetStoredState().GetCachedMapValid()
				})
				clientIP, resource = nativeOverlayAddress(initial, true), netip.MustParseAddr("fd96::20")
				prefix, peerHost = "fd96::/64", "fd94::20/128"
			}
			underlay := nativePeerUnderlay(t, nativeOverlayAddress(initial, false), resource)
			snapshot, err := s.Snapshot(initial.NodeId)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewRoutedResource(t, snapshot.Node.PublicKey, clientIP, resource, underlay)
			peer := api.Peer{ID: "resource-router", Hostname: "resource-router", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{peerHost}}
			routeTable := "auto"
			apply := func(p api.Peer) {
				t.Helper()
				if err := s.UpdatePeers(initial.NodeId, []api.Peer{p}); err != nil {
					t.Fatal(err)
				}
				m, err := s.Snapshot(initial.NodeId)
				if err != nil {
					t.Fatal(err)
				}
				v := n.AwaitNativeStatus(func(v *ipc.Status) bool {
					return nativePeerMapApplied(v, initial, m.Revision.Network, 1) && v.RouteTable == routeTable
				})
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, n, v)))
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			address := net.JoinHostPort(resource.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			blocked := func() {
				t.Helper()
				tcpOK, udpOK := probe("tcp"), probe("udp")
				if tcpOK || udpOK {
					t.Fatal("resource reachable while routing is unavailable or disabled")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				for _, protocol := range []string{"tcp", "udp"} {
					if err := testclient.Await(ctx, func() bool {
						beforeTo, beforeFrom := reference.ForwardedPacketCounts()
						if !probe(protocol) {
							return false
						}
						toResource, fromResource := reference.ForwardedPacketCounts()
						return toResource > beforeTo && fromResource > beforeFrom
					}); err != nil {
						t.Fatalf("routed %s resource did not produce fresh IP-forwarding traffic", protocol)
					}
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
