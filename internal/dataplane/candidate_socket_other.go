//go:build !windows

package dataplane

import "net"

func (b *candidateBind) listenUDP(port uint16) (candidateSocket, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
}
