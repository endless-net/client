package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

var ErrHysteresis = errors.New("working peer path improvement is held")

type peerRouteKind uint8

const (
	peerRouteNone peerRouteKind = iota
	peerRouteRelay
	peerRouteDirect
)

// peerRoute chooses exactly one output per packet. A queue failure never causes
// that same packet to be replayed on another engine. Native stateful ACL remains
// the owner of application grants; this layer enforces pair/path/epoch admission.
type peerRoute struct {
	mu             sync.Mutex
	relay          *relayCipher
	direct         *assignedCipher
	relayConfirmed bool
	kind           peerRouteKind
	epoch          uint64
	hold           *connectivity.OperationBudget
	clock          connectivity.Clock
	profile        string
	generation     uint64
	closed         bool
}

func newPeerRoute(relay *relayCipher, clock connectivity.Clock, profile string, generation uint64) (*peerRoute, error) {
	if relay == nil || clock == nil || profile == "" || generation == 0 {
		return nil, ErrConfiguration
	}
	relay.bind.mu.Lock()
	actual := relay.bind.mapValue.IntentGeneration()
	relay.bind.mu.Unlock()
	if generation != actual {
		return nil, ErrNoAuthority
	}
	return &peerRoute{relay: relay, clock: clock, profile: profile, generation: generation}, nil
}

func (r *peerRoute) relayReadyLocked() bool {
	return r.relayConfirmed && r.relay != nil && !r.relay.sessionLost.Load() && r.relay.bind.permissions().ExistingFlows && r.relay.bind.mux.transport.Ready()
}

func (r *peerRoute) temporarilyFencedLocked() bool {
	if r.relay == nil || r.relay.bind.mux.transport.authorityGone() {
		return false
	}
	r.relay.bind.mu.Lock()
	value := r.relay.bind.mapValue
	r.relay.bind.mu.Unlock()
	lease := r.relay.bind.lease
	if !lease.Permissions().ExistingFlows || !lease.MatchesMap(value) {
		return true
	}
	if r.kind == peerRouteDirect && r.direct != nil {
		r.direct.bind.mu.Lock()
		directMap := r.direct.bind.assignment.mapValue
		r.direct.bind.mu.Unlock()
		// Relay and Direct adopt a map in separate steps. A reader in the
		// middle must fence output, not permanently replace selected Direct.
		return !lease.MatchesMap(directMap)
	}
	return false
}

func (r *peerRoute) refreshLocked() {
	if r.closed {
		return
	}
	if r.temporarilyFencedLocked() {
		return
	}
	if r.relay != nil && r.relay.sessionLost.Load() {
		r.relayConfirmed = false
	}
	if r.kind == peerRouteDirect {
		if r.direct != nil {
			if _, err := r.direct.bind.transmitEpoch(); err == nil {
				return
			}
		}
		r.kind = peerRouteNone
		r.epoch++
		r.hold = nil
	}
	if r.kind == peerRouteRelay && !r.relayReadyLocked() {
		r.kind = peerRouteNone
		r.epoch++
		r.hold = nil
	}
	if r.kind == peerRouteNone && r.relayReadyLocked() {
		r.kind = peerRouteRelay
		r.epoch++
	}
}

