package tests

import (
	"net"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
	"golang.org/x/net/dns/dnsmessage"
)

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
	conn, err := net.DialTimeout("tcp", "127.0.0.1:53", time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("agent DNS TCP listener remained available after disconnect")
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

func nativeDNSMapApplied(status ipc.StatusResponse, nodeID string, afterRevision uint64) bool {
	return (nodeID == "" || status.NodeID == nodeID) && status.NodeID != "" &&
		status.CachedMapValid && status.MapRevision > afterRevision &&
		status.Agent != nil && status.Agent.StatePresent &&
		status.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
		status.Agent.MapRevision == status.MapRevision &&
		status.WireGuard != nil && status.WireGuard.OK && status.State != ipc.StateDegraded
}
