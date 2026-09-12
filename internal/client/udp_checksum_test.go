package client

import (
	"encoding/binary"
	"io"
	"net/netip"
	"slices"
	"testing"

	"github.com/tailscale/wireguard-go/tun"
)

func TestApplicationTUNCanonicalizesComputedZeroIPv6UDPChecksum(t *testing.T) {
	packet := make([]byte, 80)
	packet[0], packet[6], packet[7] = 0x60, 17, 64
	packet[8], packet[23], packet[24], packet[39] = 0xfd, 1, 0xfd, 2
	binary.BigEndian.PutUint16(packet[4:6], 40)
	binary.BigEndian.PutUint16(packet[40:42], 50976)
	binary.BigEndian.PutUint16(packet[42:44], 24001)
	binary.BigEndian.PutUint16(packet[44:46], 40)
	initial := tun.PseudoHeaderChecksum(17, packet[8:24], packet[24:40], 40)
	// Choose a payload whose computed checksum is exactly zero. This is a
	// deterministic arithmetic edge, not a random traffic/stress test.
	binary.BigEndian.PutUint16(packet[78:80], ^tun.Checksum(packet[40:], initial))
	binary.BigEndian.PutUint16(packet[46:48], initial)
	output, sizes := [][]byte{make([]byte, 80)}, make([]int, 1)
	n, err := tun.GSOSplit(packet, tun.GSOOptions{GSOType: tun.GSONone, CsumStart: 40, CsumOffset: 6, NeedsCsum: true}, output, sizes, 0)
	if err != nil || n != 1 || sizes[0] != 80 {
		t.Fatal("pinned TUN checksum completion failed")
	}
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"invalid-checksum", func(p []byte) { p[50] ^= 1 }},
		{"nonzero-checksum", func(p []byte) { p[46], p[47] = 0x12, 0x34 }},
		{"udp-length-mismatch", func(p []byte) { p[45]-- }},
		{"ip-length-mismatch", func(p []byte) { p[5]-- }},
		{"extension-header", func(p []byte) { p[6] = 44 }},
		{"ipv4", func(p []byte) { p[0] = 0x45 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := slices.Clone(output[0])
			tc.mutate(p)
			before := slices.Clone(p)
			normalizeIPv6UDPChecksum(p)
			if !slices.Equal(p, before) {
				t.Fatal("checksum normalization altered an unrelated or invalid packet")
			}
		})
	}
	for size := 0; size < 48; size++ {
		p := slices.Clone(output[0][:size])
		before := slices.Clone(p)
		normalizeIPv6UDPChecksum(p)
		if !slices.Equal(p, before) {
			t.Fatal("checksum normalization altered a truncated packet")
		}
	}
	want := slices.Clone(output[0])
	want[46], want[47] = 0xff, 0xff
	device := &checksumPacketTUN{packet: output[0]}
	filter := newApplicationPacketFilter()
	filter.local = []netip.Addr{netip.AddrFrom16([16]byte(packet[8:24]))}
	wrapper := &applicationTUN{Device: device, filter: filter}
	bufs := [][]byte{make([]byte, 112)}
	n, err = wrapper.Read(bufs, sizes, 16)
	if err != nil || n != 1 || sizes[0] != 80 || !slices.Equal(bufs[0][16:96], want) {
		t.Fatal("Client emitted a noncanonical computed-zero IPv6 UDP checksum")
	}
}

type checksumPacketTUN struct {
	tun.Device
	packet []byte
}

func (d *checksumPacketTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if d.packet == nil {
		return 0, io.EOF
	}
	sizes[0] = copy(bufs[0][offset:], d.packet)
	d.packet = nil
	return 1, nil
}
