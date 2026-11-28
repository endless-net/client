//go:build !windows && !linux

package dataplane

import (
	"net"
	"net/netip"
)

func (b *candidateBind) listenUDP(port uint16) (candidateSocket, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
}

func openNATUDP(addr netip.Addr, port uint16) (*net.UDPConn, error) {
	return net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, port)))
}
