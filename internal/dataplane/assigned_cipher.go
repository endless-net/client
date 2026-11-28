package dataplane

import (
	"context"
	"errors"
	"sync"

	"github.com/endless-net/client/internal/connectivity"
)

// A kernel preparer may return errKernelUnavailable only after releasing any
// partial allocation. Other errors (including uncertain cleanup) stop selection.
var errKernelUnavailable = errors.New("kernel peer transport unavailable")

type directCipherConfig struct {
	relay      *RelayTransport
	assignment candidateAssignment
	private    []byte
	mtu        int
	clock      connectivity.Clock
}

type kernelPreparer func(context.Context, directCipherConfig, *assignedCipher, *packetPort) (directPeerEngine, error)

type directPeerEngine interface {
	Queue([]byte, bool) error
	queueChecked([]byte, bool, func() bool) error
	BoundPort() (uint16, error)
	SetEndpoint(context.Context, string) error
	Close()
}

// assignedCipher is the runtime constructor boundary for a Direct candidate.
// Unlike the low-level crypto adapter it requires a sealed current assignment
// and owns cancellation of its socket, virtual port and pinned engine together.
type assignedCipher struct {
	mu                sync.Mutex
	updateMu          sync.Mutex
	cipher            directPeerEngine
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
	return prepareAssignedCipher(parent, m, lease, peerID, profileID, private, mtu, clock, sink, socketFactory, nil)
}

func prepareAssignedCipher(parent context.Context, m connectivity.Map, lease *connectivity.Lease, peerID, profileID string, private []byte, mtu int, clock connectivity.Clock, sink func([]byte) error, socketFactory func(uint16) (candidateSocket, error), kernel kernelPreparer) (*assignedCipher, error) {
	if parent == nil || parent.Err() != nil || clock == nil || lease == nil || sink == nil {
		return nil, ErrConfiguration
	}
	_, sourceChanged := lease.SourceSignal()
	sample, err := clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	assignment, err := newCandidateAssignment(m, lease, peerID, profileID, sample.Wall)
	if err != nil {
		return nil, err
	}
	cfg := directCipherConfig{assignment: assignment, private: private, mtu: mtu, clock: clock}
	if kernel != nil {
		channel, err := openAssignedEngine(parent, cfg, sink, socketFactory, kernel)
		if !canFinishPreparation(parent, clock, assignment, sourceChanged) {
			if channel != nil {
				channel.Close()
			}
			return nil, ErrNoAuthority
		}
		if err != errKernelUnavailable {
			return channel, err
		}
	}
	channel, err := openAssignedEngine(parent, cfg, sink, socketFactory, prepareGoDirect)
	if err != nil {
		return nil, err
	}
	if !canFinishPreparation(parent, clock, assignment, sourceChanged) {
		channel.Close()
		return nil, ErrNoAuthority
	}
	return channel, nil
}

func openAssignedEngine(parent context.Context, cfg directCipherConfig, sink func([]byte) error, socketFactory func(uint16) (candidateSocket, error), prepare kernelPreparer) (*assignedCipher, error) {
	assignment := cfg.assignment
	ctx, cancel := context.WithCancel(parent)
	bind, err := newCandidateBind(ctx, assignment, cfg.clock)
	if err != nil {
		cancel()
		return nil, err
	}
	if socketFactory != nil {
		bind.listen = socketFactory
	}
	channel := &assignedCipher{bind: bind, cancel: cancel, peerID: assignment.peer.ID, profileID: assignment.offer.Profile.ID, lease: assignment.lease, clock: cfg.clock, sink: sink, done: make(chan struct{})}
	port, err := newPacketPort(ctx, cfg.mtu, bind.permissions, channel.receivePlaintext)
	if err != nil {
		bind.shutdown()
		cancel()
		return nil, err
	}
	engine, err := prepare(ctx, cfg, channel, port)
	if err != nil || engine == nil {
		cancel()
		closeErr := bind.shutdownResult()
		_ = port.Close()
		if engine != nil {
			engine.Close()
			return nil, errors.Join(ErrEngine, err, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(ErrEngine, err, closeErr)
		}
		if err == nil {
			return nil, ErrEngine
		}
		return nil, err
	}
	channel.cipher = engine
	context.AfterFunc(ctx, channel.Close)
	return channel, nil
}

func prepareGoDirect(ctx context.Context, cfg directCipherConfig, channel *assignedCipher, port *packetPort) (directPeerEngine, error) {
	assignment := cfg.assignment
	peer := assignment.peer
	peer.Endpoint = assignment.peerEndpoint
	engine, err := buildPeerCipher(ctx, assignment.local, peer, cfg.private, assignment.offer.Profile, port, channel.bind, nil, func(cipher *peerCipher) {
		channel.bind.handshake = cipher.handshakePacket
		cipher.setPacketHooks(channel.controlSendGuard, channel.controlReceived)
	})
	if err != nil {
		return nil, err
	}
	return engine, nil
}

func canFinishPreparation(ctx context.Context, clock connectivity.Clock, assignment candidateAssignment, sourceChanged <-chan struct{}) bool {
	sample, err := clock.Sample()
	select {
	case <-sourceChanged:
		return false
	default:
		return ctx.Err() == nil && err == nil && assignment.permission(sample.Wall, true) == nil
	}
}

func (c *assignedCipher) receivePlaintext(raw []byte) error {
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	local, peer, err := a.controlAddresses()
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
	source, target, err := a.addresses()
	if err != nil || applicationForPair(raw, target, source) != nil {
		return ErrNoAuthority
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
	return c.updatePair(ctx, m, nil)
}
func (c *assignedCipher) updatePair(ctx context.Context, m connectivity.Map, index *uint32) error {
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
	if index == nil {
		previous := c.bind.assignment
		assignment.selectPair(previous.index)
		assignment.keepPair(previous.localEndpoint, previous.peerEndpoint)
	} else {
		assignment.selectPair(*index)
	}
	previousEndpoint := c.bind.assignment.peerEndpoint
	c.bind.mu.Unlock()
	if err := c.bind.update(assignment); err != nil {
		return err
	}
	if assignment.peerEndpoint != "" && assignment.peerEndpoint != previousEndpoint {
		if err := c.cipher.SetEndpoint(ctx, assignment.peerEndpoint); err != nil {
			return err
		}
	}
	sample, err = c.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	return assignment.permission(sample.Wall, true)
}

// Native application packets can still enter through the OS even when the
// userspace packet switch selects Relay. Fence the broker on route loss.
func (c *assignedCipher) stopRoutedAdapter() {
	c.bind.mu.Lock()
	routed := c.bind.adapterSocket != nil
	c.bind.mu.Unlock()
	if routed {
		c.cancel()
	}
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
