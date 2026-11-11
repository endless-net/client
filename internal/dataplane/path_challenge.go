package dataplane

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/netip"
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

type pathEvidence struct {
	At  connectivity.ClockSample
	RTT time.Duration
}

// pathChallenge is private to one isolated Direct cipher sink, not Relay or an
// arbitrary status callback. Only our own unpredictable nonce can confirm it.
type pathChallenge struct {
	mu          sync.Mutex
	channel     *assignedCipher
	attempt     *probeAttempt
	assignment  candidateAssignment
	request     transport.Control
	sent        bool
	waitForPeer bool
	sentAt      connectivity.ClockSample
	evidence    *pathEvidence
	done        chan struct{}
	permit      *probePermit
}

func (*pathChallenge) String() string   { return "[private Direct challenge]" }
func (*pathChallenge) GoString() string { return "[private Direct challenge]" }

func (c *assignedCipher) BeginProbe(cycle *probeCycle, candidateID string) (*pathChallenge, error) {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	if cycle == nil || cycle.id != candidateID {
		return nil, ErrConfiguration
	}
	c.bind.mu.Lock()
	a := c.bind.assignment
	c.bind.mu.Unlock()
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrEngine
	}
	value := transport.Control{Version: transport.Version, Kind: transport.Challenge, AccountID: a.plan.AccountID, NetworkID: a.plan.NetworkID, SourceNodeID: a.plan.LocalNodeID, DestinationNodeID: a.plan.PeerNodeID, SourcePublicKey: a.plan.LocalPublicKey, DestinationPublicKey: a.plan.PeerPublicKey, SourceGeneration: a.plan.LocalIntentGeneration, DestinationGeneration: a.plan.PeerAdvertisement.IntentGeneration, NetworkRevision: a.plan.Revision.Network, ProfileDigest: a.offer.Digest, CandidateID: candidateID, Nonce: nonce}
	if _, err := transport.MarshalControl(value); err != nil {
		return nil, err
	}
	attempt, err := cycle.attempt(hex.EncodeToString(nonce))
	if err != nil {
		return nil, err
	}
	if c.probe != nil {
		c.probe.attempt.cancel()
	}
	var permit *probePermit
	if cycle.budget != nil {
		permit, err = cycle.budget.tryProbe(attempt)
		if err != nil {
			attempt.cancel()
			return nil, err
		}
	}
	if err := c.bind.beginProbe(attempt); err != nil {
		attempt.cancel()
		return nil, err
	}
	p := &pathChallenge{channel: c, attempt: attempt, assignment: a, request: value, done: make(chan struct{}), permit: permit}
	if c.probe != nil {
		c.probe.attempt.cancel()
	}
	c.probe = p
	return p, nil
}

func (p *pathChallenge) admit() error {
	if p == nil {
		return ErrNoAuthority
	}
	c := p.channel
	c.mu.Lock()
	current := !c.closed && c.probe == p
	c.mu.Unlock()
	if !current {
		return ErrNoAuthority
	}
	endpoint, err := netip.ParseAddrPort(p.assignment.offer.PeerEndpoint)
	if err != nil {
		return ErrNoAuthority
	}
	_, _, err = c.bind.admit(p.attempt, endpoint)
	return err
}

func (a candidateAssignment) addresses() (netip.Addr, netip.Addr, error) {
	local, err := netip.ParseAddr(a.local.AssignedIP)
	if err != nil || !local.Is4() || len(a.peer.AllowedIPs) != 1 {
		return netip.Addr{}, netip.Addr{}, ErrConfiguration
	}
	peer, err := netip.ParsePrefix(a.peer.AllowedIPs[0])
	if err != nil || peer.Bits() != 32 || !peer.Addr().Is4() {
		return netip.Addr{}, netip.Addr{}, ErrConfiguration
	}
	return local, peer.Addr(), nil
}

func (p *pathChallenge) Send() error {
	if err := p.admit(); err != nil {
		return err
	}
	if err := p.channel.bind.resendHandshake(p.attempt); err != nil {
		return err
	}
	source, destination, err := p.assignment.addresses()
	if err != nil {
		return err
	}
	raw, err := controlPacket(source, destination, p.request)
	if err != nil {
		return err
	}
	sample, err := p.channel.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	p.mu.Lock()
	if !p.sent {
		p.sentAt = sample
	}
	p.sent = true
	p.mu.Unlock()
	if err := p.channel.cipher.Queue(raw, true); err != nil {
		return err
	}
	return p.admit()
}

func (p *pathChallenge) hasSent() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.sent }

func (p *pathChallenge) Evidence() (pathEvidence, error) {
	if err := p.admit(); err != nil {
		return pathEvidence{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.evidence == nil {
		return pathEvidence{}, ErrNoAuthority
	}
	return *p.evidence, nil
}

// receive is called only by the assigned cipher's post-AEAD/replay/source sink.
// A matching reply delivered through the Relay engine never enters this sink.
func (p *pathChallenge) receive(value transport.Control) error {
	if err := p.admit(); err != nil {
		return err
	}
	if value.Kind == transport.Echo {
		if !transport.MatchesReply(p.request, value) {
			return ErrNoAuthority
		}
		sample, err := p.channel.clock.Sample()
		if err != nil {
			return connectivity.ErrClock
		}
		if err := p.admit(); err != nil {
			return err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.sent || sample.Epoch != p.sentAt.Epoch || sample.Uptime < p.sentAt.UpperUptime {
			return ErrNoAuthority
		}
		if p.evidence == nil {
			p.evidence = &pathEvidence{At: sample, RTT: sample.UpperUptime - p.sentAt.Uptime}
			close(p.done)
			p.permit.release()
		}
		return nil
	}
	if value.Kind != transport.Challenge {
		return ErrNoAuthority
	}
	// Build the expected opposite-direction challenge from the frozen assignment,
	// retaining the sender's nonce solely to echo it, never as our own evidence.
	expected := replyControl(p.request, transport.Echo)
	expected.Kind = transport.Challenge
	expected.Nonce = value.Nonce
	// Each side may have received a newer unrelated global revision. Its own
	// request keeps that revision; our exact Echo still echoes ours unchanged.
	if !p.channel.bind.revisionAllowed(value.NetworkRevision) {
		return ErrNoAuthority
	}
	expected.NetworkRevision = value.NetworkRevision
	encoded, err := transport.MarshalControl(expected)
	if err != nil {
		return err
	}
	actual, err := transport.MarshalControl(value)
	if err != nil || !bytes.Equal(encoded, actual) {
		return ErrNoAuthority
	}
	source, destination, err := p.assignment.addresses()
	if err != nil {
		return err
	}
	raw, err := controlPacket(source, destination, replyControl(value, transport.Echo))
	if err != nil {
		return err
	}
	if err := p.channel.cipher.Queue(raw, true); err != nil {
		return err
	}
	p.mu.Lock()
	startOwn := p.waitForPeer && !p.sent
	p.mu.Unlock()
	if startOwn {
		return p.Send()
	}
	return p.admit()
}

func replyControl(value transport.Control, kind string) transport.Control {
	value.Kind = kind
	value.SourceNodeID, value.DestinationNodeID = value.DestinationNodeID, value.SourceNodeID
	value.SourcePublicKey, value.DestinationPublicKey = value.DestinationPublicKey, value.SourcePublicKey
	value.SourceGeneration, value.DestinationGeneration = value.DestinationGeneration, value.SourceGeneration
	value.Nonce = bytes.Clone(value.Nonce)
	return value
}
