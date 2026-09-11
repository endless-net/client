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
	exerciseNativeTraffic(t, false, "udp")
}

func TestControlPlaneNativeIPv6UDPTraffic(t *testing.T) {
	exerciseNativeTraffic(t, true, "udp")
}

func TestControlPlaneNativeTCPTraffic(t *testing.T) {
	exerciseNativeTraffic(t, false, "tcp")
}

func TestControlPlaneNativeIPv6TCPTraffic(t *testing.T) {
	exerciseNativeTraffic(t, true, "tcp")
}

func exerciseNativeTraffic(t *testing.T, ipv6 bool, protocol string) {
	t.Helper()
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
	if ipv6 {
		// Supply the dual-stack projection through the signed public contract.
		// The Client must configure its real OS IPv6 address and route itself.
		if err := s.UpdateMap(initial.NodeID, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR = "fd94::/64"
			m.Node.AssignedIPv6 = "fd94::1"
		}); err != nil {
			t.Fatal(err)
		}
		initial = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == initial.NodeID && v.OverlayIPv6 == "fd94::1" && v.CachedMapValid
		})
	}
	m, err := s.Snapshot(initial.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("100.94.0.20")
	clientIP := netip.MustParseAddr(initial.OverlayIP)
	if ipv6 {
		peerIP = netip.MustParseAddr("fd94::20")
		clientIP = netip.MustParseAddr(initial.OverlayIPv6)
	}
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
			if ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && ip != netip.MustParseAddr(initial.OverlayIP) && ip != peerIP {
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
	var reference testwireguard.Peer
	if protocol == "tcp" {
		reference = testwireguard.NewTCP(t, m.Node.PublicKey, clientIP, peerIP, underlay)
	} else {
		reference = testwireguard.NewUDP(t, m.Node.PublicKey, clientIP, peerIP, underlay)
	}
	peer := api.Peer{ID: "protocol-peer", Hostname: "udp-peer", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()}}
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
	fresh := func(port string) bool { return applicationProbe(t, binary, "", protocol, address(port)) }
	if protocol == "tcp" {
		// Diagnose an address collision outside the Client tunnel before the
		// reference peer is published. This is not a positive traffic assertion.
		baseline, _, _ := nativePing(t, peerIP)
		t.Logf("ICMP pre-peer baseline: echo=%t", baseline)
	}
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
			t.Fatalf("native %s failed: endpoint_selected=%t handshake=%t rx=%d tx=%d reference_received=%d reference_echoed=%d", protocol, selected, handshake, rx, tx, received, echoed)
		}
	}
	first := startApplicationSession(t, binary, "", protocol, address("24001"))
	second := startApplicationSession(t, binary, "", protocol, address("24002"))
	first("ok")
	second("ok")
	assertICMP := func(phase string, want bool) {
		t.Helper()
		// Only the protocol-stack peer answers ICMP; the UDP-only channel
		// fixture intentionally implements just its nonce echo protocol.
		if protocol != "tcp" {
			return
		}
		_, _, beforeProbe := reference.HandshakeCounts()
		got, exitCode, pingOutput := nativePing(t, peerIP)
		_, _, afterProbe := reference.HandshakeCounts()
		if got != want {
			status, _ := n.Status()
			// Ping uses a fixed numeric fixture IP; this output contains no Client state.
			t.Logf("ICMP process: exit=%d output=%q", exitCode, pingOutput)
			t.Logf("ICMP diagnostic: phase=%s reference_transport_packets=%d wireguard_status_present=%t", phase, afterProbe-beforeProbe, status.WireGuard != nil)
			t.Fatalf("native ICMP reachability: phase=%s got=%t want=%t", phase, got, want)
		}
	}
	assertICMP("initial", true)
	limited := peer
	limited.ACLRestricted = true
	limited.ACLGrants = []api.ACLGrant{{DestinationCIDRs: peer.AllowedIPs, AllowedPorts: []api.ACLPort{{Protocol: protocol, Port: 24002}}}}
	apply(limited)
	for range 3 {
		second("ok")
		assertICMP("tcp-only-grant", false)
		first("blocked")
		if !fresh("24002") {
			t.Fatal("retained grant stopped working")
		}
		if fresh("24001") {
			t.Fatal("withdrawn grant still permitted new traffic")
		}
		second("ok")
	}
	apply(peer)
	// A denied TCP connection may terminate or enter retransmission backoff.
	// Restored authorization must admit fresh connections; the other port's
	// established connection must remain usable throughout the policy change.
	if protocol == "udp" {
		first("ok")
	}
	second("ok")
	if !fresh("24001") || !fresh("24002") {
		t.Fatal("restored native grants did not recover")
	}
	assertICMP("restored-grant", true)
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	if fresh("24001") || fresh("24002") {
		t.Fatal("disconnected client still delivered overlay traffic")
	}
	assertICMP("disconnected", false)
	// HC-017/HC-018: a new agent process must preserve disconnected intent and
	// enrollment. Restoring connected intent must recover actual traffic using
	// the same public node/key binding held by the unchanged reference peer.
	registrationRequests := func() int {
		count := 0
		for _, event := range s.Events() {
			if event.Kind == "registration-request" {
				count++
			}
		}
		return count
	}
	before := registrationRequests()
	t.Log("native lifecycle: restart while disconnected")
	n.Stop()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateDisconnected && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected && v.NodeID == initial.NodeID && v.NodeCredentialPresent && v.CachedMapValid
	})
	if fresh("24001") || fresh("24002") {
		t.Fatal("agent restart ignored disconnected intent")
	}
	assertICMP("disconnected-restart", false)
	if registrationRequests() != before {
		t.Fatal("disconnected restart attempted credential registration or refresh")
	}
	assertConnected := func() {
		t.Helper()
		v := n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == initial.NodeID && v.OverlayIP == initial.OverlayIP && v.OverlayIPv6 == initial.OverlayIPv6 && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected && v.NodeCredentialPresent && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK && v.WireGuard.ListenPort > 0 && v.WireGuard.ListenPort <= 65535 && len(v.WireGuard.Peers) == 1
		})
		// The Client may choose a new UDP port when its native device restarts.
		// Only the fixture's return endpoint changes; its peer identity/map do not.
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(v.WireGuard.ListenPort)))
		for _, port := range []string{"24001", "24002"} {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			err := testclient.Await(ctx, func() bool { return fresh(port) })
			cancel()
			if err != nil {
				t.Fatal("connected intent did not restore native traffic")
			}
		}
		assertICMP("connected", true)
		// Registration also renews an existing node credential. The contract
		// fixture validates the original identity/key/fingerprint binding before
		// recording a refresh; only a newly created node is another enrollment.
		created := 0
		for _, event := range s.Events() {
			if event.Kind == "registered" {
				created++
			}
			if (event.Kind == "registered" || event.Kind == "registration-refreshed") && event.NodeID != initial.NodeID {
				t.Fatal("connection intent recovery changed the registered identity")
			}
		}
		if created != 1 {
			t.Fatal("connection intent recovery created another enrollment")
		}
	}
	t.Log("native lifecycle: reconnect with retained identity")
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	assertConnected()
	n.Service("connect", &connected)
	t.Log("native lifecycle: restart while connected")
	n.Stop()
	n.Start()
	assertConnected()

	// HC-065 / IT-20: retain the reference peer's keys and routes throughout
	// terminal retirement. Denial must follow the Client's credential handling,
	// not a peer-map withdrawal or an application shutdown in the fixture.
	t.Log("native lifecycle: revoke credential with established traffic")
	retiredSession := startApplicationSession(t, binary, "", protocol, address("24001"))
	retiredSession("ok")
	before = registrationRequests()
	if err := s.Revoke(initial.NodeID); err != nil {
		t.Fatal(err)
	}
	for phase := range 2 {
		if phase == 1 {
			t.Log("native lifecycle: restart after terminal retirement")
			n.Stop()
			n.Start()
		}
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && !v.NodeCredentialPresent && !v.CachedMapPresent
		})
		retiredSession("blocked")
		assertICMP("retired", false)
		if fresh("24001") || fresh("24002") {
			t.Fatal("retired client still delivered fresh native traffic")
		}
		if registrationRequests() != before {
			t.Fatal("terminal retirement or restart attempted enrollment")
		}
	}
}
