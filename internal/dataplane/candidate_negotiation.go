package dataplane

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	relaywire "github.com/endless-net/relay/api/wire"
	wgdevice "github.com/tailscale/wireguard-go/device"
)

const (
	negotiationRetry   = 3 * time.Second / 3
	stableProbeSpacing = 5 * time.Second / (3 - 1)
)

// candidateNegotiation is serialized by the peer supervisor. Incoming controls
// must be consumed from this relayCipher's authenticated Events, then rechecked
// here against current scope. It owns no native resources or application queue.
type candidateNegotiation struct {
	direct                                  *assignedCipher
	relay                                   *relayCipher
	cycle                                   *probeCycle
	assignment                              candidateAssignment
	history                                 *directStability
	initiator                               bool
	prepare                                 *transport.Control
	prepareBudget                           *probeAttempt
	discoveryBudget                         *probeAttempt
	remotePrepare                           *transport.Control
	seenPrepares                            map[[32]byte]bool
	probe                                   *pathChallenge
	recorded                                bool
	qualified                               *directQualification
	commit, pendingCommit                   *transport.Control
	acknowledgedCommit, acknowledgement     *transport.Control
	activation                              *directActivation
	activationTaken                         bool
	receiveGrant                            *directReceiveGrant
	receiveGrantTaken                       bool
	lastAction, lastProbeSend, trialStarted connectivity.ClockSample
}

type directActivation struct {
	used                              atomic.Bool
	direct                            *assignedCipher
	relay                             *relayCipher
	qualified                         *directQualification
	commit, acknowledgement, received transport.Control
}

type directReceiveGrant struct {
	used      atomic.Bool
	direct    *assignedCipher
	relay     *relayCipher
	qualified *directQualification
	commit    transport.Control
}

func (*directReceiveGrant) String() string   { return "[qualified Direct receive eligibility]" }
func (*directReceiveGrant) GoString() string { return "[qualified Direct receive eligibility]" }

func (*directActivation) String() string   { return "[bilaterally confirmed Direct activation]" }
func (*directActivation) GoString() string { return "[bilaterally confirmed Direct activation]" }

func newCandidateNegotiation(direct *assignedCipher, relay *relayCipher, cycle *probeCycle) (*candidateNegotiation, error) {
	if direct == nil || relay == nil || cycle == nil || direct.lease != relay.bind.lease {
		return nil, ErrConfiguration
	}
	n := &candidateNegotiation{direct: direct, relay: relay, cycle: cycle, seenPrepares: make(map[[32]byte]bool)}
	if err := n.refresh(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *candidateNegotiation) refresh() error {
	if err := n.cycle.check(); err != nil {
		return err
	}
	n.direct.bind.mu.Lock()
	a := n.direct.bind.assignment
	n.direct.bind.mu.Unlock()
	n.relay.bind.mu.Lock()
	rm := n.relay.bind.mapValue
	rlocal, rpeer := n.relay.bind.local, n.relay.bind.peer
	n.relay.bind.mu.Unlock()
	sample, err := n.cycle.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if a.mapValue.Cursor() != rm.Cursor() || a.plan.LocalIntentGeneration != n.cycle.generation {
		return ErrNoAuthority
	}
	if rlocal.ID != a.plan.LocalNodeID || rlocal.PublicKey != a.plan.LocalPublicKey || rpeer.ID != a.plan.PeerNodeID || rpeer.PublicKey != a.plan.PeerPublicKey {
		return ErrNoAuthority
	}
	if err := a.permission(sample.Wall, true); err != nil {
		return err
	}
	if n.history != nil && n.assignment.sameProbe(a) {
		return nil
	}
	if n.history != nil && !n.assignment.sameChannel(a) {
		return ErrNoAuthority
	}
	if n.probe != nil {
		n.probe.attempt.cancel()
	}
	if n.prepareBudget != nil {
		n.prepareBudget.cancel()
	}
	history, err := newDirectStability(n.direct, n.cycle)
	if err != nil {
		return err
	}
	// Only an actual semantic assignment change can start a new discovery
	// phase. Unrelated global revisions cannot keep a silent profile alive.
	n.discoveryBudget, err = n.cycle.attempt("discovery:" + a.mapValue.Cursor().PayloadHash)
	if err != nil {
		return err
	}
	n.assignment = a
	n.history = history
	n.initiator = a.plan.LocalNodeID < a.plan.PeerNodeID
	n.prepare = nil
	n.prepareBudget = nil
	n.remotePrepare = nil
	n.probe = nil
	n.recorded = false
	n.qualified = nil
	n.commit = nil
	n.pendingCommit = nil
	n.acknowledgedCommit = nil
	n.acknowledgement = nil
	n.activation = nil
	n.receiveGrant = nil
	n.receiveGrantTaken = false
	n.lastAction = connectivity.ClockSample{}
	n.lastProbeSend = connectivity.ClockSample{}
	n.trialStarted = connectivity.ClockSample{}
	return nil
}

func negotiationDue(now, previous connectivity.ClockSample, interval time.Duration) bool {
	return previous.Epoch == "" || now.Epoch == previous.Epoch && now.Uptime-previous.UpperUptime >= interval && now.Wall.Sub(previous.Wall) >= interval
}

func (n *candidateNegotiation) newControl(kind string) (transport.Control, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return transport.Control{}, ErrEngine
	}
	return n.relay.bind.newControl(kind, n.assignment.offer.Digest, n.cycle.id, nonce)
}

