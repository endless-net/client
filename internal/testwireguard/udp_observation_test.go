package testwireguard

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestUDPRejectionTruncatedHeaders(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		offset, source, destination := 20, netip.MustParseAddr("100.90.0.1"), netip.MustParseAddr("100.90.0.2")
		packet := make([]byte, 80)
		packet[0], packet[9] = 0x45, 17
		copy(packet[12:16], source.AsSlice())
		copy(packet[16:20], destination.AsSlice())
		if ipv6 {
			offset = 40
			source, destination = netip.MustParseAddr("fd00::1"), netip.MustParseAddr("fd00::2")
			packet[0], packet[6] = 0x60, 17
			copy(packet[8:24], source.AsSlice())
			copy(packet[24:40], destination.AsSlice())
		}
		binary.BigEndian.PutUint16(packet[offset:offset+2], 31000)
		binary.BigEndian.PutUint16(packet[offset+2:offset+4], 24001)
		binary.BigEndian.PutUint16(packet[offset+4:offset+6], 40)
		for size := 0; size <= len(packet); size++ {
			r := udpRejection(packet[:size], source, destination, 7)
			if r.Bytes != size || r.ReceivedIndex != 7 || r.UDPHeaderPresent != (size >= offset+8) {
				t.Fatalf("incorrect truncated packet observation: ipv6=%t size=%d", ipv6, size)
			}
			if r.UDPHeaderPresent && (!r.SourceMatches || !r.DestinationMatches || r.SourcePort != 31000 || r.DestinationPort != 24001 || r.UDPLength != 40 || !r.ChecksumZero) {
				t.Fatal("complete UDP header observation lost packet properties")
			}
		}
	}
}
