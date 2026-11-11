package dataplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"net/netip"
	"sync"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/internal/connectivity"
)

type candidateSocket interface {
	ReadFromUDPAddrPort([]byte) (int, netip.AddrPort, error)
	WriteToUDPAddrPort([]byte, netip.AddrPort) (int, error)
	SetWriteDeadline(time.Time) error
	LocalAddr() net.Addr
	Close() error
}

// candidateBind owns an IPv4-only socket for one signed pair/profile. It cannot
// roam to a peer-supplied endpoint or use DNS. Unconfirmed I/O requires an
// admitted probe; application output requires a bilateral activation token.
type candidateBind struct {
	mu                                  sync.Mutex
	ctx                                 context.Context
	cancel                              context.CancelFunc
	assignment                          candidateAssignment
	clock                               connectivity.Clock
	socket                              candidateSocket
	port                                uint16
	listen                              func(uint16) (candidateSocket, error)
	probe                               *probeAttempt
	sendGate                            chan struct{}
	closed                              bool
	receiveConfirmed, transmitConfirmed bool
	confirmationEpoch                   uint64
	revisionFloor                       uint64
	handshake                           func([]byte) bool
	lastHandshake                       []byte
	handshakeAttempt                    *probeAttempt
}

func newCandidateBind(parent context.Context, a candidateAssignment, clock connectivity.Clock) (*candidateBind, error) {
	if parent == nil || parent.Err() != nil || clock == nil {
		return nil, ErrConfiguration
	}
	sample, err := clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	if err := a.permission(sample.Wall, true); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	// A new channel has no prior nonce/history. A peer may have prepared it
	// before our construction; current pair authority and fresh own proofs still
	// apply. Only an observed semantic change advances the rejection floor.
	b := &candidateBind{ctx: ctx, cancel: cancel, assignment: a, revisionFloor: 1, clock: clock, sendGate: make(chan struct{}, 1), listen: func(port uint16) (candidateSocket, error) {
		return net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
	}}
	b.sendGate <- struct{}{}
	context.AfterFunc(ctx, b.shutdown)
	return b, nil
}

func (*candidateBind) BatchSize() int { return 1 }
func (*candidateBind) SetMark(mark uint32) error {
	if mark != 0 {
		return ErrConfiguration
	}
	return nil
}
func (b *candidateBind) Close() error {
	b.mu.Lock()
	socket := b.socket
	b.socket = nil
	b.port = 0
	b.receiveConfirmed = false
	b.transmitConfirmed = false
	b.confirmationEpoch++
	clear(b.lastHandshake)
	b.lastHandshake, b.handshakeAttempt = nil, nil
	if b.probe != nil {
		b.probe.cancel()
		b.probe = nil
	}
	b.mu.Unlock()
	if socket != nil {
		return socket.Close()
	}
	return nil
}

// Bind.Close must permit upstream's Close/Open during Up and BindUpdate.
// Only cancellation of the channel owner permanently shuts this bind down.
func (b *candidateBind) shutdown() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.cancel()
	_ = b.Close()
}

func (b *candidateBind) Open(port uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return nil, 0, net.ErrClosed
	}
	if b.socket != nil {
		return nil, 0, awgconn.ErrBindAlreadyOpen
	}
	sample, err := b.clock.Sample()
	if err != nil {
		return nil, 0, connectivity.ErrClock
	}
	if err := b.assignment.permission(sample.Wall, true); err != nil {
		return nil, 0, err
	}
	socket, err := b.listen(port)
	if err != nil {
		return nil, 0, ErrPacketIO
	}
	address, ok := socket.LocalAddr().(*net.UDPAddr)
	if !ok || address.Port < 1 || address.Port > 65535 || port != 0 && address.Port != int(port) {
		_ = socket.Close()
		return nil, 0, ErrPacketIO
	}
	b.socket = socket
	b.port = uint16(address.Port)
	return []awgconn.ReceiveFunc{func(packets [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
		return b.receive(socket, packets, sizes, endpoints)
	}}, uint16(address.Port), nil
}

func (b *candidateBind) ParseEndpoint(value string) (awgconn.Endpoint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	address, err := netip.ParseAddrPort(value)
	if err != nil || !address.Addr().Is4() || address.Port() == 0 || value != address.String() || value != b.assignment.offer.PeerEndpoint {
		return nil, ErrConfiguration
	}
	return &awgconn.StdNetEndpoint{AddrPort: address}, nil
}