// The reserved Relay channel is already established. Account for its encoded
// negotiation frame with conservative WG transport/padding size; application
// frames on that same working Relay are never charged to the search budget.
func (n *candidateNegotiation) chargeControl(value transport.Control, incoming bool) error {
	raw, err := transport.MarshalControl(value)
	if err != nil {
		return err
	}
	encryptedSize := (len(raw) + 28 + 15) &^ 15
	encryptedSize += wgdevice.MessageTransportSize
	var message relaywire.Message = &relaywire.ClientFrame{Type: relaywire.MessageClientFrame, ProtocolVersion: relaywire.Version, PeerID: value.DestinationNodeID, PeerNetworkID: value.NetworkID, Payload: make([]byte, encryptedSize)}
	if incoming {
		message = &relaywire.ServerFrame{Type: relaywire.MessageServerFrame, ProtocolVersion: relaywire.Version, FromNodeID: value.SourceNodeID, FromNetworkID: value.NetworkID, Payload: make([]byte, encryptedSize)}
	}
	frame, err := relaywire.MarshalMessage(message)
	if err != nil {
		return err
	}
	return n.cycle.charge(len(frame))
}

func (n *candidateNegotiation) send(value transport.Control) error {
	if err := n.chargeControl(value, false); err != nil {
		return err
	}
	return n.relay.QueueControl(value)
}

func (n *candidateNegotiation) startTrial(now connectivity.ClockSample) error {
	value, err := n.newControl(transport.Prepare)
	if err != nil {
		return err
	}
	budget, err := n.cycle.attempt("prepare:" + hex.EncodeToString(value.Nonce))
	if err != nil {
		return err
	}
	if n.prepareBudget != nil {
		n.prepareBudget.cancel()
	}
	if n.probe != nil {
		n.probe.attempt.cancel()
	}
	n.prepare = &value
	n.prepareBudget = budget
	n.probe = nil
	n.recorded = false
	n.qualified = nil
	n.commit = nil
	n.receiveGrant = nil
	n.receiveGrantTaken = false
	n.trialStarted = now
	n.lastAction = now
	n.lastProbeSend = connectivity.ClockSample{}
	return n.send(value)
}

func (n *candidateNegotiation) collect() error {
	if n.probe == nil || n.recorded {
		return nil
	}
	select {
	case <-n.probe.done:
		if err := n.history.Record(n.probe); err != nil {
			return err
		}
		n.recorded = true
		if qualified, err := n.history.Qualify(); err == nil {
			n.qualified = qualified
		}
	default:
	}
	return nil
}

func (n *candidateNegotiation) Tick() error {
	if err := n.refresh(); err != nil {
		return err
	}
	if err := n.collect(); err != nil {
		return err
	}
	n.history.mu.Lock()
	unproven := len(n.history.seen) == 0
	n.history.mu.Unlock()
	if unproven && n.discoveryBudget != nil {
		if err := n.discoveryBudget.check(); err != nil {
			return err
		}
	}
	now, err := n.cycle.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	if n.acknowledgedCommit != nil {
		return nil
	}
	if n.qualified != nil {
		if err := n.qualified.check(n.direct); err != nil {
			n.qualified = nil
			n.commit = nil
		}
	}
	if n.qualified != nil {
		if !n.initiator {
			if n.pendingCommit != nil {
				return n.acknowledge()
			}
			return nil
		}
		if n.commit == nil {
			value, err := n.newControl(transport.Commit)
			if err != nil {
				return err
			}
			n.commit = &value
			n.lastAction = connectivity.ClockSample{}
		}
		if negotiationDue(now, n.lastAction, negotiationRetry) {
			n.lastAction = now
			if err := n.send(*n.commit); err != nil {
				return err
			}
			if n.receiveGrant == nil {
				n.receiveGrant = &directReceiveGrant{direct: n.direct, relay: n.relay, qualified: n.qualified, commit: *n.commit}
			}
			return nil
		}
		return nil
	}
	if n.initiator {
		if n.prepare == nil {
			return n.startTrial(now)
		}
		if n.probe == nil {
			if n.prepareBudget.check() != nil {
				return n.startTrial(now)
			}
			if negotiationDue(now, n.lastAction, negotiationRetry) {
				n.lastAction = now
				return n.send(*n.prepare)
			}
			return nil
		}
		if n.probe.attempt.check() != nil || n.recorded && negotiationDue(now, n.trialStarted, stableProbeSpacing) {
			return n.startTrial(now)
		}
	}
	if n.probe != nil && !n.recorded && n.probe.attempt.check() == nil && negotiationDue(now, n.lastProbeSend, negotiationRetry) {
		n.lastProbeSend = now
		if !n.probe.hasSent() {
			return n.direct.bind.resendHandshake(n.probe.attempt)
		}
		return n.probe.Send()
	}
	return nil
}

