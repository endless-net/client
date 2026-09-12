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

// HC-050 / IT-18: a real recipient Client consumes a signed, cross-network
// machine-sharing grant. Access is limited to the granted protocol and port,
// expires without removing the peer, recovers only with a fresh grant, and is
// denied again after the signed peer/grant withdrawal.
func TestControlPlaneNativeMachineSharing(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("machine-sharing-recipient", "198.18.91.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			clientIP := netip.MustParseAddr(status.OverlayIP)
			sharedIP := netip.MustParseAddr("198.18.99.20")
			if family == "ipv6" {
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR = "fd91::/64"
					m.Node.AssignedIPv6 = "fd91::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.OverlayIPv6 == "fd91::1" && v.CachedMapValid })
				clientIP = netip.MustParseAddr(status.OverlayIPv6)
				sharedIP = netip.MustParseAddr("fd99::20")
			}
			underlay := nativePeerUnderlay(t, netip.MustParseAddr(status.OverlayIP), sharedIP)
			snapshot, err := s.Snapshot(status.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			recipientAllowedIPs := []string{netip.PrefixFrom(netip.MustParseAddr(snapshot.Node.AssignedIP), 32).String()}
			if value := snapshot.Node.AssignedIPv6; value != "" {
				recipientAllowedIPs = append(recipientAllowedIPs, netip.PrefixFrom(netip.MustParseAddr(value), 128).String())
			}
			reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, sharedIP, underlay)
			peer := api.Peer{
				ID: "shared-node", NetworkID: "shared-network", Hostname: "shared-node",
				PublicKey: reference.PublicKey, Endpoint: reference.Endpoint,
				EndpointCandidates: []string{reference.Endpoint},
				AllowedIPs:         []string{netip.PrefixFrom(sharedIP, sharedIP.BitLen()).String()},
			}
			grant := func(revision uint64, expires time.Time) api.SharePeerGrant {
				return api.SharePeerGrant{
					GrantID: "machine-share", RecipientNetworkID: snapshot.Network.ID,
					RecipientNodeID: snapshot.Node.ID, RecipientPublicKey: snapshot.Node.PublicKey,
					RecipientAllowedIPs: append([]string(nil), recipientAllowedIPs...),
					SourceNetworkID:     peer.NetworkID, SourceNodeID: peer.ID, SourcePublicKey: peer.PublicKey,
					SourceAllowedIPs: append([]string(nil), peer.AllowedIPs...),
					Rights:           []api.ShareTraffic{{Protocol: "tcp", FirstPort: 24001, LastPort: 24001}},
					Initiation:       api.ShareInitiationRecipientOnly, Revision: revision,
					IssuedAt: time.Now().Add(-time.Second), ExpiresAt: expires,
				}
			}
			apply := func(peers []api.Peer, grants []api.SharePeerGrant) {
				t.Helper()
				if err := s.UpdateMap(status.NodeID, func(m *api.NetworkMapSnapshot) {
					m.Peers = append([]api.Peer(nil), peers...)
					m.Network.SharePeerGrants = append([]api.SharePeerGrant(nil), grants...)
				}); err != nil {
					t.Fatal(err)
				}
				current, err := s.Snapshot(status.NodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.MapRevision >= current.Revision.Network && v.PeerCount == len(peers) &&
						v.Agent != nil && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" &&
						v.WireGuard != nil && v.WireGuard.OK
				})
				if len(peers) != 0 {
					reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(status.WireGuard.ListenPort)))
				}
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			allowedAddress := net.JoinHostPort(sharedIP.String(), "24001")
			wrongPort := net.JoinHostPort(sharedIP.String(), "24002")
			probe := func(protocol, address string) bool { return applicationProbe(t, binary, "", protocol, address) }
			blocked := func() {
				t.Helper()
				if probe("tcp", allowedAddress) || probe("tcp", wrongPort) || probe("udp", allowedAddress) {
					t.Fatal("machine sharing allowed traffic outside the active grant")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool { return probe("tcp", allowedAddress) }); err != nil {
					t.Fatal("granted shared-machine TCP service did not become reachable")
				}
				if probe("tcp", wrongPort) || probe("udp", allowedAddress) {
					t.Fatal("machine-sharing grant permitted an ungranted port or protocol")
				}
			}

			apply(nil, nil)
			blocked()
			expires := time.Now().Add(12 * time.Second)
			apply([]api.Peer{peer}, []api.SharePeerGrant{grant(1, expires)})
			reachable()
			timer := time.NewTimer(time.Until(expires) + 100*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal("sharing lease expiry wait interrupted")
			}
			blocked()
			apply([]api.Peer{peer}, []api.SharePeerGrant{grant(2, time.Now().Add(time.Minute))})
			reachable()
			apply(nil, nil)
			blocked()
		})
	}
}
