package client

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestInboundFilterTCPHandshakeExpiryAndPolicyChange(t *testing.T) {
	f := &inboundPacketFilter{}
	f.setAllowed(false)
	now := time.Now()
	if f.allows(shareTCP(true, 2), true, now) || f.allows(shareTCP(true, 18), true, now) || f.allows(shareTCP(true, 16), true, now) {
		t.Fatal("unsolicited inbound TCP accepted")
	}
	if !f.allows(shareTCP(false, 2), false, now) || f.allows(shareTCP(true, 16), true, now) || !f.allows(shareTCP(true, 18), true, now) || !f.allows(shareTCP(false, 16), false, now) || !f.allows(shareTCP(true, 16), true, now) {
		t.Fatal("outbound TCP handshake tracking failed")
	}
	if f.allows(shareTCP(true, 16), true, now.Add(2*time.Minute)) {
		t.Fatal("expired TCP response accepted")
	}
	f.setAllowed(true)
	if !f.allows(shareTCP(true, 2), true, now) {
		t.Fatal("enabled inbound remained blocked")
	}
	f.setAllowed(false)
	if f.allows(shareTCP(true, 16), true, now) {
		t.Fatal("policy change retained established flow")
	}
}

func inboundTestUDP(reverse bool, port uint16) []byte {
	p := shareTCP(reverse, 0)[:28]
	binary.BigEndian.PutUint16(p[2:4], 28)
	p[9] = 17
	if reverse {
		binary.BigEndian.PutUint16(p[20:22], port)
	} else {
		binary.BigEndian.PutUint16(p[22:24], port)
	}
	binary.BigEndian.PutUint16(p[24:26], 8)
	return p
}

func TestInboundFilterUDPBindingBoundsAndReplyLifetime(t *testing.T) {
	f := &inboundPacketFilter{}
	f.setAllowed(false)
	now := time.Now()
	if f.allows(inboundTestUDP(true, 53), true, now) {
		t.Fatal("unsolicited UDP accepted")
	}
	if !f.allows(inboundTestUDP(false, 53), false, now) || !f.allows(inboundTestUDP(true, 53), true, now.Add(29*time.Second)) {
		t.Fatal("outbound UDP response blocked")
	}
	if f.allows(inboundTestUDP(true, 54), true, now) || f.allows(inboundTestUDP(true, 53), true, now.Add(30*time.Second)) {
		t.Fatal("port binding or fixed reply expiry bypassed")
	}
	for i := 1; i <= inboundFlowLimit; i++ {
		if !f.allows(inboundTestUDP(false, uint16(i)), false, now) {
			t.Fatal("flow capacity rejected early", i)
		}
	}
	if f.allows(inboundTestUDP(false, inboundFlowLimit+1), false, now) {
		t.Fatal("flow state unbounded")
	}
	if !f.allows(inboundTestUDP(false, inboundFlowLimit+1), false, now.Add(time.Minute)) {
		t.Fatal("expired flow capacity not recovered")
	}
	malformed := inboundTestUDP(false, 53)
	binary.BigEndian.PutUint16(malformed[24:26], 100)
	if f.allows(malformed, false, now) || f.allows([]byte{0x45}, true, now) {
		t.Fatal("malformed packet authorized")
	}
}
