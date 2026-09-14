package client

import (
	"net/netip"
	"testing"
	"time"
)

func TestResourceFilterDeniesOverlapsAndTransitionsInBothDirections(t *testing.T) {
	now := time.Now()
	f := &resourcePacketFilter{}
	service := resourceDenyRule{prefix: netip.MustParsePrefix("100.65.0.1/32"), protocol: 6, port: 443}
	if err := f.suspend([]resourceDenyRule{service}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, inbound := range []bool{false, true} {
		if f.allows(shareTCP(inbound, 16), inbound, now) {
			t.Fatal("service port restriction bypassed before commit", inbound)
		}
		if !f.allows(inboundTestUDP(inbound, 443), inbound, now) {
			t.Fatal("TCP restriction blocked UDP", inbound)
		}
	}
	f.commit()
	if err := f.suspend([]resourceDenyRule{{prefix: netip.MustParsePrefix("100.65.0.0/24")}}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if f.allows(inboundTestUDP(false, 53), false, now) {
		t.Fatal("broad denial did not tighten during apply")
	}
	f.commit()
	if err := f.suspend(nil, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if f.allows(inboundTestUDP(true, 53), true, now) {
		t.Fatal("pending enable reopened denied overlap")
	}
	f.commit()
	if !f.allows(inboundTestUDP(true, 53), true, now) {
		t.Fatal("commit did not remove denial")
	}
	if f.allows(inboundTestUDP(true, 53), true, now.Add(3*time.Minute)) {
		t.Fatal("expired resource map authorized traffic")
	}
	f.withdraw()
	if f.allows(inboundTestUDP(false, 53), false, now) {
		t.Fatal("withdrawal left traffic enabled")
	}
}

func TestResourceFilterRuleValidationAndOwnership(t *testing.T) {
	f := &resourcePacketFilter{}
	now := time.Now()
	for _, invalid := range []resourceDenyRule{{}, {prefix: netip.MustParsePrefix("0.0.0.0/0")}, {prefix: netip.MustParsePrefix("100.65.0.1/32"), protocol: 6}, {prefix: netip.MustParsePrefix("100.65.0.1/32"), port: 443}} {
		if err := f.suspend([]resourceDenyRule{invalid}, now.Add(time.Minute)); err == nil {
			t.Fatal("invalid rule accepted")
		}
	}
	rules := []resourceDenyRule{{prefix: netip.MustParsePrefix("100.65.0.1/32")}}
	if err := f.suspend(rules, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	rules[0].prefix = netip.MustParsePrefix("192.0.2.1/32")
	f.commit()
	if f.allows(inboundTestUDP(false, 53), false, now) || f.allows([]byte{0x45}, false, now) {
		t.Fatal("rule alias or malformed packet bypassed denial")
	}
}
