package client

import (
	"encoding/binary"
	"net/netip"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func aclTestPacket(destination string, protocol byte, port uint16) []byte {
	ip := netip.MustParseAddr(destination)
	header := 20
	if ip.Is6() {
		header = 40
	}
	packet := make([]byte, header+20)
	if ip.Is4() {
		packet[0], packet[9] = 0x45, protocol
		binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
		copy(packet[12:16], netip.MustParseAddr("100.94.0.2").AsSlice())
		copy(packet[16:20], ip.AsSlice())
	} else {
		packet[0], packet[6] = 0x60, protocol
		binary.BigEndian.PutUint16(packet[4:6], 20)
		copy(packet[8:24], netip.MustParseAddr("fd00::2").AsSlice())
		copy(packet[24:40], ip.AsSlice())
	}
	binary.BigEndian.PutUint16(packet[header:header+2], 40000)
	binary.BigEndian.PutUint16(packet[header+2:header+4], port)
	return packet
}

func TestPeerACLWithdrawalRestorationAndFailedApply(t *testing.T) {
	f := &peerACLFilter{}
	first := aclTestPacket("100.94.0.20", 17, 24001)
	second := aclTestPacket("100.94.0.20", 17, 24002)
	if !f.allows(first) || !f.allows(second) {
		t.Fatal("unrestricted initial traffic denied")
	}
	limited := []clientapi.Peer{{AllowedIPs: []string{"100.94.0.20/32"}, ACLRestricted: true, ACLGrants: []clientapi.ACLGrant{{DestinationCIDRs: []string{"100.94.0.20/32"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "udp", Port: 24002}}}}}}
	f.suspend(limited)
	if f.allows(first) || !f.allows(second) {
		t.Fatal("pending withdrawal did not enforce the permission intersection")
	}
	f.commit()
	for range 3 {
		if f.allows(first) || !f.allows(second) || f.allows(aclTestPacket("100.94.0.20", 6, 24002)) {
			t.Fatal("current policy failed to recheck the established flow and protocol")
		}
	}
	f.suspend(nil)
	if f.allows(first) {
		t.Fatal("permission became effective before runtime apply")
	}
	f.withdraw()
	if f.allows(first) || f.allows(second) {
		t.Fatal("failed runtime apply left traffic open")
	}
	f.suspend(nil)
	f.commit()
	if !f.allows(first) || !f.allows(second) {
		t.Fatal("restoring permission did not recover the same flows")
	}
}

func TestPeerACLPrefixAndPortCorrelation(t *testing.T) {
	for _, family := range []struct{ parent, child, other, target string }{
		{"100.94.0.0/24", "100.94.0.20/32", "100.94.0.21", "100.94.0.20"},
		{"fd00::/64", "fd00::20/128", "fd00::21", "fd00::20"},
	} {
		t.Run(family.target, func(t *testing.T) {
			f := &peerACLFilter{}
			f.suspend([]clientapi.Peer{
				{AllowedIPs: []string{family.parent}},
				{AllowedIPs: []string{family.child}, ACLRestricted: true, ACLGrants: []clientapi.ACLGrant{
					{DestinationCIDRs: []string{family.child}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 443}}},
					{DestinationCIDRs: []string{family.other}, AllowedPorts: []clientapi.ACLPort{{Protocol: "udp", Port: 53}}},
				}},
			})
			f.commit()
			if !f.allows(aclTestPacket(family.other, 17, 9999)) || !f.allows(aclTestPacket(family.target, 6, 443)) {
				t.Fatal("unrestricted parent or valid child grant denied")
			}
			if f.allows(aclTestPacket(family.target, 17, 443)) || f.allows(aclTestPacket(family.target, 17, 53)) || f.allows(aclTestPacket(family.target, 6, 80)) {
				t.Fatal("parent or uncorrelated grant bypassed restricted child")
			}
			if f.allows([]byte{0x45}) {
				t.Fatal("malformed packet bypassed restrictions")
			}
		})
	}
}
