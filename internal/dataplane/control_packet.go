package dataplane

import (
	"encoding/binary"
	"net/netip"

	"github.com/endless-net/client/api/transport"
)

// Control uses the published strict Client DTO/codec inside authenticated IPv4
// UDP data. Port zero is reserved from application grants (which start at one).
// The virtual channel consumes these packets before the native TUN/OS boundary.
func controlPacket(source, destination netip.Addr, value transport.Control) ([]byte, error) {
	if !source.Is4() || !destination.Is4() || source == destination {
		return nil, ErrConfiguration
	}
	payload, err := transport.MarshalControl(value)
	if err != nil {
		return nil, err
	}
	packet := make([]byte, 28+len(payload))
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[6:8], 0x4000) // no IP fragmentation for internal control
	a, b := source.As4(), destination.As4()
	copy(packet[12:16], a[:])
	copy(packet[16:20], b[:])
	binary.BigEndian.PutUint16(packet[24:26], uint16(len(packet)-20))
	copy(packet[28:], payload)
	binary.BigEndian.PutUint16(packet[10:12], packetChecksum(packet[:20], 0))
	sum := packetChecksum(packet[20:], udpPseudoSum(packet))
	if sum == 0 {
		sum = 0xffff
	}
	binary.BigEndian.PutUint16(packet[26:28], sum)
	return packet, nil
}

func packetChecksum(raw []byte, sum uint32) uint16 {
	for len(raw) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(raw[:2]))
		raw = raw[2:]
	}
	if len(raw) > 0 {
		sum += uint32(raw[0]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
func udpPseudoSum(raw []byte) uint32 {
	sum := uint32(17) + uint32(len(raw)-20)
	for i := 12; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(raw[i : i+2]))
	}
	return sum
}

func applicationForPair(raw []byte, source, destination netip.Addr) error {
	if !source.Is4() || !destination.Is4() || len(raw) < 20 || raw[0]>>4 != 4 || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) || [4]byte(raw[12:16]) != source.As4() || [4]byte(raw[16:20]) != destination.As4() {
		return ErrPacketIO
	}
	if _, claimed, _ := decodeControlPacket(raw, source, destination); claimed {
		return ErrNoAuthority
	}
	return nil
}

// claimed includes malformed/reserved control packets, which must be consumed
// even when decoding fails. Fragmented UDP is unsupported by the MVP packet
// policy and is never forwarded into the OS by this virtual channel.
func decodeControlPacket(raw []byte, source, destination netip.Addr) (value transport.Control, claimed bool, err error) {
	if len(raw) < 20 || raw[0]>>4 != 4 || raw[9] != 17 {
		return value, false, nil
	}
	header := int(raw[0]&15) * 4
	if binary.BigEndian.Uint16(raw[6:8])&^uint16(0x4000) != 0 || header < 20 || header > 60 || len(raw) < header+8 {
		return value, true, ErrPacketIO
	}
	if binary.BigEndian.Uint16(raw[header:header+2]) != 0 && binary.BigEndian.Uint16(raw[header+2:header+4]) != 0 {
		return value, false, nil
	}
	claimed = true
	if header != 20 || len(raw) > 28+transport.MaxDatagramBytes || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) || binary.BigEndian.Uint16(raw[20:22]) != 0 || binary.BigEndian.Uint16(raw[22:24]) != 0 || int(binary.BigEndian.Uint16(raw[24:26])) != len(raw)-20 || binary.BigEndian.Uint16(raw[26:28]) == 0 || packetChecksum(raw[:20], 0) != 0 || packetChecksum(raw[20:], udpPseudoSum(raw)) != 0 {
		return value, claimed, ErrPacketIO
	}
	if !source.Is4() || !destination.Is4() {
		return value, claimed, ErrPacketIO
	}
	a, b := source.As4(), destination.As4()
	if [4]byte(raw[12:16]) != a || [4]byte(raw[16:20]) != b {
		return value, claimed, ErrNoAuthority
	}
	value, err = transport.DecodeControl(raw[28:])
	return value, claimed, err
}
