//go:build linux || windows

package dataplane

import (
	"net"
	"net/netip"
	"sync/atomic"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
)

type kernelCandidateBroker struct {
	link     kernelPeerLink
	bind     *candidateBind
	external candidateSocket
	// A single reader publishes the largest ciphertext before handing it to
	// the driver. Keep it for the lifetime of this link: decrypted packets
	// can be delivered out of order or across maintenance windows.
	largestIngress atomic.Int32
}

func attachWireGuardBroker(link kernelPeerLink, b *candidateBind, assignment candidateAssignment, changed <-chan struct{}) (*kernelCandidateBroker, error) {
	if b == nil || changed == nil || link.socket == nil || !link.driver.Addr().IsLoopback() || link.driver.Port() == 0 {
		return nil, ErrNoAuthority
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.socket == nil || b.adapterSource != nil || !b.assignment.sameProbe(assignment) ||
		!canFinishPreparation(b.ctx, b.clock, b.assignment, changed) {
		return nil, ErrNoAuthority
	}
	b.adapterSource = changed
	b.adapterSocket = b.socket
	b.adapterProbeOnly = true
	b.handshake = wireGuardHandshakePacket
	return &kernelCandidateBroker{link: link, bind: b, external: b.socket}, nil
}

func (w *kernelCandidateBroker) receive() error {
	return forwardKernelIngress(func(p [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
		return w.bind.receive(w.external, p, sizes, endpoints)
	}, w.forwardIngress)
}

func (w *kernelCandidateBroker) forwardIngress(packet []byte) error {
	if len(packet) == 0 || len(packet) > 65507 {
		return ErrPacketIO
	}
	if size := int32(len(packet)); size > w.largestIngress.Load() {
		w.largestIngress.Store(size)
	}
	return w.link.write(packet)
}

func (w *kernelCandidateBroker) send(denied func(error)) error {
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

// Both sides use the service socket's loopback address.
type kernelPeerLink struct {
	socket *net.UDPConn
	driver netip.AddrPort
}

func (l kernelPeerLink) read(buf []byte) (int, error) {
	for {
		n, source, err := l.socket.ReadFromUDPAddrPort(buf)
		if err != nil || source == l.driver {
			return n, err
		}
	}
}

func (l kernelPeerLink) write(packet []byte) error {
	n, err := l.socket.WriteToUDPAddrPort(packet, l.driver)
	if err == nil && n != len(packet) {
		return ErrPacketIO
	}
	return err
}

func (broker *kernelCandidateBroker) chargeIngress(channel *assignedCipher, window *probeCycle) error {
	b := channel.bind
	if broker.bind != b || broker.external == nil {
		return ErrNoAuthority
	}
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil || b.adapterSocket != broker.external {
		b.mu.Unlock()
		return ErrNoAuthority
	}
	guarded := b.adapterProbeOnly
	err := b.checkAdapterLocked()
	b.mu.Unlock()
	if err != nil || guarded {
		// Discovery ingress was already charged on the external socket.
		return err
	}
	return channel.chargeService(int(broker.largestIngress.Load()), window)
}
