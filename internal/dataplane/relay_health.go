package dataplane

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
)

// relayEvidence is intentionally distinct from Direct pathEvidence. A healthy
// TLS socket, peer challenge, or Relay echo cannot qualify a Direct candidate.
type relayEvidence struct {
	At  connectivity.ClockSample
	RTT time.Duration
}
type relayHealthProbe struct {
	mu         sync.Mutex
	channel    *relayCipher
	attempt    *probeAttempt
	request    transport.Control
	sent       bool
	sentAt     connectivity.ClockSample
	lastSent   connectivity.ClockSample
	scopeEpoch uint64
	evidence   *relayEvidence
	done       chan struct{}
	permit     *probePermit
}

func (*relayHealthProbe) String() string   { return "[private Relay health challenge]" }
func (*relayHealthProbe) GoString() string { return "[private Relay health challenge]" }

func (b *relayPeerBind) newControl(kind, digest, candidateID string, nonce []byte) (transport.Control, error) {
	b.mu.Lock()
	local, peer, m, account := b.local, b.peer, b.mapValue, b.account
	b.mu.Unlock()
	if peer.Transport == nil {
		return transport.Control{}, ErrNoAuthority
	}
	value := transport.Control{Version: transport.Version, Kind: kind, AccountID: account, NetworkID: b.mux.transport.network, SourceNodeID: local.ID, DestinationNodeID: peer.ID, SourcePublicKey: local.PublicKey, DestinationPublicKey: peer.PublicKey, SourceGeneration: m.IntentGeneration(), DestinationGeneration: peer.Transport.PeerAdvertisement.IntentGeneration, NetworkRevision: m.Cursor().Revision.Network, ProfileDigest: digest, CandidateID: candidateID, Nonce: append([]byte(nil), nonce...)}
	if err := b.controlAllowed(value, false); err != nil {
		return transport.Control{}, err
	}
	return value, nil
}

func (c *relayCipher) BeginHealth(cycle *probeCycle) (*relayHealthProbe, error) {
	if cycle == nil {
		return nil, ErrConfiguration
	}
	if err := cycle.check(); err != nil {
		return nil, err
	}
	digest, err := transport.ProfileDigest(relayBaseline())
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrEngine
	}
	request, err := c.bind.newControl(transport.Challenge, digest, cycle.id, nonce)
	if err != nil {
		return nil, err
	}
	if cycle.generation != request.SourceGeneration {
		return nil, ErrNoAuthority
	}
	if _, err := transport.MarshalControl(request); err != nil {
		return nil, err
	}
	attempt, err := cycle.attempt(hex.EncodeToString(nonce))
	if err != nil {
		return nil, err
	}
	p := &relayHealthProbe{channel: c, attempt: attempt, request: request, done: make(chan struct{})}
	c.bind.mu.Lock()
	p.scopeEpoch = c.bind.scopeEpoch
	c.bind.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		attempt.cancel()
		return nil, ErrClosed
	}
	if c.health != nil {
		c.health.attempt.cancel()
	}
	if cycle.budget != nil {
		p.permit, err = cycle.budget.tryProbe(attempt)
		if err != nil {
			attempt.cancel()
			return nil, err
		}
	}
	c.health = p
	return p, nil
}

func (p *relayHealthProbe) admit() error {
	if p == nil {
		return ErrNoAuthority
	}
	p.channel.mu.Lock()
	current := !p.channel.closed && p.channel.health == p
	p.channel.mu.Unlock()
	if !current {
		return ErrNoAuthority
	}
	if !p.scopeCurrent() {
		return ErrNoAuthority
	}
	if err := p.attempt.check(); err != nil {
		return err
	}
	return p.channel.bind.controlAllowed(p.request, false)
}

func (p *relayHealthProbe) scopeCurrent() bool {
	p.channel.bind.mu.Lock()
	defer p.channel.bind.mu.Unlock()
	return !p.channel.bind.closed && p.scopeEpoch == p.channel.bind.scopeEpoch
}

func (p *relayHealthProbe) Send() error {
	if err := p.admit(); err != nil {
		return err
	}
	sample, err := p.attempt.cycle.clock.Sample()
	if err != nil {
		return connectivity.ErrClock
	}
	p.mu.Lock()
	if !p.sent {
		p.sentAt = sample
	}
	p.sent = true
	p.lastSent = sample
	p.mu.Unlock()
	if err := p.channel.QueueControl(p.request); err != nil {
		return err
	}
	return p.admit()
}

func (p *relayHealthProbe) resendDue(now connectivity.ClockSample) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent && negotiationDue(now, p.lastSent, negotiationRetry)
}

func (p *relayHealthProbe) receive(value transport.Control) error {
	if err := p.admit(); err != nil {
		return err
	}
	if !transport.MatchesReply(p.request, value) {
		return ErrNoAuthority
	}
	sample, err := p.attempt.cycle.clock.Sample()
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
		p.evidence = &relayEvidence{At: sample, RTT: sample.UpperUptime - p.sentAt.Uptime}
		close(p.done)
		p.permit.release()
	}
	return nil
}

func (p *relayHealthProbe) Evidence() (relayEvidence, error) {
	if err := p.admit(); err != nil {
		return relayEvidence{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.evidence == nil {
		return relayEvidence{}, ErrNoAuthority
	}
	return *p.evidence, nil
}

// Called exclusively by this Relay cipher's authenticated plaintext sink.
// Health controls are consumed here and never enter the Direct control sink.
func (c *relayCipher) receiveHealth(value transport.Control) bool {
	digest, err := transport.ProfileDigest(relayBaseline())
	if err != nil || value.ProfileDigest != digest {
		return false
	}
	if value.Kind == transport.Challenge {
		_ = c.QueueControl(replyControl(value, transport.Echo))
		return true
	}
	if value.Kind == transport.Echo {
		c.mu.Lock()
		probe := c.health
		c.mu.Unlock()
		if probe != nil {
			_ = probe.receive(value)
		}
	}
	return true
}
