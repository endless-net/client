package tests

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-019: public CLI preferences persist independently of the agent process.
// Observe both public IPC and the OS interface, without reading saved state.
func TestControlPlaneNativeMTUPreference(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("mtu-preference", "198.18.94.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto", "--mtu", "1280")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			nodeID := status.NodeID
			clientIP, peerIP := netip.MustParseAddr(status.OverlayIP), netip.MustParseAddr("198.18.94.20")
			if family == "ipv6" {
				if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR, m.Node.AssignedIPv6 = "fd94::/64", "fd94::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd94::1" && v.CachedMapValid })
				clientIP, peerIP = netip.MustParseAddr(status.OverlayIPv6), netip.MustParseAddr("fd94::20")
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), peerIP)
			snapshot, err := s.Snapshot(nodeID)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
			if err := s.UpdatePeers(nodeID, []api.Peer{{ID: "mtu-peer", Hostname: "mtu-peer", PublicKey: reference.PublicKey,
				Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint},
				AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()}}}); err != nil {
				t.Fatal(err)
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			check := func(mtu int) {
				t.Helper()
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.NodeID == nodeID && v.CachedMapValid && v.PeerCount == 1 &&
						v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK && v.WireGuard.MTU == mtu
				})
				iface, err := net.InterfaceByName(status.WireGuard.Interface)
				if err != nil || iface.MTU != mtu {
					t.Fatal("OS interface MTU does not match the persisted CLI preference")
				}
				reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool {
					address := net.JoinHostPort(peerIP.String(), "24001")
					tcpOK := applicationProbe(t, binary, "", "tcp", address)
					udpOK := applicationProbe(t, binary, "", "udp", address)
					return tcpOK && udpOK
				}); err != nil {
					t.Fatal("persisted MTU preference did not preserve TCP and UDP traffic")
				}
			}
			check(1280)
			for _, mtu := range []int{1400, 1279, 0} {
				n.Stop()
				_, err := n.Run("sync", "--config", n.Config, "--offline", "--mtu", strconv.Itoa(mtu))
				if (err != nil) != (mtu == 1279) {
					t.Fatalf("unexpected CLI MTU validation result for %d: rejected=%t", mtu, err != nil)
				}
				n.Start()
				expected := 1400
				if mtu == 0 {
					expected = 1420
				}
				check(expected)
			}
		})
	}
}
