package client

import (
	"encoding/binary"
	"io"
	"slices"
	"testing"
	"time"
)

func inboundEcho(ipv6, reply bool, sequence uint16) []byte {
	offset := 20
	if ipv6 {
		offset = 40
	}
	p := make([]byte, offset+8)
	if ipv6 {
		p[0], p[6], p[23], p[39] = 0x60, 58, 1, 2
		binary.BigEndian.PutUint16(p[4:6], 8)
		p[offset] = 128
		if reply {
			p[23], p[39], p[offset] = 2, 1, 129
		}
	} else {
		p[0], p[9], p[15], p[19] = 0x45, 1, 1, 2
		binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
		p[offset] = 8
		if reply {
			p[15], p[19], p[offset] = 2, 1, 0
		}
	}
	binary.BigEndian.PutUint16(p[offset+4:offset+6], 123)
	binary.BigEndian.PutUint16(p[offset+6:offset+8], sequence)
	return p
}

func TestInboundFilterEchoFamiliesAndExtensionRejection(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		f := &inboundPacketFilter{}
		f.setAllowed(false)
		now := time.Now()
		if f.allows(inboundEcho(ipv6, false, 1), true, now) || f.allows(inboundEcho(ipv6, true, 1), true, now) {
			t.Fatal("unsolicited echo accepted", ipv6)
		}
		if !f.allows(inboundEcho(ipv6, false, 1), false, now) || !f.allows(inboundEcho(ipv6, true, 1), true, now) {
			t.Fatal("echo response rejected", ipv6)
		}
		if f.allows(inboundEcho(ipv6, true, 2), true, now) || f.allows(inboundEcho(ipv6, true, 1), true, now.Add(30*time.Second)) {
			t.Fatal("echo binding or expiry bypass", ipv6)
		}
		malformed := inboundEcho(ipv6, true, 1)
		if ipv6 {
			malformed[6] = 44
		} else {
			malformed[6] = 0x20
		}
		if f.allows(malformed, true, now) {
			t.Fatal("fragment bypass", ipv6)
		}
	}
}

type inboundOnceTUN struct {
	applicationBatchTUN
	read bool
}

func (d *inboundOnceTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if d.read {
		return 0, io.EOF
	}
	d.read = true
	return d.applicationBatchTUN.Read(bufs, sizes, offset)
}

func TestInboundTUNFilteringPreservesBatchesAndCannotSeedDeniedFlows(t *testing.T) {
	f := &inboundPacketFilter{}
	f.setAllowed(false)
	device := &inboundOnceTUN{applicationBatchTUN: applicationBatchTUN{packets: [][]byte{inboundTestUDP(false, 53)}}}
	acl := &peerACLFilter{}
	acl.withdraw()
	tun := &applicationTUN{Device: device, filter: newApplicationPacketFilter(), peerACL: acl, inbound: f}
	tun.filter.update(sharingFilterFixture(time.Now(), false))
	bufs, sizes := [][]byte{make([]byte, 128)}, make([]int, 1)
	if _, err := tun.Read(bufs, sizes, 16); err != io.EOF {
		t.Fatal("closed peer ACL did not deny outbound", err)
	}
	if f.allows(inboundTestUDP(true, 53), true, time.Now()) {
		t.Fatal("ACL-denied outgoing packet seeded inbound permission")
	}
	tun.peerACL, device.read = nil, false
	if n, err := tun.Read(bufs, sizes, 16); err != nil || n != 1 || !slices.Equal(bufs[0][16:16+sizes[0]], inboundTestUDP(false, 53)) {
		t.Fatal("outbound batch offset corrupted", n, err)
	}
	wrap := func(p []byte) []byte { return append(make([]byte, 16), p...) }
	if n, err := tun.Write([][]byte{wrap(inboundTestUDP(true, 54)), wrap(inboundTestUDP(true, 53))}, 16); err != nil || n != 2 || len(device.written) != 1 || !slices.Equal(device.written[0], inboundTestUDP(true, 53)) {
		t.Fatal("inbound batch bypass or corruption", n, err)
	}
	f.setAllowed(true)
	tun.peerACL = acl
	device.read = false
	if _, err := tun.Read(bufs, sizes, 16); err != io.EOF {
		t.Fatal("allow inbound bypassed peer ACL")
	}
}
