//go:build !windows

package dataplane

import (
	"net"
	"net/netip"
)

func (b *candidateBind) listenUDP(port uint16) (candidateSocket, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
}

const multiInterfaceNAT = false

func openNATUDP(addr netip.Addr, port uint16) (*net.UDPConn, error) {
	return net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, port)))
}
func (s *natSocket) readNAT(buf []byte) (int, netip.AddrPort, netip.Addr, error) {
	n, peer, err := s.UDPConn.ReadFromUDPAddrPort(buf)
	return n, peer, s.local.Addr(), err
}
func (s *natSocket) writeNAT(buf []byte, peer netip.AddrPort, local netip.Addr) (int, error) {
	if local != s.local.Addr() {
		return 0, ErrNoAuthority
	}
	return s.UDPConn.WriteToUDPAddrPort(buf, peer)
}
