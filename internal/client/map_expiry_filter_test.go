package client

import (
	"net/netip"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestExpiredMapDeniesOrdinaryPeerPackets(t *testing.T) {
	for _, destination := range []string{"100.94.0.20", "fd00::20"} {
		t.Run(destination, func(t *testing.T) {
			for _, protocol := range []byte{6, 17} {
				packet := aclTestPacket(destination, protocol, 24001)
				parsed, ok := parseApplicationPacket(packet)
				if !ok {
					t.Fatal("invalid packet fixture")
				}
				deadline := time.Now().UTC()
				m := clientapi.RegisterNodeResponse{MapSignature: &clientapi.MapSignature{ExpiresAt: deadline}}
				filter := newApplicationPacketFilter()
				filter.update(m)
				// Both directions exercise ordinary local traffic, without any
				// application-protected destination that could mask the gap.
				filter.local = []netip.Addr{parsed.source, parsed.destination}
				for _, inbound := range []bool{false, true} {
					if !filter.allows(packet, inbound, deadline.Add(-time.Nanosecond)) {
						t.Fatal("unexpired map denied ordinary peer traffic")
					}
					if filter.allows(packet, inbound, deadline) || filter.allows(packet, inbound, deadline.Add(time.Second)) {
						t.Fatal("expired map allowed ordinary peer traffic")
					}
				}
				m.MapSignature.ExpiresAt = deadline.Add(time.Minute)
				filter.update(m)
				filter.local = []netip.Addr{parsed.source, parsed.destination}
				if !filter.allows(packet, false, deadline.Add(time.Second)) || !filter.allows(packet, true, deadline.Add(time.Second)) {
					t.Fatal("fresh authority did not restore ordinary peer traffic")
				}
			}
		})
	}
}
