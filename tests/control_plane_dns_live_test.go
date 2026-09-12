package tests

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
	"golang.org/x/net/dns/dnsmessage"
)

// HC-017/HC-025: the operating-system resolver selects the running Client's
// DNS configuration, including a map received while connection intent is off.
func TestControlPlaneNativeSystemDNS(t *testing.T) {
	requireControlScenario(t)
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	if !filepath.IsAbs(binary) {
		t.Fatal("system DNS requires the packetprobe binary")
	}
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("native-system-dns", "198.18.97.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, join, "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return nativeDNSMapApplied(v, "", 0) })
	id := status.NodeID
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	setPeer := func(hostname, address string) {
		t.Helper()
		previous := status.MapRevision
		if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
			m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
			m.Peers = []api.Peer{{ID: "system-dns-peer", Hostname: hostname, PublicKey: public, AllowedIPs: []string{address + "/32"}}}
		}); err != nil {
			t.Fatal(err)
		}
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return nativeDNSMapApplied(v, id, previous) })
	}
	setPeer("system-peer-one", "198.18.97.20")
	assertSystemDNSAddress(t, binary, "system-peer-one.scenario.endlessnet", "198.18.97.20")

	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	previous := status.MapRevision
	if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
		m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
		m.Peers = []api.Peer{{ID: "system-dns-peer", Hostname: "system-peer-two", PublicKey: public, AllowedIPs: []string{"198.18.97.21/32"}}}
	}); err != nil {
		t.Fatal(err)
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, id, previous) && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected
	})
	assertSystemDNSAddress(t, binary, "system-peer-two.scenario.endlessnet", "198.18.97.21")
}

// HC-025: the running native agent applies DNS changes from signed maps.
// Queries target its DNS listener; this does not prove OS resolver selection.
func TestControlPlaneNativeDNSMapUpdates(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("native-dns", "198.18.96.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, join, "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, "", 0)
	})
	id := status.NodeID
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []struct{ hostname, ipv4, ipv6 string }{
		{"live-peer", "198.18.96.20", "fd96::20"},
		{"live-peer", "198.18.96.21", "fd96::21"},
		{"renamed-peer", "198.18.96.21", "fd96::21"},
		{},
		{"live-peer", "198.18.96.20", "fd96::20"},
	} {
		if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR = "fd96::/64"
			m.Node.AssignedIPv6 = "fd96::1"
			m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
			m.Peers = nil
			if phase.hostname != "" {
				m.Peers = []api.Peer{{ID: "live-dns-peer", Hostname: phase.hostname, PublicKey: public, AllowedIPs: []string{phase.ipv4 + "/32", phase.ipv6 + "/128"}}}
			}
		}); err != nil {
			t.Fatal(err)
		}
		previous := status.MapRevision
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return nativeDNSMapApplied(v, id, previous)
		})
		for _, transport := range []string{"udp", "tcp"} {
			for _, hostname := range []string{"live-peer", "renamed-peer"} {
				code, ipv4, ipv6 := dnsmessage.RCodeNameError, "", ""
				if hostname == phase.hostname {
					code, ipv4, ipv6 = dnsmessage.RCodeSuccess, phase.ipv4, phase.ipv6
				}
				name := hostname + ".scenario.endlessnet."
				assertDNSWire(t, transport, "127.0.0.1:53", name, code, ipv4)
				assertDNSWireType(t, transport, "127.0.0.1:53", name, dnsmessage.TypeAAAA, code, ipv6)
			}
		}
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	for _, transport := range []string{"udp", "tcp"} {
		assertDNSListenerUnavailable(t, transport, "127.0.0.1:53")
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, id, 0) && v.NodeCredentialPresent &&
			!v.UserDisconnected && v.DesiredState == ipc.DesiredConnected &&
			v.State != ipc.StateDegraded
	})
	for _, transport := range []string{"udp", "tcp"} {
		assertDNSWire(t, transport, "127.0.0.1:53", "live-peer.scenario.endlessnet.", dnsmessage.RCodeSuccess, "198.18.96.20")
		assertDNSWireType(t, transport, "127.0.0.1:53", "live-peer.scenario.endlessnet.", dnsmessage.TypeAAAA, dnsmessage.RCodeSuccess, "fd96::20")
		assertDNSWire(t, transport, "127.0.0.1:53", "renamed-peer.scenario.endlessnet.", dnsmessage.RCodeNameError, "")
	}
}

func assertDNSListenerUnavailable(t *testing.T, transport, address string) {
	t.Helper()
	conn, err := net.DialTimeout(transport, address, time.Second)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if transport == "tcp" {
		t.Fatal("agent DNS TCP listener remained available after disconnect")
	}
	qname, err := dnsmessage.NewName("live-peer.scenario.endlessnet.")
	if err != nil {
		t.Fatal(err)
	}
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 0x6143, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: qname, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(wire); err != nil {
		return
	}
	var reply [512]byte
	if _, err := conn.Read(reply[:]); err == nil {
		t.Fatal("agent DNS UDP listener remained available after disconnect")
	}
}

func assertSystemDNSAddress(t *testing.T, binary, name, expected string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output, err := packetProbeCommand(ctx, "", binary, "--mode", "resolve", "--address", name).CombinedOutput()
	if err != nil {
		t.Fatal("system resolver did not resolve the published Client DNS name")
	}
	if strings.TrimSpace(string(output)) != expected {
		t.Fatal("system resolver returned an address outside the published Client map")
	}
}

func nativeDNSMapApplied(status ipc.StatusResponse, nodeID string, afterRevision uint64) bool {
	return (nodeID == "" || status.NodeID == nodeID) && status.NodeID != "" &&
		status.CachedMapValid && status.MapRevision > afterRevision &&
		status.Agent != nil && status.Agent.StatePresent &&
		status.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
		status.Agent.MapRevision == status.MapRevision &&
		status.WireGuard != nil && status.WireGuard.OK && status.State != ipc.StateDegraded
}
