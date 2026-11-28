package dataplane

import (
	"bytes"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestWindowsCandidateReportsDeliveredDatagramSize(t *testing.T) {
	address := netip.MustParseAddr("127.0.0.1")
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var index uint32
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			index = uint32(iface.Index)
			break
		}
	}
	if index == 0 {
		t.Fatal("missing loopback interface")
	}
	open := func() *windowsLANCandidate {
		t.Helper()
		bind := &candidateBind{assignment: candidateAssignment{localEndpoint: "127.0.0.1:1"}}
		conn, err := bind.listenUDP(0)
		if err != nil {
			t.Fatal(err)
		}
		socket := conn.(*windowsLANCandidate)
		// This test uses loopback in place of a discovered LAN interface.
		socket.address, socket.index, socket.checked = address, index, time.Now()
		t.Cleanup(func() { socket.Close() })
		return socket
	}
	sender, receiver := open(), open()
	dst := netip.AddrPortFrom(address, receiver.LocalAddr().(*net.UDPAddr).AddrPort().Port())
	if err := receiver.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload := []byte("probe")
	sent, err := sender.WriteToUDPAddrPort(payload, dst)
	if err != nil {
		t.Fatal(err)
	}
	var data [32]byte
	n, source, err := receiver.ReadFromUDPAddrPort(data[:])
	if err != nil || !bytes.Equal(data[:n], payload) || source.Port() != sender.LocalAddr().(*net.UDPAddr).AddrPort().Port() {
		t.Fatalf("receive n=%d source=%s err=%v", n, source, err)
	}
	if sent != n {
		t.Fatalf("socket reported %d bytes for a delivered %d-byte datagram", sent, n)
	}
}