func sameControl(a, b transport.Control) bool {
	first, err := transport.MarshalControl(a)
	if err != nil {
		return false
	}
	second, err := transport.MarshalControl(b)
	return err == nil && bytes.Equal(first, second)
}

func (n *candidateNegotiation) Handle(value transport.Control) error {
	if err := n.refresh(); err != nil {
		return err
	}
	if err := n.relay.bind.controlAllowed(value, true); err != nil {
		return err
	}
	if value.CandidateID != n.cycle.id || value.ProfileDigest != n.assignment.offer.Digest {
		return ErrNoAuthority
	}
	if !n.direct.bind.revisionAllowed(value.NetworkRevision) {
		return ErrNoAuthority
	}
	value.Nonce = bytes.Clone(value.Nonce)
	if err := n.chargeControl(value, true); err != nil {
		return err
	}
	if err := n.collect(); err != nil {
		return err
	}
	switch value.Kind {
	case transport.Prepare:
		if n.initiator {
			return ErrNoAuthority
		}
		if n.remotePrepare != nil && sameControl(*n.remotePrepare, value) {
			if n.probe == nil || n.probe.attempt.check() != nil {
				return ErrNoAuthority
			}
			return n.send(replyControl(value, transport.Prepared))
		}
		nonce := [32]byte(value.Nonce)
		if n.seenPrepares[nonce] || len(n.seenPrepares) >= transport.MaxProbeBytes/32 {
			return ErrNoAuthority
		}
		probe, err := n.direct.BeginProbe(n.cycle, n.cycle.id)
		if err != nil {
			return err
		}
		// Publish the accepted prepare only after probe admission. A temporary
		// map fence or occupied slot must not turn its bounded retry into an
		// ACK replay for a probe that was never created.
		n.seenPrepares[nonce] = true
		n.remotePrepare = &value
		n.pendingCommit = nil
		n.acknowledgedCommit = nil
		n.acknowledgement = nil
		n.qualified = nil
		n.recorded = false
		n.probe = probe
		probe.mu.Lock()
		probe.waitForPeer = true
		probe.mu.Unlock()
		if err := n.send(replyControl(value, transport.Prepared)); err != nil {
			return err
		}
		now, err := n.cycle.clock.Sample()
		if err != nil {
			return connectivity.ErrClock
		}
		n.lastProbeSend = now
		return nil // The lower-ID peer starts crypto; our own nonce follows its data.
	case transport.Prepared:
		if !n.initiator || n.prepare == nil || n.prepareBudget == nil || n.prepareBudget.check() != nil || !transport.MatchesReply(*n.prepare, value) {
			return ErrNoAuthority
		}
		if n.probe != nil {
			return nil
		} // An ACK replay never renews a probe.
		probe, err := n.direct.BeginProbe(n.cycle, n.cycle.id)
		if err != nil {
			return err
		}
		n.probe = probe
		now, err := n.cycle.clock.Sample()
		if err != nil {
			return connectivity.ErrClock
		}
		n.lastProbeSend = now
		return probe.Send()
	case transport.Commit:
		if n.initiator || n.remotePrepare == nil {
			return ErrNoAuthority
		}
		if n.acknowledgedCommit != nil {
			if !sameControl(*n.acknowledgedCommit, value) {
				return ErrNoAuthority
			}
			return n.send(*n.acknowledgement)
		}
		if n.pendingCommit != nil && !sameControl(*n.pendingCommit, value) {
			return ErrNoAuthority
		}
		n.pendingCommit = &value
		if n.qualified == nil {
			return nil
		}
		return n.acknowledge()
	case transport.Committed:
		if !n.initiator || n.commit == nil || n.qualified == nil || !transport.MatchesReply(*n.commit, value) {
			return ErrNoAuthority
		}
		if err := n.qualified.check(n.direct); err != nil {
			return err
		}
		n.activation = &directActivation{direct: n.direct, relay: n.relay, qualified: n.qualified, commit: *n.commit, acknowledgement: value, received: value}
		n.acknowledgedCommit = n.commit
		n.acknowledgement = &value
		return nil
	default:
		return ErrNoAuthority
	}
}

