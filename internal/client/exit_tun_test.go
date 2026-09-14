package client

import (
	"io"
	"net/netip"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitBatchTUN struct {
	applicationBatchTUN
	read bool
}

func (d *exitBatchTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if d.read {
		return 0, io.EOF
	}
	d.read = true
	return d.applicationBatchTUN.Read(bufs, sizes, offset)
}

func TestExitTUNEnforcesExpiryInBothDirectionsAndRetainsACL(t *testing.T) {
	cfg, networkMap, key := signedApplicationFixture(t, false)
	now := time.Now()
	networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
	host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
	networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
	resignApplicationMap(t, &networkMap, key)
	selection := &ClientExitSelection{ID: "exit", NetworkID: cfg.NetworkID, NodeID: cfg.NodeID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
	exit := &exitPacketFilter{}
	if err := exit.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	exit.commit()
	applications := newApplicationPacketFilter()
	applications.update(networkMap)
	device := &exitBatchTUN{}
	wrapper := &applicationTUN{Device: device, filter: applications, exit: exit}
	local := networkMap.Node.AssignedIP
	ordinary := netip.MustParsePrefix(networkMap.Peers[0].AllowedIPs[0]).Addr().String()
	request := applicationTCPPacket(local, "8.8.8.8", 40000, 443)
	reply := applicationTCPPacket("8.8.8.8", local, 443, 40000)
	ordinaryRequest := applicationTCPPacket(local, ordinary, 40000, 443)
	ordinaryReply := applicationTCPPacket(ordinary, local, 443, 40000)
	device.packets = [][]byte{request}
	bufs, sizes := [][]byte{make([]byte, 128), make([]byte, 128)}, make([]int, 2)
	if n, err := wrapper.Read(bufs, sizes, 16); err != nil || n != 1 {
		t.Fatal("live exit request denied", n, err)
	}
	if _, err := wrapper.Write([][]byte{reply}, 0); err != nil || len(device.written) != 1 {
		t.Fatal("live exit reply denied", err)
	}
	exit.mu.Lock()
	exit.current.grantExpires = now.Add(-time.Second)
	exit.mu.Unlock()
	device.written = nil
	prefixed := func(packet []byte) []byte { return append(make([]byte, 16), packet...) }
	if n, err := wrapper.Write([][]byte{prefixed(reply), prefixed(ordinaryReply)}, 16); err != nil || n != 2 || len(device.written) != 1 || !slices.Equal(device.written[0], ordinaryReply) {
		t.Fatal("expired exit reply bypassed TUN or damaged batch", n, err)
	}
	device.read = false
	device.packets = [][]byte{request, ordinaryRequest}
	if n, err := wrapper.Read(bufs, sizes, 16); err != nil || n != 1 || !slices.Equal(bufs[0][16:16+sizes[0]], ordinaryRequest) {
		t.Fatal("expired request bypassed TUN or damaged batch", n, err)
	}
	// Exit authorization is an additional gate, never an override of peer ACLs.
	wrapper.peerACL = &peerACLFilter{current: []peerACLRule{{destination: netip.PrefixFrom(netip.MustParseAddr(ordinary), 32), deny: true}}}
	device.read = false
	device.packets = [][]byte{ordinaryRequest}
	if n, err := wrapper.Read(bufs, sizes, 16); err != io.EOF || n != 0 {
		t.Fatal("exit gate bypassed independent peer ACL", n, err)
	}
}
