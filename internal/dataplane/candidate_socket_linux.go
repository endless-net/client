package dataplane

import (
	"context"
	"net"
	"net/netip"
	"strconv"

	"github.com/endless-net/client/internal/underlay"
)

func (b *candidateBind) listenUDP(port uint16) (candidateSocket, error) {
	listener := net.ListenConfig{Control: underlay.Control}
	packet, err := listener.ListenPacket(b.ctx, "udp4", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return nil, err
	}
	return packet.(*net.UDPConn), nil
}

func openNATUDP(addr netip.Addr, port uint16) (*net.UDPConn, error) {
	listener := net.ListenConfig{Control: underlay.Control}
	packet, err := listener.ListenPacket(context.Background(), "udp4", netip.AddrPortFrom(addr, port).String())
	if err != nil {
		return nil, err
	}
	return packet.(*net.UDPConn), nil
}