func (n *candidateNegotiation) acknowledge() error {
	if n.pendingCommit == nil || n.qualified == nil {
		return ErrNoAuthority
	}
	if err := n.qualified.check(n.direct); err != nil {
		return err
	}
	ack := replyControl(*n.pendingCommit, transport.Committed)
	if err := n.send(ack); err != nil {
		return err
	}
	n.acknowledgedCommit = n.pendingCommit
	n.acknowledgement = &ack
	n.activation = &directActivation{direct: n.direct, relay: n.relay, qualified: n.qualified, commit: *n.pendingCommit, acknowledgement: ack, received: *n.pendingCommit}
	return nil
}

func (a *directActivation) check() error {
	if a == nil || a.direct == nil || a.relay == nil || !transport.MatchesReply(a.commit, a.acknowledgement) {
		return ErrNoAuthority
	}
	if err := a.qualified.check(a.direct); err != nil {
		return err
	}
	if err := a.relay.bind.controlAllowed(a.received, true); err != nil {
		return err
	}
	scope := a.qualified.stability.assignment
	if a.commit.CandidateID != a.qualified.stability.cycle.id || a.commit.ProfileDigest != scope.offer.Digest || !a.direct.bind.revisionAllowed(a.commit.NetworkRevision) {
		return ErrNoAuthority
	}
	localID, peerID := scope.plan.LocalNodeID, scope.plan.PeerNodeID
	localKey, peerKey := scope.plan.LocalPublicKey, scope.plan.PeerPublicKey
	localGeneration, peerGeneration := scope.plan.LocalIntentGeneration, scope.plan.PeerAdvertisement.IntentGeneration
	if localID > peerID {
		localID, peerID = peerID, localID
		localKey, peerKey = peerKey, localKey
		localGeneration, peerGeneration = peerGeneration, localGeneration
	}
	if a.commit.AccountID != scope.plan.AccountID || a.commit.NetworkID != scope.plan.NetworkID || a.commit.SourceNodeID != localID || a.commit.DestinationNodeID != peerID || a.commit.SourcePublicKey != localKey || a.commit.DestinationPublicKey != peerKey || a.commit.SourceGeneration != localGeneration || a.commit.DestinationGeneration != peerGeneration {
		return ErrNoAuthority
	}
	return nil
}

func (n *candidateNegotiation) TakeActivation() (*directActivation, error) {
	if n.activation == nil || n.activationTaken {
		return nil, ErrNoAuthority
	}
	if err := n.activation.check(); err != nil {
		return nil, err
	}
	n.activationTaken = true
	return n.activation, nil
}

func (g *directReceiveGrant) check() error {
	if g == nil || g.direct == nil || g.relay == nil || g.qualified == nil || g.commit.Kind != transport.Commit {
		return ErrNoAuthority
	}
	if err := g.qualified.check(g.direct); err != nil {
		return err
	}
	if err := g.relay.bind.controlAllowed(g.commit, false); err != nil {
		return err
	}
	a := g.qualified.stability.assignment
	if g.commit.SourceNodeID != a.plan.LocalNodeID || g.commit.DestinationNodeID != a.plan.PeerNodeID || g.commit.SourcePublicKey != a.plan.LocalPublicKey || g.commit.DestinationPublicKey != a.plan.PeerPublicKey || g.commit.SourceGeneration != a.plan.LocalIntentGeneration || g.commit.DestinationGeneration != a.plan.PeerAdvertisement.IntentGeneration || !g.direct.bind.revisionAllowed(g.commit.NetworkRevision) || g.commit.ProfileDigest != a.offer.Digest || g.commit.CandidateID != g.qualified.stability.cycle.id {
		return ErrNoAuthority
	}
	return nil
}

func (n *candidateNegotiation) TakeReceiveGrant() (*directReceiveGrant, error) {
	if n.receiveGrant == nil || n.receiveGrantTaken {
		return nil, ErrNoAuthority
	}
	if err := n.receiveGrant.check(); err != nil {
		return nil, err
	}
	n.receiveGrantTaken = true
	return n.receiveGrant, nil
}