func (r *peerRoute) ConfirmRelay(probe *relayHealthProbe) error {
	if probe == nil {
		return ErrNoAuthority
	}
	if _, err := probe.Evidence(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || probe.channel != r.relay {
		return ErrNoAuthority
	}
	r.relayConfirmed = true
	r.refreshLocked()
	return nil
}

func (r *peerRoute) canImproveLocked() error {
	r.refreshLocked()
	if r.temporarilyFencedLocked() {
		return ErrNoAuthority
	}
	if r.closed {
		return ErrClosed
	}
	if r.kind == peerRouteNone || r.hold == nil {
		return nil
	}
	_, err := r.hold.Remaining()
	if errors.Is(err, context.DeadlineExceeded) {
		r.hold = nil
		return nil
	}
	if err != nil {
		return err
	}
	return ErrHysteresis
}

func (r *peerRoute) CanImprove() error { r.mu.Lock(); defer r.mu.Unlock(); return r.canImproveLocked() }

// SelectDirect returns the previous retained receiver to the resource owner.
// Activation performs no network I/O, but is deliberately outside route.mu:
// engine shutdown callbacks must remain able to report path failure.
func (r *peerRoute) SelectDirect(token *directActivation) (*assignedCipher, error) {
	if token == nil {
		return nil, ErrNoAuthority
	}
	r.mu.Lock()
	if token.relay != r.relay {
		r.mu.Unlock()
		return nil, ErrNoAuthority
	}
	if err := r.canImproveLocked(); err != nil {
		r.mu.Unlock()
		return nil, err
	}
	epoch := r.epoch
	previousKind := r.kind
	previous := r.direct
	r.mu.Unlock()
	if err := token.direct.Activate(token); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.epoch != epoch || token.relay != r.relay {
		return nil, connectivity.ErrSourceChanged
	}
	if _, err := token.direct.bind.transmitEpoch(); err != nil {
		return nil, err
	}
	sample, err := r.clock.Sample()
	if err != nil {
		return nil, connectivity.ErrClock
	}
	var hold *connectivity.OperationBudget
	if previousKind != peerRouteNone && (previousKind != peerRouteDirect || previous != token.direct) {
		hold, err = connectivity.NewOperationBudget(r.clock, r.profile, r.generation, sample, 30*time.Second)
		if err != nil {
			return nil, err
		}
	}
	r.direct = token.direct
	r.kind = peerRouteDirect
	r.epoch++
	r.hold = hold
	if previous == token.direct {
		previous = nil
	}
	return previous, nil
}

func (r *peerRoute) FailDirect(channel *assignedCipher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.direct != channel {
		return
	}
	channel.bind.mu.Lock()
	channel.bind.transmitConfirmed = false
	channel.bind.confirmationEpoch++
	channel.bind.mu.Unlock()
	if r.kind == peerRouteDirect {
		r.kind = peerRouteNone
		r.epoch++
		r.hold = nil
		r.refreshLocked()
	}
}

func (r *peerRoute) FailRelay(channel *relayCipher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.relay != channel {
		return
	}
	r.relayConfirmed = false
	if r.kind == peerRouteRelay {
		r.kind = peerRouteNone
		r.epoch++
		r.hold = nil
	}
}

func (r *peerRoute) ReplaceRelay(channel *relayCipher) error {
	if channel == nil {
		return ErrConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	if channel.bind.lease != r.relay.bind.lease {
		return ErrNoAuthority
	}
	r.relay.bind.mu.Lock()
	local, peer, account := r.relay.bind.local, r.relay.bind.peer, r.relay.bind.account
	r.relay.bind.mu.Unlock()
	channel.bind.mu.Lock()
	nextLocal, nextPeer, nextAccount := channel.bind.local, channel.bind.peer, channel.bind.account
	nextGeneration := channel.bind.mapValue.IntentGeneration()
	channel.bind.mu.Unlock()
	if local.ID != nextLocal.ID || local.PublicKey != nextLocal.PublicKey || peer.ID != nextPeer.ID || peer.PublicKey != nextPeer.PublicKey || account != nextAccount || r.generation != nextGeneration || r.relay.bind.mux.transport.network != channel.bind.mux.transport.network {
		return ErrNoAuthority
	}
	r.relay = channel
	r.relayConfirmed = false
	r.epoch++
	if r.kind == peerRouteRelay {
		r.kind = peerRouteNone
		r.hold = nil
	}
	return nil
}

func (r *peerRoute) guard(kind peerRouteKind, epoch uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.epoch != epoch || r.kind != kind {
		return false
	}
	if kind == peerRouteRelay {
		return r.relayReadyLocked()
	}
	if kind == peerRouteDirect && r.direct != nil {
		_, err := r.direct.bind.transmitEpoch()
		return err == nil
	}
	return false
}

func (r *peerRoute) Send(raw []byte) error {
	return r.sendChecked(raw, nil)
}

func (r *peerRoute) sendChecked(raw []byte, outer func() bool) error {
	r.mu.Lock()
	r.refreshLocked()
	kind, epoch, relay, direct, closed := r.kind, r.epoch, r.relay, r.direct, r.closed
	r.mu.Unlock()
	if closed {
		return ErrClosed
	}
	guard := func() bool { return r.guard(kind, epoch) && (outer == nil || outer()) }
	switch kind {
	case peerRouteRelay:
		return relay.queueApplication(raw, guard)
	case peerRouteDirect:
		return direct.queueApplication(raw, guard)
	default:
		return ErrNoAuthority
	}
}

func (r *peerRoute) Current() peerRouteKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshLocked()
	if r.temporarilyFencedLocked() {
		return peerRouteNone
	}
	return r.kind
}

// Invalidate stops output selection; the resource owner closes engines outside
// this mutex and releases their slots only after complete worker shutdown.
func (r *peerRoute) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.kind = peerRouteNone
	r.epoch++
	r.hold = nil
}