func (b *candidateBind) beginProbe(p *probeAttempt) error {
	if err := p.check(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return net.ErrClosed
	}
	sample, err := b.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if err := b.assignment.permission(sample.Wall, true); err != nil {
		return err
	}
	if !b.endpointsReady() {
		return ErrNoAuthority
	}
	if p.cycle.generation != b.assignment.plan.LocalIntentGeneration {
		return ErrNoAuthority
	}
	if b.probe != nil && p.cycle == b.probe.cycle && p.sequence < b.probe.sequence {
		return ErrNoAuthority
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.owner != nil && p.owner != b {
		return ErrNoAuthority
	}
	if p.assignment != nil && !p.assignment.sameProbe(b.assignment) {
		return ErrNoAuthority
	}
	p.owner = b
	if p.assignment == nil {
		scope := b.assignment
		p.assignment = &scope
	}
	if b.probe != p {
		clear(b.lastHandshake)
		b.lastHandshake, b.handshakeAttempt = nil, nil
		b.probe = p
	}
	return nil
}

// update retains the acknowledged listener only for an unchanged cryptographic
// channel. A semantic map change invalidates its old nonce before any next I/O.
func (b *candidateBind) update(a candidateAssignment) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return net.ErrClosed
	}
	if !b.assignment.sameChannel(a) {
		return ErrConfiguration
	}
	if !b.assignment.sameProbe(a) {
		clear(b.lastHandshake)
		b.lastHandshake, b.handshakeAttempt = nil, nil
		b.revisionFloor = a.plan.Revision.Network
		b.confirmationEpoch++
		if b.assignment.offer.LocalEndpoint != a.offer.LocalEndpoint || b.assignment.offer.PeerEndpoint != a.offer.PeerEndpoint {
			b.receiveConfirmed = false
			b.transmitConfirmed = false
		}
		if b.probe != nil {
			b.probe.cancel()
		}
		b.probe = nil
	}
	b.assignment = a
	return nil
}

func (b *candidateBind) revisionAllowed(revision uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed && revision != 0 && revision >= b.revisionFloor && revision <= b.assignment.plan.Revision.Network
}

func (b *candidateBind) admit(p *probeAttempt, endpoint netip.AddrPort) (candidateSocket, time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil || b.socket == nil {
		return nil, time.Time{}, net.ErrClosed
	}
	if p == nil || p != b.probe || endpoint.String() != b.assignment.offer.PeerEndpoint || !b.endpointsReady() {
		return nil, time.Time{}, ErrNoAuthority
	}
	sample, err := b.clock.Sample()
	if err != nil {
		return nil, time.Time{}, connectivity.ErrClock
	}
	if err := b.assignment.permission(sample.Wall, true); err != nil {
		return nil, time.Time{}, err
	}
	if err := p.check(); err != nil {
		return nil, time.Time{}, err
	}
	remaining, err := p.timer.Remaining()
	if err != nil {
		return nil, time.Time{}, err
	}
	deadline := minTime(sample.Wall.Add(remaining), b.assignment.plan.ExpiresAt)
	if leaseEnd := b.assignment.lease.Permissions().ExpiresAt; leaseEnd.Before(deadline) {
		deadline = leaseEnd
	}
	if end, ok := p.ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	return b.socket, deadline, nil
}

