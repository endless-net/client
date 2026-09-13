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

// HC-050 / IT-18: a real recipient Client consumes a signed, cross-network
// machine-sharing grant. Access is limited to the granted protocol and port,
// expires without removing the peer, recovers only with a fresh grant, and is
// denied again after the signed peer/grant withdrawal.
func TestControlPlaneNativeMachineSharing(t *testing.T) {
	requireControlScenario(t)
	for _, family := range []string{"ipv4", "ipv6"} {
		t.Run(family, func(t *testing.T) {
			s := testcontrol.NewTLS(t)
			network, token, err := s.AddNetwork("machine-sharing-recipient", "198.18.91.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.TrustControlTLS(s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			status := n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid() && nativeOverlayAddress(v, false).IsValid()
			})
			nodeID, profileID := status.NodeId, status.ActiveProfileId
			runNativeControlMutation(t, n, "connect", "a5020000-0000-4000-8000-000000000001")
			status = n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == nodeID && v.ActiveProfileId == profileID &&
					v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED &&
					v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
			})
			clientIP := nativeOverlayAddress(status, false)
			sharedIP := netip.MustParseAddr("198.18.99.20")
			if family == "ipv6" {
				if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
					m.Network.IPv6CIDR = "fd91::/64"
					m.Node.AssignedIPv6 = "fd91::1"
				}); err != nil {
					t.Fatal(err)
				}
				status = n.AwaitNativeStatus(func(v *ipc.Status) bool {
					return v.NodeId == nodeID && v.ActiveProfileId == profileID && nativeOverlayAddress(v, true).String() == "fd91::1" && v.GetStoredState().GetCachedMapValid()
				})
				clientIP = nativeOverlayAddress(status, true)
				sharedIP = netip.MustParseAddr("fd99::20")
			}
			underlay := nativePeerUnderlay(t, nativeOverlayAddress(status, false), sharedIP)
			snapshot, err := s.Snapshot(nodeID)
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
				if err := s.UpdateMap(nodeID, func(m *api.NetworkMapSnapshot) {
					m.Peers = append([]api.Peer(nil), peers...)
					m.Network.SharePeerGrants = append([]api.SharePeerGrant(nil), grants...)
				}); err != nil {
					t.Fatal(err)
				}
				current, err := s.Snapshot(nodeID)
				if err != nil {
					t.Fatal(err)
				}
				status = awaitNativePeerMap(t, n, status, current.Revision.Network, uint32(len(peers)))
				if len(peers) != 0 {
					reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, n, status)))
				}
			}
			binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
			allowedAddress := net.JoinHostPort(sharedIP.String(), "24001")
			wrongPort := net.JoinHostPort(sharedIP.String(), "24002")
			probe := func(protocol, address string) bool { return applicationProbe(t, binary, "", protocol, address) }
			verifiedProbe := func(protocol, address string) bool {
				beforeReceived, beforeEchoed := reference.PacketCounts()
				if !probe(protocol, address) {
					return false
				}
				received, echoed := reference.PacketCounts()
				return received > beforeReceived && echoed > beforeEchoed
			}
			blocked := func() {
				t.Helper()
				tcpOK, wrongPortOK, udpOK := probe("tcp", allowedAddress), probe("tcp", wrongPort), probe("udp", allowedAddress)
				if tcpOK || wrongPortOK || udpOK {
					t.Fatal("machine sharing allowed traffic outside the active grant")
				}
			}
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool { return verifiedProbe("tcp", allowedAddress) }); err != nil {
					t.Fatal("granted shared-machine TCP service did not become reachable")
				}
				wrongPortOK, udpOK := probe("tcp", wrongPort), probe("udp", allowedAddress)
				if wrongPortOK || udpOK {
					t.Fatal("machine-sharing grant permitted an ungranted port or protocol")
				}
			}

			apply(nil, nil)
			blocked()
			expires := time.Now().Add(30 * time.Second)
			apply([]api.Peer{peer}, []api.SharePeerGrant{grant(1, expires)})
			reachable()
			session := startApplicationSession(t, binary, "", "tcp", allowedAddress)
			session("ok")
			timer := time.NewTimer(time.Until(expires) + 100*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-t.Context().Done():
				t.Fatal("sharing lease expiry wait interrupted")
			}
			session("blocked")
			blocked()
			n.Stop()
			n.Start()
			status = awaitNativePeerMap(t, n, status, status.MapRevision, 1)
			reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, nativeTunnelPort(t, n, status)))
			session("blocked")
			blocked()
			apply([]api.Peer{peer}, []api.SharePeerGrant{grant(2, time.Now().Add(time.Minute))})
			reachable()
			// Change rights while retaining the same peer and grant identity. The
			// reference serves every tested port/protocol, so the newly allowed
			// probes also establish that earlier denials were not dead services.
			replacement := grant(3, time.Now().Add(time.Minute))
			replacement.Rights = []api.ShareTraffic{
				{Protocol: "tcp", FirstPort: 24002, LastPort: 24002},
				{Protocol: "udp", FirstPort: 24001, LastPort: 24001},
			}
			apply([]api.Peer{peer}, []api.SharePeerGrant{replacement})
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			err = testclient.Await(ctx, func() bool {
				tcpOK := verifiedProbe("tcp", wrongPort)
				udpOK := verifiedProbe("udp", allowedAddress)
				return tcpOK && udpOK
			})
			cancel()
			if err != nil {
				t.Fatal("updated sharing rights did not enable the newly granted TCP port and UDP protocol")
			}
			tcpOK, udpOK := probe("tcp", allowedAddress), probe("udp", wrongPort)
			if tcpOK || udpOK {
				t.Fatal("updated sharing rights retained revoked TCP access or allowed an ungranted UDP port")
			}
			apply([]api.Peer{peer}, []api.SharePeerGrant{grant(4, time.Now().Add(time.Minute))})
			reachable()
			apply(nil, nil)
			blocked()
		})
	}
}
