package testwireguard

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestResourceForwardingValidatesAndDecrementsIPHop(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		source, destination := netip.MustParseAddr("198.18.94.1"), netip.MustParseAddr("198.18.96.20")
		packet := make([]byte, 24)
		hop := 8
		packet[0], packet[8], packet[9] = 0x45, 64, 17
		binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
		copy(packet[12:16], source.AsSlice())
		copy(packet[16:20], destination.AsSlice())
		binary.BigEndian.PutUint16(packet[10:12], resourceHeaderChecksum(packet[:20]))
		if ipv6 {
			source, destination = netip.MustParseAddr("fd94::1"), netip.MustParseAddr("fd96::20")
			packet = make([]byte, 44)
			packet[0], packet[6], packet[7] = 0x60, 17, 64
			binary.BigEndian.PutUint16(packet[4:6], 4)
			copy(packet[8:24], source.AsSlice())
			copy(packet[24:40], destination.AsSlice())
			hop = 7
		}
		original := append([]byte(nil), packet...)
		out := forwardResourcePacket(packet, source, destination)
		if len(out) != len(packet) || out[hop] != 63 || !bytes.Equal(packet, original) {
			t.Fatal("forwarding did not preserve input and decrement hop limit")
		}
		if !ipv6 && resourceHeaderChecksum(out[:20]) != 0 {
			t.Fatal("forwarding damaged IPv4 checksum")
		}
		if forwardResourcePacket(packet, source, destination.Next()) != nil {
			t.Fatal("forwarded unconfigured destination")
		}
		if forwardResourcePacket(packet, source.Next(), destination) != nil {
			t.Fatal("forwarded unconfigured source")
		}
		packet[hop] = 1
		if !ipv6 {
			packet[10], packet[11] = 0, 0
			binary.BigEndian.PutUint16(packet[10:12], resourceHeaderChecksum(packet[:20]))
		}
		if forwardResourcePacket(packet, source, destination) != nil {
			t.Fatal("forwarded expired IP hop")
		}
		if !ipv6 {
			packet = original
			packet[10] ^= 1
			if forwardResourcePacket(packet, source, destination) != nil {
				t.Fatal("repaired a corrupted incoming IP packet")
			}
		}
	}
}
