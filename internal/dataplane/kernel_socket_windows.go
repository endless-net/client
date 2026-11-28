package dataplane

import (
	"context"
	"errors"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

func (d *windowsPeerDevice) attachCandidate(b *candidateBind) (*kernelCandidateBroker, error) {
	if b == nil || d.sourceChanged == nil {
		return nil, ErrNoAuthority
	}
	link, err := d.activeLink()
	if err != nil {
		return nil, err
	}
	return attachKernelBroker(link, b, d.assignment, d.sourceChanged, wireGuardHandshakePacket)
}

// Reserve a distinct loopback endpoint for this candidate before publishing
// the peer configuration to WireGuardNT. WinSock's exclusive bind prevents a
// second socket from adopting the endpoint with SO_REUSEADDR.
func openWindowsPeerSocket(ctx context.Context) (*net.UDPConn, error) {
	return openExclusiveUDP(ctx, "udp4", "127.0.0.1:0")
}

func openExclusiveUDP(ctx context.Context, network, address string) (*net.UDPConn, error) {
	lc := exclusiveListenConfig()
	socket, err := lc.ListenPacket(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return socket.(*net.UDPConn), nil
}

func exclusiveListenConfig() net.ListenConfig {
	return net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optErr error
		err := raw.Control(func(fd uintptr) {
			const exclusiveAddrUse = ^windows.SO_REUSEADDR // winsock2.h SO_EXCLUSIVEADDRUSE
			optErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, exclusiveAddrUse, 1)
		})
		return errors.Join(err, optErr)
	}}
}
