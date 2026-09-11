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
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-024/HC-027: the real OS routes application packets into the real Client.
// The reference peer's overlay address is not local to this host, preventing
// a same-host shortcut that could falsely prove encrypted packet delivery.
func TestControlPlaneNativeUDPTraffic(t *testing.T) {
	requireControlScenario(t)
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	if !filepath.IsAbs(binary) {
		t.Fatal("native traffic requires the packetprobe binary")
	}
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("native-udp", "100.94.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, join, "--route-table", "auto")
	n.Start()
	defer n.Stop()
	initial := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
	m, err := s.Snapshot(initial.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("100.94.0.20")
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr() == peerIP {
			t.Fatal("reference peer overlay IP is assigned to the host")
		}
	}
	clientIP := netip.MustParseAddr(initial.OverlayIP)
	var underlay netip.Addr
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		values, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			prefix, err := netip.ParsePrefix(value.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr()
			if ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && ip != clientIP && ip != peerIP {
				underlay = ip
				break
			}
		}
		if underlay.IsValid() {
			break
		}
	}
	if !underlay.IsValid() {
		t.Fatal("runner has no usable IPv4 underlay interface")
	}
	reference := testwireguard.NewUDP(t, m.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{ID: "protocol-peer", Hostname: "udp-peer", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{peerIP.String() + "/32"}}
	apply := func(desired api.Peer) {
		t.Helper()
		if err := s.UpdatePeers(initial.NodeID, []api.Peer{desired}); err != nil {
			t.Fatal(err)
		}
		current, err := s.Snapshot(initial.NodeID)
		if err != nil {
			t.Fatal(err)
		}
		applied := n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.MapRevision >= current.Revision.Network && v.PeerCount == 1 && v.Agent != nil && v.Agent.StatePresent && v.Agent.SnapshotState == ipc.AgentSnapshotCurrent && v.Agent.MapRevision == v.MapRevision && v.Agent.LastError == "" && v.WireGuard != nil && v.WireGuard.OK
		})
		if len(applied.WireGuard.Peers) != 1 || applied.WireGuard.Peers[0].Endpoint != reference.Endpoint {
			t.Fatal("client did not select the fixture's signed direct endpoint")
		}
		if applied.WireGuard.ListenPort <= 0 || applied.WireGuard.ListenPort > 65535 {
			t.Fatal("client did not publish a usable WireGuard listen port")
		}
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(applied.WireGuard.ListenPort)))
	}
	address := func(port string) string { return net.JoinHostPort(peerIP.String(), port) }
	fresh := func(port string) bool { return applicationProbe(t, binary, "", "udp", address(port)) }
	apply(peer)
	for _, port := range []string{"24001", "24002"} {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		err := testclient.Await(ctx, func() bool { return fresh(port) })
		cancel()
		if err != nil {
			v, _ := n.Status()
			var handshake bool
			var rx, tx uint64
			var selected bool
			if v.WireGuard != nil {
				for _, p := range v.WireGuard.Peers {
					handshake = handshake || p.LatestHandshakeUnix > 0
					rx += p.TransferRXBytes
					tx += p.TransferTXBytes
					selected = selected || p.Endpoint == reference.Endpoint
				}
			}
			received, echoed := reference.PacketCounts()
			initiations, responses, other := reference.HandshakeCounts()
			t.Logf("reference handshake: initiations_received=%d responses_sent=%d other_received=%d", initiations, responses, other)
			t.Fatalf("native UDP failed: endpoint_selected=%t handshake=%t rx=%d tx=%d reference_received=%d reference_echoed=%d", selected, handshake, rx, tx, received, echoed)
		}
	}
	first := startApplicationSession(t, binary, "", "udp", address("24001"))
	second := startApplicationSession(t, binary, "", "udp", address("24002"))
	first("ok")
	second("ok")
	limited := peer
	limited.ACLRestricted = true
	limited.ACLGrants = []api.ACLGrant{{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []api.ACLPort{{Protocol: "udp", Port: 24002}}}}
	apply(limited)
	for range 3 {
		second("ok")
		first("blocked")
		if !fresh("24002") {
			t.Fatal("retained UDP grant stopped working")
		}
		if fresh("24001") {
			t.Fatal("withdrawn UDP grant still permitted new traffic")
		}
		second("ok")
	}
	apply(peer)
	first("ok")
	second("ok")
	if !fresh("24001") || !fresh("24002") {
		t.Fatal("restored native UDP grants did not recover")
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	if fresh("24001") || fresh("24002") {
		t.Fatal("disconnected client still delivered overlay traffic")
	}
}
