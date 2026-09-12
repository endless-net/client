package client

import "encoding/binary"

// normalizeIPv6UDPChecksum handles computed zero after native TUN checksum
// completion. RFC 8200 section 8.1 requires FFFF on the wire in this case.
// Only a complete, fixed-header datagram whose checksum really computes to zero
// is changed; missing/invalid checksums and other packet shapes are untouched.
func normalizeIPv6UDPChecksum(packet []byte) {
	if len(packet) < 48 || packet[0]>>4 != 6 || packet[6] != 17 ||
		binary.BigEndian.Uint16(packet[4:6]) != uint16(len(packet)-40) || len(packet)-40 > 65535 ||
		binary.BigEndian.Uint16(packet[44:46]) != uint16(len(packet)-40) ||
		binary.BigEndian.Uint16(packet[46:48]) != 0 {
		return
	}
	sum := uint32(len(packet)-40) + 17
	for i := 8; i < 40; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	for i := 40; i < len(packet); i += 2 {
		if i+1 == len(packet) {
			sum += uint32(packet[i]) << 8
		} else {
			sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
		}
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	if sum == 0xffff {
		packet[46], packet[47] = 0xff, 0xff
	}
}
