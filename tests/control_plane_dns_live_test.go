package tests

import (
	"testing"

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
		return v.NodeID != "" && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK
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
			return v.NodeID == id && v.CachedMapValid && v.MapRevision > previous &&
				v.WireGuard != nil && v.WireGuard.OK && v.State != ipc.StateDegraded
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
}
