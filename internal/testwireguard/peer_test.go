package testwireguard

import (
	"bytes"
	"encoding/binary"
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
