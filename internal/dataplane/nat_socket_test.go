package dataplane

import (
	"bytes"
	"encoding/hex"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestBindingOnProfileSocket(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	remote := server.LocalAddr().(*net.UDPAddr).AddrPort()
	sock, err := reserveNATSocket(remote, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	sock.discover(remote)
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [64]byte
	n, from, err := server.ReadFromUDPAddrPort(buf[:])
	if err != nil || n != 20 || from != sock.local {
		t.Fatal("binding socket", n, from, err)
	}
	response, _ := hex.DecodeString("0101000c2112a4420102030405060708090a0b0c002000080001f523ea12d545")
	copy(response[8:20], buf[8:20])
	if _, err := server.WriteToUDPAddrPort(response, from); err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddrPort("203.0.113.7:54321")
	until := time.Now().Add(time.Second)
	for sock.mapping() != want && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if sock.mapping() != want {
		t.Fatal("missing mapped candidate", sock.mapping())
	}
	if _, err := sock.WriteToUDPAddrPort([]byte("peer traffic"), remote); err != nil {
		t.Fatal(err)
	}
	n, from, err = server.ReadFromUDPAddrPort(buf[:])
	if err != nil || from != sock.local || !bytes.Equal(buf[:n], []byte("peer traffic")) {
		t.Fatal("profile socket changed", from, err)
	}
	sock.discover(netip.AddrPort{})
	if sock.mapping().IsValid() {
		t.Fatal("withdrawn server retained mapping")
	}
}

func TestRejectUnrelatedBinding(t *testing.T) {
	raw, _ := hex.DecodeString("0101000c2112a4420102030405060708090a0b0c002000080001f523ea12d545")
	server := netip.MustParseAddrPort("192.0.2.1:3478")
	tr := stunTransaction{server: server}
	copy(tr.id[:], raw[8:20])
	if _, ok := tr.mapped(netip.MustParseAddrPort("192.0.2.2:3478"), raw); ok {
		t.Fatal("foreign server")
	}
	for _, pos := range []int{0, 3, 4, 8, 21, 23, 25} {
		bad := bytes.Clone(raw)
		bad[pos] ^= 1
		if _, ok := tr.mapped(server, bad); ok {
			t.Fatal("invalid response", pos)
		}
	}
}
