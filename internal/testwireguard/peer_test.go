package testwireguard

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestEchoUDPPreservesValidChecksumsAndRejectsUnrelatedPackets(t *testing.T) {
	client, peer := [4]byte{100, 94, 0, 2}, [4]byte{100, 94, 0, 20}
	packet := make([]byte, 60)
	packet[0], packet[8], packet[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(packet[2:4], 60)
	copy(packet[12:16], client[:])
	copy(packet[16:20], peer[:])
	binary.BigEndian.PutUint16(packet[20:22], 40000)
	binary.BigEndian.PutUint16(packet[22:24], 24001)
	binary.BigEndian.PutUint16(packet[24:26], 40)
	copy(packet[28:], bytes.Repeat([]byte{0xa5}, 32))
	checksum := func(data []byte) uint16 {
		var sum uint32
		for i := 0; i < len(data); i += 2 {
			sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
		}
		for sum>>16 != 0 {
			sum = (sum & 0xffff) + (sum >> 16)
		}
		return ^uint16(sum)
	}
	udpChecksum := func(p []byte) uint16 {
		pseudo := make([]byte, 12)
		copy(pseudo[:8], p[12:20])
		pseudo[9] = 17
		binary.BigEndian.PutUint16(pseudo[10:], 40)
		return checksum(append(pseudo, p[20:]...))
	}
	binary.BigEndian.PutUint16(packet[10:12], checksum(packet[:20]))
	binary.BigEndian.PutUint16(packet[26:28], udpChecksum(packet))
	reply := echoUDP(packet, client, peer)
	if len(reply) != 60 || !bytes.Equal(reply[12:16], peer[:]) || !bytes.Equal(reply[16:20], client[:]) || binary.BigEndian.Uint16(reply[20:22]) != 24001 || binary.BigEndian.Uint16(reply[22:24]) != 40000 || !bytes.Equal(reply[28:], packet[28:]) {
		t.Fatal("reference peer did not echo the nonce to the original sender")
	}
	if checksum(reply[:20]) != 0 || udpChecksum(reply) != 0 {
		t.Fatal("reference echo broke IP/UDP checksums")
	}
	for _, offset := range []int{0, 2, 6, 9, 12, 16, 22, 24} {
		bad := append([]byte(nil), packet...)
		bad[offset] ^= 1
		if echoUDP(bad, client, peer) != nil {
			t.Fatalf("unrelated/malformed packet accepted at offset %d", offset)
		}
	}
	if echoUDP(packet[:59], client, peer) != nil {
		t.Fatal("truncated datagram accepted")
	}
}

func TestEchoIPv6UDPPreservesChecksumAndRejectsUnrelatedPackets(t *testing.T) {
	client, peer := netip.MustParseAddr("fd94::1").As16(), netip.MustParseAddr("fd94::20").As16()
	checksum := func(packet []byte) uint16 {
		pseudo := make([]byte, 40)
		copy(pseudo[:32], packet[8:40])
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(packet)-40))
		pseudo[39] = 17
		data := append(pseudo, packet[40:]...)
		var sum uint32
		for i := 0; i < len(data); i += 2 {
			sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
		}
		for sum>>16 != 0 {
			sum = (sum & 0xffff) + (sum >> 16)
		}
		return ^uint16(sum)
	}
	for _, port := range []uint16{24001, 24002} {
		packet := make([]byte, 80)
		packet[0], packet[6], packet[7] = 0x60, 17, 64
		binary.BigEndian.PutUint16(packet[4:6], 40)
		copy(packet[8:24], client[:])
		copy(packet[24:40], peer[:])
		binary.BigEndian.PutUint16(packet[40:42], 40000)
		binary.BigEndian.PutUint16(packet[42:44], port)
		binary.BigEndian.PutUint16(packet[44:46], 40)
		copy(packet[48:], bytes.Repeat([]byte{0xa5}, 32))
		value := checksum(packet)
		if value == 0 {
			value = 0xffff
		}
		binary.BigEndian.PutUint16(packet[46:48], value)
		reply := echoUDPv6(packet, client, peer)
		if len(reply) != 80 || !bytes.Equal(reply[8:24], peer[:]) || !bytes.Equal(reply[24:40], client[:]) || binary.BigEndian.Uint16(reply[40:42]) != port || binary.BigEndian.Uint16(reply[42:44]) != 40000 || !bytes.Equal(reply[48:], packet[48:]) {
			t.Fatal("IPv6 reference peer did not echo the nonce to its sender")
		}
		if checksum(reply) != 0 {
			t.Fatal("IPv6 reference echo broke the mandatory UDP checksum")
		}
		for _, offset := range []int{4, 6, 8, 24, 42, 44} {
			bad := append([]byte(nil), packet...)
			bad[offset] ^= 1
			if echoUDPv6(bad, client, peer) != nil {
				t.Fatalf("unrelated IPv6 packet accepted at offset %d", offset)
			}
		}
		for _, nextHeader := range []byte{0, 43, 44, 60} {
			bad := append([]byte(nil), packet...)
			bad[6] = nextHeader
			if echoUDPv6(bad, client, peer) != nil {
				t.Fatal("IPv6 extension or fragment header accepted")
			}
		}
		packet[46], packet[47] = 0, 0
		if echoUDPv6(packet, client, peer) != nil || echoUDPv6(packet[:79], client, peer) != nil {
			t.Fatal("zero-checksum or truncated IPv6 datagram accepted")
		}
	}
}
