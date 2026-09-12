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

// HC-030: a previously accepted signature expires during a control outage.
// Expired authority must not survive either a running agent or its restart.
func TestControlPlaneNativeCachedMapExpiry(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("cache-expiry", "198.18.95.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			nodeID := status.NodeID
			clientIP, peerIP := netip.MustParseAddr(status.OverlayIP), netip.MustParseAddr("198.18.95.20")
			if family == "ipv6" {
				if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR, m.Node.AssignedIPv6 = "fd95::/64", "fd95::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd95::1" && v.CachedMapValid })
				clientIP, peerIP = netip.MustParseAddr(status.OverlayIPv6), netip.MustParseAddr("fd95::20")
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), peerIP)
			snapshot, err := s.Snapshot(nodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
			peers := []api.Peer{{ID: "cache-peer", Hostname: "cache-peer", PublicKey: reference.PublicKey,
				Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint},
				AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()}}}
			if err := s.UpdatePeers(nodeID, peers); err != nil {
				t.Fatal(err)
			}
			ready := func() {
				t.Helper()
				current, err := s.Snapshot(nodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.NodeID == nodeID && v.NodeCredentialPresent && v.CachedMapValid && v.PeerCount == 1 &&
						v.MapRevision >= current.Revision.Network && v.Agent != nil &&
						v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK
				})
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
			}
			ready()
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			address := net.JoinHostPort(peerIP.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool {
					tcpOK, udpOK := probe("tcp"), probe("udp")
					return tcpOK && udpOK
				}); err != nil {
					t.Fatal("valid cached authority did not carry TCP and UDP traffic")
				}
			}
			reachable()
			issuedAt := time.Now().UTC().Truncate(time.Second)
			expires := issuedAt.Add(30 * time.Second)
			if err := s.SetMapValidity(nodeID, issuedAt, 30*time.Second); err != nil {
				t.Fatal(err)
			}
			ready()
			s.SetUnavailable(true)
			defer s.SetUnavailable(false)
			reachable()
			timer := time.NewTimer(time.Until(expires) + 500*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal("cached map expiry wait interrupted")
			}
			assertExpired := func() {
				t.Helper()
				n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.NodeID == nodeID && v.NodeCredentialPresent && !v.CachedMapValid && !v.UserDisconnected
				})
				if probe("tcp") || probe("udp") {
					t.Fatal("expired cached authority still permitted application traffic")
				}
			}
			assertExpired()
			n.Stop()
			if _, err := n.Run("sync", "--config", n.Config, "--offline"); err == nil {
				t.Fatal("offline CLI accepted an expired cached map")
			}
			n.Start()
			assertExpired()
			if err := s.UpdatePeers(nodeID, peers); err != nil {
				t.Fatal(err)
			}
			s.SetUnavailable(false)
			ready()
			reachable()
		})
	}
}
