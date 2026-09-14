package client

import (
	"net/netip"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitPacketFilterExpiryTransitionAndWithdrawal(t *testing.T) {
	cfg, networkMap, key := signedApplicationFixture(t, false)
	now := time.Now()
	networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0", "::/0")
	host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
	deadline := now.Add(10 * time.Second)
	networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: deadline, AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyDualStack}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
	resignApplicationMap(t, &networkMap, key)
	selection := &ClientExitSelection{ID: "exit", NetworkID: cfg.NetworkID, NodeID: cfg.NodeID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
	f := &exitPacketFilter{}
	ipv4 := aclTestPacket("8.8.8.8", 6, 443)
	ipv6 := aclTestPacket("2001:4860:4860::8888", 17, 53)
	reply := applicationTCPPacket("8.8.8.8", networkMap.Node.AssignedIP, 443, 24001)
	ordinary := aclTestPacket(netip.MustParsePrefix(networkMap.Peers[0].AllowedIPs[0]).Addr().String(), 6, 443)
	if err := f.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	if f.allows(ipv4, false, now) {
		t.Fatal("permission became effective before commit")
	}
	f.commit()
	if !f.allows(ipv4, false, now) || !f.allows(reply, true, now) || f.allows(ipv6, false, now) {
		t.Fatal("family or reply policy mismatch")
	}
	if f.allows(ipv4, false, deadline) || f.allows(reply, true, deadline) {
		t.Fatal("expired grant admitted existing flow")
	}
	if !f.allows(ordinary, false, deadline) {
		t.Fatal("exit grant expiry revoked an independent ordinary route")
	}
	if f.allows([]byte{1, 2, 3}, false, now) {
		t.Fatal("malformed packet allowed")
	}
	selection.Family = api.ExitFamilyDualStack
	if err := f.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	if f.allows(ipv6, false, now) {
		t.Fatal("pending expansion allowed IPv6")
	}
	f.commit()
	if !f.allows(ipv6, false, now) {
		t.Fatal("committed dual stack denied IPv6")
	}
	selection.Family = api.ExitFamilyIPv4Only
	if err := f.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	if f.allows(ipv6, false, now) || !f.allows(ipv4, false, now) {
		t.Fatal("pending restriction did not intersect old and new rules")
	}
	f.commit()
	if err := f.suspend(cfg, networkMap, nil, now); err != nil {
		t.Fatal(err)
	}
	if f.allows(ipv4, false, now) {
		t.Fatal("pending clear retained exit permission")
	}
	f.commit()
	if f.allows(ipv4, false, now) {
		t.Fatal("cleared selection retained exit permission")
	}
	if err := f.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	f.commit()
	networkMap.Network.ClientPolicy.ExitNodes[0].Name = "tampered"
	if err := f.suspend(cfg, networkMap, selection, now); err == nil {
		t.Fatal("tampered policy accepted")
	}
	f.commit()
	if f.allows(ipv4, false, now) {
		t.Fatal("invalid apply restored old exit permission")
	}
	resignApplicationMap(t, &networkMap, key)
	networkMap.Network.ClientPolicy.ExitNodes[0].ExpiresAt = networkMap.MapSignature.ExpiresAt.Add(time.Hour)
	resignApplicationMap(t, &networkMap, key)
	if err := f.suspend(cfg, networkMap, selection, now); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if f.allows(ipv4, false, networkMap.MapSignature.ExpiresAt) {
		t.Fatal("expired map allowed exit")
	}
	f.withdraw()
	f.commit()
	if f.allows(ipv4, false, now) {
		t.Fatal("withdrawn policy resurrected by commit")
	}
}