// Called with mu held. A prepared listener is not probe-ready until the signed
// local advertisement confirms its acknowledged port and the peer has an endpoint.
func (b *candidateBind) endpointsReady() bool {
	local, err := netip.ParseAddrPort(b.assignment.offer.LocalEndpoint)
	return err == nil && b.socket != nil && b.port != 0 && local.Port() == b.port && b.assignment.offer.PeerEndpoint != ""
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (b *candidateBind) write(p *probeAttempt, endpoint netip.AddrPort, raw []byte) error {
	socket, deadline, err := b.admit(p, endpoint)
	if err != nil {
		return err
	}
	// Count both directions at this endpoint, including IPv4 and UDP headers.
	if err := p.cycle.charge(len(raw) + 28); err != nil {
		return err
	}
	if socket.SetWriteDeadline(deadline) != nil {
		return ErrPacketIO
	}
	if _, _, err := b.admit(p, endpoint); err != nil {
		return err
	}
	count, err := socket.WriteToUDPAddrPort(raw, endpoint)
	if err != nil || count != len(raw) {
		return ErrPacketIO
	}
	if _, _, err := b.admit(p, endpoint); err != nil {
		return err
	}
	return nil
}

func (b *candidateBind) Send(packets [][]byte, endpoint awgconn.Endpoint) error {
	return b.send(packets, endpoint, nil)
}

// SendGuarded keeps the plaintext packet's frozen authorization attached through
// crypto staging and executes it under the socket serialization gate. A guard
// checked before waiting for sendGate could expire while another packet writes.
func (b *candidateBind) SendGuarded(packets [][]byte, endpoint awgconn.Endpoint, guards []func(int) error) error {
	if len(guards) != 1 {
		return ErrPacketIO
	}
	return b.send(packets, endpoint, guards[0])
}

func (b *candidateBind) send(packets [][]byte, endpoint awgconn.Endpoint, guard func(int) error) error {
	if len(packets) != 1 || len(packets[0]) == 0 || len(packets[0]) > 65507 || endpoint == nil {
		return ErrPacketIO
	}
	address, err := netip.ParseAddrPort(endpoint.DstToString())
	if err != nil {
		return ErrConfiguration
	}
	b.mu.Lock()
	p := b.probe
	confirmed, epoch := b.receiveConfirmed || b.transmitConfirmed, b.confirmationEpoch
	b.mu.Unlock()
	if confirmed {
		return b.sendConfirmed(packets[0], address, epoch, guard)
	}
	if p == nil {
		return ErrNoAuthority
	}
	select {
	case <-b.ctx.Done():
		return net.ErrClosed
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-b.sendGate:
	}
	defer func() { b.sendGate <- struct{}{} }()
	if _, _, err := b.admit(p, address); err != nil {
		return err
	}
	if err := b.emitPrelude(p, address); err != nil {
		return err
	}
	if b.handshake != nil && b.handshake(packets[0]) {
		b.mu.Lock()
		if b.probe == p && !b.closed {
			clear(b.lastHandshake)
			b.lastHandshake, b.handshakeAttempt = bytes.Clone(packets[0]), p
		}
		b.mu.Unlock()
	}
	if guard != nil {
		if err := guard(len(packets[0])); err != nil {
			return err
		}
	}
	return b.write(p, address, packets[0])
}

func (b *candidateBind) resendHandshake(p *probeAttempt) error {
	if err := p.check(); err != nil {
		return err
	}
	b.mu.Lock()
	if b.probe != p || b.handshakeAttempt != p || b.receiveConfirmed || b.transmitConfirmed || len(b.lastHandshake) == 0 {
		b.mu.Unlock()
		return nil
	}
	raw := bytes.Clone(b.lastHandshake)
	address, err := netip.ParseAddrPort(b.assignment.offer.PeerEndpoint)
	b.mu.Unlock()
	defer clear(raw)
	if err != nil {
		return ErrNoAuthority
	}
	select {
	case <-b.ctx.Done():
		return ErrClosed
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-b.sendGate:
	}
	defer func() { b.sendGate <- struct{}{} }()
	return b.write(p, address, raw)
}

// Called under sendGate: no handshake can overtake its bounded declarative
// prelude, and a repeated Send/prepare cannot replay or reset that prelude.
func (b *candidateBind) emitPrelude(p *probeAttempt, address netip.AddrPort) (result error) {
	p.mu.Lock()
	if p.preludeStarted {
		result = p.preludeError
		p.mu.Unlock()
		return result
	}
	p.preludeStarted = true
	p.mu.Unlock()
	b.mu.Lock()
	datagrams := b.assignment.offer.Profile.Prelude
	b.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.preludeError = result
		p.mu.Unlock()
	}()
	for _, datagram := range datagrams {
		if datagram.DelayMS > 0 {
			timer := time.NewTimer(time.Duration(datagram.DelayMS) * time.Millisecond)
			select {
			case <-b.ctx.Done():
				timer.Stop()
				return net.ErrClosed
			case <-p.ctx.Done():
				timer.Stop()
				return p.ctx.Err()
			case <-timer.C:
			}
		}
		if _, _, err := b.admit(p, address); err != nil {
			return err
		}
		var raw []byte
		for _, segment := range datagram.Segments {
			raw = append(raw, segment.Bytes...)
			if segment.Random > 0 {
				start := len(raw)
				raw = append(raw, make([]byte, int(segment.Random))...)
				if _, err := rand.Read(raw[start:]); err != nil {
					clear(raw)
					return ErrPacketIO
				}
			}
		}
		err := b.write(p, address, raw)
		clear(raw)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *candidateBind) receive(socket candidateSocket, packets [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
	if len(packets) != 1 || len(sizes) != 1 || len(endpoints) != 1 {
		return 0, ErrPacketIO
	}
	count, address, err := socket.ReadFromUDPAddrPort(packets[0])
	if err != nil {
		return 0, err
	}
	if count <= 0 || count > len(packets[0]) {
		return 0, ErrPacketIO
	}
	b.mu.Lock()
	p := b.probe
	current := b.socket == socket
	confirmed, epoch := b.receiveConfirmed, b.confirmationEpoch
	b.mu.Unlock()
	if !current {
		return 0, net.ErrClosed
	}
	if confirmed {
		if _, _, err := b.confirmedAdmission(address, epoch); err != nil {
			clear(packets[0][:count])
			return 0, nil
		}
		sizes[0] = count
		endpoints[0] = &awgconn.StdNetEndpoint{AddrPort: address}
		return 1, nil
	}
	if _, _, err := b.admit(p, address); err != nil {
		clear(packets[0][:count])
		return 0, nil
	}
	if err := p.cycle.charge(count + 28); err != nil {
		clear(packets[0][:count])
		return 0, nil
	}
	sizes[0] = count
	endpoints[0] = &awgconn.StdNetEndpoint{AddrPort: address}
	return 1, nil
}
