package dataplane

import (
	"context"
	"errors"
	"sync"

	"github.com/endless-net/client/internal/connectivity"
)

var ErrTransportCapacity = errors.New("peer transport capacity exhausted")

// TransportResources is allocated once by the Client connection factory and
// shared across intent generations. It contains no keys or network authority.
// A zero value is invalid; callers cannot reset accounting on session renewal.
type TransportResources struct{ budget *transportBudget }

func NewTransportResources(maxPeers int) (*TransportResources, error) {
	budget, err := newTransportBudget(maxPeers)
	if err != nil {
		return nil, err
	}
	return &TransportResources{budget: budget}, nil
}

type engineRole uint8

const (
	enginePrimary engineRole = iota
	engineRelay
	engineDirect
	engineTransient
)

// transportBudget is shared by all peer supervisors of one Client. The legacy
// primary engine is included in 2*MaxPeers+2; two transient slots are a ceiling,
// not a promise of two free slots when all other roles are occupied.
type transportBudget struct {
	mu       sync.Mutex
	maxPeers int
	engines  map[*engineReservation]bool
	probes   map[*probePermit]bool
}
type engineReservation struct {
	budget *transportBudget
	owner  *connectivity.Lease
	peer   string
	role   engineRole
	done   chan struct{}
}
type probePermit struct {
	budget  *transportBudget
	attempt *probeAttempt
	stop    func() bool
	closed  bool
}

func newTransportBudget(maxPeers int) (*transportBudget, error) {
	if maxPeers < 1 || maxPeers > (int(^uint(0)>>1)-2)/2 {
		return nil, ErrConfiguration
	}
	return &transportBudget{maxPeers: maxPeers, engines: make(map[*engineReservation]bool), probes: make(map[*probePermit]bool)}, nil
}

func (b *transportBudget) reserve(owner *connectivity.Lease, peer string, role engineRole) (*engineReservation, error) {
	if owner == nil || role > engineTransient || role != enginePrimary && (peer == "" || len(peer) > 128) || role == enginePrimary && peer != "" {
		return nil, ErrConfiguration
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.engines) >= 2*b.maxPeers+2 {
		return nil, ErrTransportCapacity
	}
	count := 0
	for current := range b.engines {
		if current.role == role {
			count++
			if role != engineTransient && current.owner == owner && current.peer == peer {
				return nil, ErrTransportCapacity
			}
		}
	}
	limit := b.maxPeers
	switch role {
	case enginePrimary:
		limit = 1
	case engineTransient:
		limit = 2
	}
	if count >= limit {
		return nil, ErrTransportCapacity
	}
	slot := &engineReservation{budget: b, owner: owner, peer: peer, role: role, done: make(chan struct{})}
	b.engines[slot] = true
	return slot, nil
}

// promote exchanges an existing retained Direct slot for the candidate. The
// former receiver remains a counted transient until the owner closes it.
func (b *transportBudget) promote(slot *engineReservation) (*engineReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if slot == nil || slot.budget != b || !b.engines[slot] {
		return nil, ErrClosed
	}
	if slot.role == engineDirect {
		return nil, nil
	}
	if slot.role != engineTransient {
		return nil, ErrConfiguration
	}
	var old *engineReservation
	directs := 0
	for current := range b.engines {
		if current.role == engineDirect {
			directs++
			if current.owner == slot.owner && current.peer == slot.peer {
				old = current
			}
		}
	}
	if old == nil && directs >= b.maxPeers {
		return nil, ErrTransportCapacity
	}
	if old != nil {
		old.role = engineTransient
	}
	slot.role = engineDirect
	return old, nil
}

func (s *engineReservation) release() {
	if s == nil {
		return
	}
	b := s.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.engines[s] {
		delete(b.engines, s)
		close(s.done)
	}
}

func (b *transportBudget) tryProbe(attempt *probeAttempt) (*probePermit, error) {
	if attempt == nil {
		return nil, ErrConfiguration
	}
	if err := attempt.check(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for permit := range b.probes {
		if permit.attempt.check() != nil {
			permit.releaseLocked()
		} else if permit.attempt == attempt {
			return permit, nil
		}
	}
	if len(b.probes) >= 2 {
		return nil, ErrTransportCapacity
	}
	permit := &probePermit{budget: b, attempt: attempt}
	b.probes[permit] = true
	permit.stop = context.AfterFunc(attempt.ctx, permit.release)
	return permit, nil
}
func (p *probePermit) releaseLocked() {
	if p.closed {
		return
	}
	p.closed = true
	delete(p.budget.probes, p)
	if p.stop != nil {
		p.stop()
	}
}
func (p *probePermit) release() {
	if p == nil {
		return
	}
	p.budget.mu.Lock()
	defer p.budget.mu.Unlock()
	p.releaseLocked()
}

func (b *transportBudget) newCycle(parent context.Context, clock connectivity.Clock, profile string, generation uint64, id string) (*probeCycle, error) {
	cycle, err := newProbeCycle(parent, clock, profile, generation, id)
	if err != nil {
		return nil, err
	}
	cycle.budget = b
	return cycle, nil
}

func (b *transportBudget) newAssigned(parent context.Context, value connectivity.Map, lease *connectivity.Lease, peerID, profileID string, private []byte, mtu int, clock connectivity.Clock, sink func([]byte) error, socketFactory func(uint16) (candidateSocket, error)) (*assignedCipher, error) {
	slot, err := b.reserve(lease, peerID, engineTransient)
	if err != nil {
		return nil, err
	}
	channel, err := buildAssignedCipher(parent, value, lease, peerID, profileID, private, mtu, clock, sink, socketFactory)
	if err != nil {
		slot.release()
		return nil, err
	}
	channel.mu.Lock()
	closed := channel.closed
	if !closed {
		channel.reservation = slot
	}
	channel.mu.Unlock()
	if closed {
		channel.Close()
		slot.release()
		return nil, ErrClosed
	}
	return channel, nil
}

func (b *transportBudget) newRelay(parent context.Context, mux *relayMux, value connectivity.Map, lease *connectivity.Lease, peerID string, private []byte, mtu int, sink func([]byte) error) (*relayCipher, error) {
	slot, err := b.reserve(lease, peerID, engineRelay)
	if err != nil {
		return nil, err
	}
	channel, err := newRelayCipher(parent, mux, value, lease, peerID, private, mtu, sink)
	if err != nil {
		slot.release()
		return nil, err
	}
	channel.mu.Lock()
	closed := channel.closed
	if !closed {
		channel.reservation = slot
	}
	channel.mu.Unlock()
	if closed {
		channel.Close()
		slot.release()
		return nil, ErrClosed
	}
	return channel, nil
}
