//go:build !windows

package dataplane

import "net/netip"

const multiInterfaceNAT = false

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
