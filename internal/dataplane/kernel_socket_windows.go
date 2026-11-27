package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"golang.org/x/sys/windows"
)

type windowsCandidateBroker struct {
	link     windowsPeerLink
	bind     *candidateBind
	external candidateSocket
	// A single reader publishes the largest ciphertext before handing it to
	// the driver. Keep it for the lifetime of this link: decrypted packets
	// can be delivered out of order or across maintenance windows.
	largestIngress atomic.Int32
}

func (d *windowsPeerDevice) attachCandidate(b *candidateBind) (*windowsCandidateBroker, error) {
	if b == nil || d.sourceChanged == nil {
		return nil, ErrNoAuthority
	}
	link, err := d.activeLink()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.socket == nil || b.adapterSource != nil || !b.assignment.sameProbe(d.assignment) ||
		!canFinishPreparation(b.ctx, b.clock, b.assignment, d.sourceChanged) {
		return nil, ErrNoAuthority
	}
	b.adapterSource = d.sourceChanged
	b.adapterSocket = b.socket
	b.adapterProbeOnly = true
	b.handshake = wireGuardHandshakePacket
	return &windowsCandidateBroker{link: link, bind: b, external: b.socket}, nil
}

func (w *windowsCandidateBroker) receive() error {
	return forwardKernelIngress(func(p [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
		return w.bind.receive(w.external, p, sizes, endpoints)
	}, func(packet []byte) error {
		if size := int32(len(packet)); size > w.largestIngress.Load() {
			w.largestIngress.Store(size)
		}
		return w.link.write(packet)
	})
}

func (w *windowsCandidateBroker) send(denied func(error)) error {
	var buffer [65535]byte
	for {
		n, err := w.link.read(buffer[:])
		if err != nil {
			return err
		}
		w.bind.mu.Lock()
		target := w.bind.assignment.peerEndpoint
		current := !w.bind.closed && w.bind.socket == w.external
		w.bind.mu.Unlock()
		if !current {
			return net.ErrClosed
		}
		endpoint, err := w.bind.ParseEndpoint(target)
		if err == nil {
			err = w.bind.Send([][]byte{buffer[:n]}, endpoint)
		}
		if err != nil && denied != nil {
			denied(err)
		}
	}
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

// Both sides use the service socket's loopback address.
type windowsPeerLink struct {
	socket *net.UDPConn
	driver netip.AddrPort
}

func (l windowsPeerLink) read(buf []byte) (int, error) {
	for {
		n, source, err := l.socket.ReadFromUDPAddrPort(buf)
		if err != nil || source == l.driver {
			return n, err
		}
	}
}

func (l windowsPeerLink) write(packet []byte) error {
	n, err := l.socket.WriteToUDPAddrPort(packet, l.driver)
	if err == nil && n != len(packet) {
		return ErrPacketIO
	}
	return err
}
