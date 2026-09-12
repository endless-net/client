package tests

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
	"golang.org/x/net/dns/dnsmessage"
)

// HC-040: a real native Client consumes a signed application route and
// DNS projection, enforces its TCP target, retires withdrawn/expired access and
// recovers after a fresh signed grant. The resource sits behind the connector.
func TestControlPlaneNativeApplicationRoute(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("native-application", "198.18.93.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			clientIP := netip.MustParseAddr(status.OverlayIP)
			resourceIP := netip.MustParseAddr("198.18.98.20")
			peerHost := "198.18.93.20/32"
			routeCIDR := resourceIP.String() + "/32"
			dnsType := dnsmessage.TypeA
			if family == "ipv6" {
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR = "fd93::/64"
					m.Node.AssignedIPv6 = "fd93::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.OverlayIPv6 == "fd93::1" && v.CachedMapValid
				})
				clientIP = netip.MustParseAddr(status.OverlayIPv6)
				resourceIP = netip.MustParseAddr("fd98::20")
				peerHost = "fd93::20/128"
				routeCIDR = resourceIP.String() + "/128"
				dnsType = dnsmessage.TypeAAAA
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), resourceIP)
			snapshot, err := s.Snapshot(status.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewRoutedResource(t, snapshot.Node.PublicKey, clientIP, resourceIP, underlay)
			connector := api.ServiceHost{NodeID: "application-connector", PublicKey: reference.PublicKey}
			source := api.ServiceHost{NodeID: snapshot.Node.ID, PublicKey: snapshot.Node.PublicKey}
			peer := api.Peer{
				ID:                 connector.NodeID,
				Hostname:           "application-connector",
				PublicKey:          connector.PublicKey,
				Endpoint:           reference.Endpoint,
				EndpointCandidates: []string{reference.Endpoint},
				AllowedIPs:         []string{peerHost},
			}
			application := api.Application{
				ID: "application-portal", Name: "portal", TargetType: "url",
				Target: "https://portal.scenario.endlessnet:24001", DNSEnabled: true,
				PolicyHash: strings.Repeat("a", 64), Sources: []api.ServiceHost{source},
				Connectors: []api.ServiceHost{connector},
			}
			apply := func(routes []api.ApplicationRoute) {
				t.Helper()
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					application.Routes = routes
					m.Peers = []api.Peer{peer}
					m.Network.Applications = []api.Application{application}
				}); err != nil {
					t.Fatal(err)
				}
				current, err := s.Snapshot(status.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.MapRevision >= current.Revision.Network && v.PeerCount == 1 &&
						v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
						v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK
				})
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			address := net.JoinHostPort(resourceIP.String(), "24001")
			wrongPort := net.JoinHostPort(resourceIP.String(), "24002")
			probe := func(protocol, target string) bool { return applicationProbe(t, binary, "", protocol, target) }
			blocked := func() {
				t.Helper()
				if probe("tcp", address) || probe("udp", address) || probe("tcp", wrongPort) {
					t.Fatal("application traffic bypassed its signed route and TCP target")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				beforeTo, beforeFrom := reference.ForwardedPacketCounts()
				if err := testclient.Await(ctx, func() bool { return probe("tcp", address) }); err != nil {
					t.Fatal("application TCP target did not become reachable")
				}
				if probe("udp", address) || probe("tcp", wrongPort) {
					t.Fatal("application grant permitted a protocol or port outside its target")
				}
				toResource, fromResource := reference.ForwardedPacketCounts()
				if toResource <= beforeTo || fromResource <= beforeFrom {
					t.Fatalf("application traffic did not produce fresh connector forwarding: before=%d/%d after=%d/%d", beforeTo, beforeFrom, toResource, fromResource)
				}
			}
			assertDNS := func(code dnsmessage.RCode, expected string) {
				t.Helper()
				assertDNSWireType(t, "udp", clientDNSListenerAddress(status), "portal.scenario.endlessnet.", dnsType, code, expected)
			}
			route := func(expiry time.Time) []api.ApplicationRoute {
				return []api.ApplicationRoute{{Connector: connector, CIDRs: []string{routeCIDR}, ExpiresAt: expiry}}
			}

			apply(nil)
			blocked()
			assertDNS(dnsmessage.RCodeNameError, "")
			restoredRoutes := route(time.Now().Add(time.Minute))
			apply(restoredRoutes)
			reachable()
			assertDNS(dnsmessage.RCodeSuccess, resourceIP.String())

			// Retire the connector's underlay endpoint without changing its key,
			// application identity or route lease. Recovery requires consuming
			// the new endpoint from the signed projection.
			nextEndpoint := reference.RotateEndpoint(t)
			blocked()
			peer.Endpoint = nextEndpoint
			peer.EndpointCandidates = []string{nextEndpoint}
			apply(restoredRoutes)
			reachable()
			assertDNS(dnsmessage.RCodeSuccess, resourceIP.String())
			apply(nil)
			blocked()
			assertDNS(dnsmessage.RCodeNameError, "")

			expires := time.Now().Add(8 * time.Second)
			apply(route(expires))
			reachable()
			assertDNS(dnsmessage.RCodeSuccess, resourceIP.String())
			timer := time.NewTimer(time.Until(expires) + 250*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal("application expiry wait interrupted")
			}
			blocked()
			assertDNS(dnsmessage.RCodeNameError, "")
			apply(route(time.Now().Add(time.Minute)))
			reachable()
			assertDNS(dnsmessage.RCodeSuccess, resourceIP.String())
		})
	}
}
