//go:build windows && integration

package dataplane

import (
	"net"
	"net/netip"
	"testing"
	"time"
)

// Exercises the production wildcard socket and WinSock ancillary data.
func TestWindowsCandidateUsesSelectedInterface(t *testing.T) {
	rows, err := LANIPv4Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Skip("requires two active IPv4 interface addresses")
	}
	b := &candidateBind{}
	candidate, err := b.listenUDP(0)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	socket := candidate.(*windowsLANCandidate)
	for addr := range rows {
		t.Run(addr.String(), func(t *testing.T) {
			peer, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, 0)))
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			port := uint16(socket.LocalAddr().(*net.UDPAddr).Port)
			local := netip.AddrPortFrom(addr, port)
			b.mu.Lock()
			b.assignment.localEndpoint = local.String()
			b.mu.Unlock()
			deadline := time.Now().Add(2 * time.Second)
			if err := peer.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			if err := socket.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			target := peer.LocalAddr().(*net.UDPAddr).AddrPort()
			if _, err := socket.WriteToUDPAddrPort([]byte{1}, target); err != nil {
				t.Fatal(err)
			}
			var data [8]byte
			n, source, err := peer.ReadFromUDPAddrPort(data[:])
			if err != nil || n != 1 || data[0] != 1 || source != local {
				t.Fatalf("outbound %v %v %v", source, n, err)
			}
			for other := range rows {
				if other != addr {
					sender, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(other, 0)))
					if err != nil {
						t.Fatal(err)
					}
					defer sender.Close()
					if _, err := sender.WriteToUDPAddrPort([]byte{9}, netip.AddrPortFrom(other, port)); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if _, err = peer.WriteToUDPAddrPort([]byte{2}, local); err != nil {
				t.Fatal(err)
			}
			n, source, err = socket.ReadFromUDPAddrPort(data[:])
			if err != nil || n != 1 || data[0] != 2 || source != target {
				t.Fatalf("inbound %v %v %v", source, n, err)
			}
		})
	}
}
