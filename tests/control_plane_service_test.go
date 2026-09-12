package tests

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-051: a real native Client consumes a signed logical-service catalog with
// two reachable hosts, removes a drained host, fails closed without approval
// and restores the service. Host traffic uses the real OS and WireGuard path.
func TestControlPlaneNativeServiceCatalog(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("native-service", "198.18.92.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			clientIP := netip.MustParseAddr(status.OverlayIP)
			hostIPs := []netip.Addr{netip.MustParseAddr("198.18.92.20"), netip.MustParseAddr("198.18.92.21")}
			prefixBits := 32
			lookupNetwork := "ip4"
			if family == "ipv6" {
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR = "fd92::/64"
					m.Node.AssignedIPv6 = "fd92::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.OverlayIPv6 == "fd92::1" && v.CachedMapValid
				})
				clientIP = netip.MustParseAddr(status.OverlayIPv6)
				hostIPs = []netip.Addr{netip.MustParseAddr("fd92::20"), netip.MustParseAddr("fd92::21")}
				prefixBits = 128
				lookupNetwork = "ip6"
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), hostIPs[0])
			snapshot, err := s.Snapshot(status.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			references := make([]testwireguard.Peer, len(hostIPs))
			peers := make([]api.Peer, len(hostIPs))
			hosts := make([]api.ServiceHost, len(hostIPs))
			for i, hostIP := range hostIPs {
				references[i] = testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, hostIP, underlay)
				hosts[i] = api.ServiceHost{NodeID: "service-host-" + string(rune('a'+i)), PublicKey: references[i].PublicKey}
				prefix := netip.PrefixFrom(hostIP, prefixBits).String()
				peers[i] = api.Peer{
					ID: hosts[i].NodeID, Hostname: hosts[i].NodeID, PublicKey: hosts[i].PublicKey,
					Endpoint: references[i].Endpoint, EndpointCandidates: []string{references[i].Endpoint},
					AllowedIPs: []string{prefix}, ACLRestricted: true,
					ACLGrants: []api.ACLGrant{{DestinationCIDRs: []string{prefix}, AllowedPorts: []api.ACLPort{{Protocol: "tcp", Port: 24001}}}},
				}
			}
			service := api.AdvertisedService{
				ID: "database", Name: "database", DNSName: "database.scenario.endlessnet",
				Ports:        []api.ServicePort{{Protocol: "tcp", Port: 24001}},
				ApprovalMode: "manual", ApprovalStatus: "pending",
			}
			apply := func(approval string, selected []api.ServiceHost) {
				t.Helper()
				service.ApprovalStatus = approval
				service.Hosts = selected
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Peers = peers
					m.Network.Services = []api.AdvertisedService{service}
				}); err != nil {
					t.Fatal(err)
				}
				current, err := s.Snapshot(status.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.MapRevision >= current.Revision.Network && v.PeerCount == len(peers) &&
						v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
						v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK
				})
				for i := range references {
					references[i].SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
				}
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			assertHostTraffic := func(hostIP netip.Addr) {
				t.Helper()
				address := net.JoinHostPort(hostIP.String(), "24001")
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool { return applicationProbe(t, binary, "", "tcp", address) }); err != nil {
					t.Fatal("approved service host did not become reachable")
				}
				if applicationProbe(t, binary, "", "udp", address) || applicationProbe(t, binary, "", "tcp", net.JoinHostPort(hostIP.String(), "24002")) {
					t.Fatal("service host policy allowed an undeclared protocol or port")
				}
			}

			apply("pending", nil)
			assertServiceDNS(t, clientDNSListenerAddress(status), lookupNetwork, nil)
			apply("approved", hosts)
			resolved := assertServiceDNS(t, clientDNSListenerAddress(status), lookupNetwork, hostIPs)
			for _, address := range resolved {
				assertHostTraffic(address)
			}
			apply("approved", hosts[1:])
			assertServiceDNS(t, clientDNSListenerAddress(status), lookupNetwork, hostIPs[1:])
			assertHostTraffic(hostIPs[1])
			apply("pending", nil)
			assertServiceDNS(t, clientDNSListenerAddress(status), lookupNetwork, nil)
			apply("approved", hosts)
			resolved = assertServiceDNS(t, clientDNSListenerAddress(status), lookupNetwork, hostIPs)
			for _, address := range resolved {
				assertHostTraffic(address)
			}
		})
	}
}

func assertServiceDNS(t *testing.T, listener, network string, expected []netip.Addr) []netip.Addr {
	t.Helper()
	resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", listener)
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	addresses, err := resolver.LookupNetIP(ctx, network, "database.scenario.endlessnet")
	if len(expected) == 0 {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound || len(addresses) != 0 {
			t.Fatal("unapproved service did not return DNS name-not-found")
		}
		return nil
	}
	if err != nil {
		t.Fatal("approved service DNS lookup failed")
	}
	slices.SortFunc(addresses, func(a, b netip.Addr) int { return a.Compare(b) })
	want := slices.Clone(expected)
	slices.SortFunc(want, func(a, b netip.Addr) int { return a.Compare(b) })
	if !slices.Equal(addresses, want) {
		t.Fatal("service DNS did not return exactly the signed host set")
	}
	return addresses
}
