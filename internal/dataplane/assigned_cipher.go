package dataplane

import (
	"context"
	"sync"

	"github.com/endless-net/client/internal/connectivity"
)

// assignedCipher is the runtime constructor boundary for a Direct candidate.
// Unlike the low-level crypto adapter it requires a sealed current assignment
// and owns cancellation of its socket, virtual port and pinned engine together.
type assignedCipher struct {
	mu                sync.Mutex
	updateMu          sync.Mutex
	cipher            *peerCipher
	bind              *candidateBind
	cancel            context.CancelFunc
	peerID, profileID string
	lease             *connectivity.Lease
	clock             connectivity.Clock
	closed            bool
	probe             *pathChallenge
	sink              func([]byte) error
	done              chan struct{}
	reservation       *engineReservation
	meterMu           sync.Mutex
	serviceWindow     *probeCycle
	serviceReceipts   map[[32]byte]*serviceReceipt
}

func (*assignedCipher) String() string   { return "[assigned encrypted peer channel]" }
func (*assignedCipher) GoString() string { return "[assigned encrypted peer channel]" }

func newAssignedCipher(parent context.Context, m connectivity.Map, lease *connectivity.Lease, peerID, profileID string, private []byte, mtu int, clock connectivity.Clock, sink func([]byte) error) (*assignedCipher, error) {
	return buildAssignedCipher(parent, m, lease, peerID, profileID, private, mtu, clock, sink, nil)
}

// socketFactory is a local unit I/O seam, never part of a delivered profile.
func buildAssignedCipher(parent context.Context, m connectivity.Map, lease *connectivity.Lease, peerID, profileID string, private []byte, mtu int, clock connectivity.Clock, sink func([]byte) error, socketFactory func(uint16) (candidateSocket, error)) (*assignedCipher, error) {
	if parent == nil || parent.Err() != nil || clock == nil {
		return nil, ErrConfiguration
	}
	sample, err := clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	assignment, err := newCandidateAssignment(m, lease, peerID, profileID, sample.Wall)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	bind, err := newCandidateBind(ctx, assignment, clock)
	if err != nil {
		cancel()
		return nil, err
	}
	if socketFactory != nil {
		bind.listen = socketFactory
	}
	channel := &assignedCipher{bind: bind, cancel: cancel, peerID: peerID, profileID: profileID, lease: lease, clock: clock, sink: sink, done: make(chan struct{})}
	if sink == nil {
		bind.shutdown()
		cancel()
		return nil, ErrConfiguration
	}
	port, err := newPacketPort(ctx, mtu, bind.permissions, channel.receivePlaintext)
	if err != nil {
		bind.shutdown()
		cancel()
		return nil, err
	}
	peer := assignment.peer
	peer.Endpoint = assignment.offer.PeerEndpoint
	_, err = buildPeerCipher(ctx, assignment.local, peer, private, assignment.offer.Profile, port, bind, nil, func(cipher *peerCipher) {
		channel.cipher = cipher
		bind.handshake = cipher.handshakePacket
		cipher.setPacketHooks(channel.controlSendGuard, channel.controlReceived)
	})
	if err != nil {
		_ = port.Close()
		bind.shutdown()
		cancel()
		return nil, err
	}
	sample, err = clock.Sample()
	if err != nil || assignment.permission(sample.Wall, true) != nil {
		channel.Close()
		return nil, ErrNoAuthority
	}
	context.AfterFunc(ctx, channel.Close)
	return channel, nil
}

func (c *assignedCipher) receivePlaintext(raw []byte) error {
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	local, peer, err := a.addresses()
	if err != nil {
		return err
	}
	value, claimed, err := decodeControlPacket(raw, peer, local)
	if claimed {
		// Invalid control is consumed, never delivered to the application. A bad
		// packet must not shut down an otherwise authorized encrypted peer channel.
		if err != nil {
			return nil
		}
		c.mu.Lock()
		probe := c.probe
		closed := c.closed
		c.mu.Unlock()
		if probe != nil && !closed {
			if probe.receive(value) == nil {
				return nil
			}
		}
		if !closed {
			_ = c.echoConfirmed(value)
		}
		return nil
	}
	if err != nil {
		return err
	}
	return c.sink(raw)
}

func (b *candidateBind) permissions() connectivity.Permission {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return connectivity.Permission{}
	}
	sample, err := b.clock.Sample()
	if err != nil || b.assignment.permission(sample.Wall, false) != nil {
		return connectivity.Permission{}
	}
	return b.assignment.lease.Permissions()
}

func (c *assignedCipher) Update(ctx context.Context, m connectivity.Map) error {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.mu.Unlock()
	if ctx == nil || ctx.Err() != nil {
		return ErrConfiguration
	}
	sample, err := c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	assignment, err := newCandidateAssignment(m, c.lease, c.peerID, c.profileID, sample.Wall)
	if err != nil {
		return err
	}
	c.bind.mu.Lock()
	previousEndpoint := c.bind.assignment.offer.PeerEndpoint
	c.bind.mu.Unlock()
	if err := c.bind.update(assignment); err != nil {
		return err
	}
	if assignment.offer.PeerEndpoint != "" && assignment.offer.PeerEndpoint != previousEndpoint {
		if err := c.cipher.SetEndpoint(ctx, assignment.offer.PeerEndpoint); err != nil {
			return err
		}
	}
	sample, err = c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	return assignment.permission(sample.Wall, true)
}

func (c *assignedCipher) Close() {
	c.mu.Lock()
	if c.closed {
		done := c.done
		c.mu.Unlock()
		<-done
		return
	}
	c.closed = true
	reservation := c.reservation
	c.mu.Unlock()
	c.cancel()
	c.bind.shutdown()
	c.cipher.Close()
	if reservation != nil {
		reservation.release()
	}
	close(c.done)
}
