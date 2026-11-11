package dataplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"maps"
	"math/big"
	"sort"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type directSearch struct {
	parent       *probeCycle
	child        *probeCycle
	candidate    *assignedCipher
	negotiation  *candidateNegotiation
	order        []string
	tried        map[string]bool
	seen         map[string]bool
	traffic      map[string]*probeWireBudget
	rx, finished bool
	hintEpoch    uint64
	hintBudget   *probeAttempt
	hintReady    bool
	hintContext  string
	hintSaved    bool
}

func randomCandidateID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", ErrEngine
	}
	return hex.EncodeToString(raw), nil
}

func directRetryDelay(failures uint32) time.Duration {
	base := 15 * time.Second
	for i := uint32(1); i < failures && base < 300*time.Second; i++ {
		base = min(300*time.Second, 2*base)
	}
	lower := max(15*time.Second, base*4/5)
	value, err := rand.Int(rand.Reader, big.NewInt(int64(base-lower)+1))
	if err != nil {
		return base
	}
	return lower + time.Duration(value.Int64())
}

func (g *transportSupervisor) offers(p *supervisedRelay) []coordinator.TransportProfileOffer {
	for _, peer := range g.snapshot.Peers {
		if peer.ID == p.scope.peerID && peer.Transport != nil {
			return peer.Transport.Profiles
		}
	}
	return nil
}

func (g *transportSupervisor) startSearch(p *supervisedRelay) error {
	if p.search != nil {
		return nil
	}
	if p.retryBlocked {
		return connectivity.ErrClock
	}
	if !g.lease.Permissions().NewFlows || p.channel == nil || !g.lease.MatchesMap(g.current) || len(g.offers(p)) == 0 {
		return ErrNoAuthority
	}
	g.refreshProfileSet(p)
	if p.cooldown != nil {
		if _, err := p.cooldown.Remaining(); !errors.Is(err, context.DeadlineExceeded) {
			return ErrNoAuthority
		}
	}
	if err := p.route.CanImprove(); err != nil {
		return err
	}
	if p.directCycle != nil {
		p.directCycle.cancel()
		p.directHealth, p.directCycle = nil, nil
	}
	id, err := randomCandidateID()
	if err != nil {
		return err
	}
	parent, err := g.budget.newCycle(g.ctx, g.clock, g.profile, g.generation, id)
	if err != nil {
		return err
	}
	s := &directSearch{parent: parent, tried: make(map[string]bool), seen: make(map[string]bool), traffic: make(map[string]*probeWireBudget)}
	s.order = g.profileOrder(p)
	if p.resumeProfile != "" {
		if p.spare != nil && len(s.order) > 0 {
			s.order = append([]string{s.order[0]}, preferProfile(s.order[1:], p.resumeProfile)...)
		} else {
			s.order = preferProfile(s.order, p.resumeProfile)
		}
	}
	p.resumeProfile = ""
	p.search, p.cooldown = s, nil
	if g.hints != nil {
		s.hintBudget, err = parent.attempt("hint-priority")
		if err != nil {
			parent.cancel()
			p.search = nil
			return err
		}
		s.hintEpoch = g.hints.request(g.current)
	}
	return nil
}

func (g *transportSupervisor) profileOrder(p *supervisedRelay) []string {
	offers := append([]coordinator.TransportProfileOffer(nil), g.offers(p)...)
	sort.Slice(offers, func(i, j int) bool {
		if offers[i].Profile.Engine != offers[j].Profile.Engine {
			return offers[i].Profile.Engine == transport.WireGuard
		}
		return offers[i].Profile.ID < offers[j].Profile.ID
	})
	var order []string
	if p.spare != nil {
		for _, offer := range offers {
			if offer.Profile.ID == p.spare.profileID {
				order = append(order, p.spare.profileID)
			}
		}
	}
	for _, offer := range offers {
		if p.spare == nil || offer.Profile.ID != p.spare.profileID {
			order = append(order, offer.Profile.ID)
		}
	}
	return order
}

func (g *transportSupervisor) refreshProfileSet(p *supervisedRelay) {
	next := make(map[string]string)
	for _, offer := range g.offers(p) {
		next[offer.Profile.ID] = offer.Digest
	}
	previous := p.profileSet
	p.profileSet = next
	if previous == nil || maps.Equal(previous, next) {
		return
	}
	p.cooldown, p.failures, p.resumeProfile = nil, 0, ""
	if s := p.search; s != nil && !s.finished {
		s.order = g.profileOrder(p)
		for id, digest := range next {
			if previous[id] != digest {
				delete(s.tried, id)
			}
		}
		// Keep parent, seen and traffic: replacing profiles cannot extend
		// the current search or replenish A when it returns after A->B->A.
	}
}

func (g *transportSupervisor) finishSearch(p *supervisedRelay, failed bool) {
	s := p.search
	if s == nil {
		return
	}
	if failed && s.candidate != nil {
		p.resumeProfile = s.candidate.profileID
		g.forgetCandidateHint(s, s.candidate)
	}
	if failed && s.candidate == nil {
		for _, id := range s.order {
			if !s.tried[id] {
				p.resumeProfile = id
				break
			}
		}
	}
	if s.candidate != nil && s.candidate != p.direct && s.candidate != p.spare {
		if s.rx {
			if p.direct == nil {
				if _, err := g.budget.promote(s.candidate.reservation); err == nil {
					p.direct = s.candidate
				} else {
					s.candidate.Close()
				}
			} else if p.spare == nil {
				p.spare = s.candidate
			} else {
				s.candidate.Close()
			}
		} else {
			s.candidate.Close()
		}
	}
	s.parent.cancel()
	p.search = nil
	if failed {
		p.failures = min(p.failures+1, uint32(6))
		if sample, err := g.clock.Sample(); err == nil {
			var err error
			p.cooldown, err = connectivity.NewRetryDelay(g.clock, g.profile, g.generation, sample, directRetryDelay(p.failures))
			p.retryBlocked = err != nil
		}
	}
}

func (g *transportSupervisor) releaseCandidate(p *supervisedRelay, failed bool) {
	s := p.search
	if s == nil {
		return
	}
	if s.child != nil {
		s.child.cancel()
	}
	if c := s.candidate; c != nil {
		if failed {
			g.forgetCandidateHint(s, c)
		}
		if failed && c == p.spare {
			p.spare = nil
			c.Close()
		}
		if c == p.direct {
			if failed {
				p.route.FailDirect(c)
			}
		} else if c != p.spare {
			c.Close()
		}
	}
	s.child, s.candidate, s.negotiation = nil, nil, nil
	s.rx, s.finished = false, false
}

func (g *transportSupervisor) prepareCandidate(p *supervisedRelay, offer coordinator.TransportProfileOffer, id string) (result error) {
	_, sourceSignal := g.lease.SourceSignal()
	defer func() {
		if result != nil {
			select {
			case <-sourceSignal:
				// Upstream may wrap a bind's authority fence as an engine
				// error. Even a pause/resume completed before we return is a
				// source transition, not evidence against this protocol.
				result = ErrNoAuthority
			default:
			}
		}
	}()
	s := p.search
	if s == nil || s.parent.check() != nil || s.seen[id] || len(s.seen) >= transport.MaxProbeBytes/32 {
		return ErrNoAuthority
	}
	if p.spare != nil && p.spare.profileID != offer.Profile.ID {
		return ErrTransportCapacity
	}
	child, err := s.parent.child(id)
	if err != nil {
		return err
	}
	if s.traffic == nil {
		s.traffic = make(map[string]*probeWireBudget)
	}
	if previous := s.traffic[offer.Profile.ID]; previous != nil {
		if previous.consumed() == transport.MaxProbeBytes {
			child.cancel()
			return ErrProbeBudget
		}
		child.wire = previous
	} else {
		s.traffic[offer.Profile.ID] = child.wire
	}
	var c *assignedCipher
	for _, existing := range []*assignedCipher{p.spare, p.direct} {
		if existing != nil && existing.profileID == offer.Profile.ID {
			c = existing
			break
		}
	}
	if c == nil {
		sink, err := g.packets.Sink(g.current, p.scope.peerID)
		if err != nil {
			child.cancel()
			return err
		}
		var factory func(uint16) (candidateSocket, error)
		if g.config.listenCandidate != nil {
			factory = func(port uint16) (candidateSocket, error) {
				return g.config.listenCandidate(p.scope.peerID, offer.Profile.ID, port)
			}
		}
		c, err = g.budget.newAssigned(g.ctx, g.current, g.lease, p.scope.peerID, offer.Profile.ID, g.private, g.config.mtu, g.clock, sink, factory)
		if err != nil {
			child.cancel()
			return err
		}
	}
	n, err := newCandidateNegotiation(c, p.channel, child)
	if err != nil {
		child.cancel()
		if c != p.direct && c != p.spare {
			c.Close()
		}
		return err
	}
	s.child, s.candidate, s.negotiation = child, c, n
	c.bind.mu.Lock()
	s.rx = c.bind.receiveConfirmed
	c.bind.mu.Unlock()
	if !g.listenersFit() {
		g.releaseCandidate(p, false)
		return ErrTransportCapacity
	}
	s.seen[id], s.tried[offer.Profile.ID] = true, true
	g.publishListeners()
	return nil
}

func (g *transportSupervisor) updateDirect(p *supervisedRelay, value connectivity.Map) bool {
	seen := make(map[*assignedCipher]bool)
	channels := []*assignedCipher{p.direct, p.spare}
	if p.search != nil {
		channels = append(channels, p.search.candidate)
	}
	for _, c := range channels {
		if c == nil || seen[c] {
			continue
		}
		seen[c] = true
		c.bind.mu.Lock()
		previous := c.bind.assignment
		c.bind.mu.Unlock()
		if c.Update(g.ctx, value) != nil {
			if !g.mapAvailable(value) {
				return false
			}
			p.route.FailDirect(c)
			if p.search != nil && p.search.candidate == c {
				g.releaseCandidate(p, false)
			}
			if c == p.direct {
				p.direct = nil
			}
			if c == p.spare {
				p.spare = nil
			}
			c.Close()
			continue
		}
		c.bind.mu.Lock()
		current := c.bind.assignment
		c.bind.mu.Unlock()
		if !previous.sameProbe(current) {
			if p.directHealth != nil && p.directHealth.channel == c {
				p.directCycle.cancel()
				p.directHealth, p.directCycle = nil, nil
				p.lastDirect = connectivity.ClockSample{}
			}
			if p.search != nil && p.search.candidate == c && p.search.finished {
				g.finishSearch(p, false)
			}
		}
	}
	return true
}

func (g *transportSupervisor) handlePeerControl(p *supervisedRelay, value transport.Control) {
	if p.channel == nil || p.channel.bind.controlAllowed(value, true) != nil {
		return
	}
	if value.Kind == transport.Prepare {
		if p.scope.localID < p.scope.peerID {
			return
		}
		p.route.mu.Lock()
		known := p.route.relayConfirmed
		p.route.mu.Unlock()
		if !known || g.startSearch(p) != nil {
			return
		}
		s := p.search
		if raw, err := transport.MarshalControl(value); err != nil || s.parent.charge(len(raw)) != nil {
			return
		}
		if s.negotiation == nil || s.child.id != value.CandidateID || s.candidate.profileID != profileForDigest(g.offers(p), value.ProfileDigest) {
			if s.seen[value.CandidateID] {
				return
			}
			var offer *coordinator.TransportProfileOffer
			for _, candidate := range g.offers(p) {
				if candidate.Digest == value.ProfileDigest {
					copy := candidate
					offer = &copy
					break
				}
			}
			if offer == nil {
				return
			}
			// The peer may already be in its next cycle while our original
			// parent is still active. A new CID gets fresh proofs, but shares
			// the profile's remaining bytes and our original parent deadline.
			// A new authenticated prepare abandons only an unqualified candidate.
			// A receive-qualified uncertainty is retained by finishSearch.
			if s.rx && s.candidate != p.direct && s.candidate != p.spare {
				return
			}
			if s.candidate != nil && !s.rx {
				g.forgetCandidateHint(s, s.candidate)
			}
			g.releaseCandidate(p, false)
			if g.prepareCandidate(p, *offer, value.CandidateID) != nil {
				return
			}
		}
	}
	if p.search == nil || p.search.negotiation == nil {
		return
	}
	_ = p.search.negotiation.Handle(value)
	g.collectNegotiation(p)
}

func profileForDigest(offers []coordinator.TransportProfileOffer, digest string) string {
	for _, offer := range offers {
		if offer.Digest == digest {
			return offer.Profile.ID
		}
	}
	return ""
}

func (g *transportSupervisor) collectNegotiation(p *supervisedRelay) {
	s := p.search
	if s == nil || s.negotiation == nil || s.finished {
		return
	}
	n := s.negotiation
	if grant, err := n.TakeReceiveGrant(); err == nil {
		if s.candidate.AllowReceive(grant) == nil {
			s.rx = true
		} else if !grant.used.Load() {
			n.receiveGrantTaken = false
		}
	}
	activation, err := n.TakeActivation()
	if err != nil {
		return
	}
	if _, err := p.route.SelectDirect(activation); err != nil {
		if !activation.used.Load() {
			n.activationTaken = false
		}
		return
	}
	c := s.candidate
	if _, err := g.budget.promote(c.reservation); err != nil {
		p.route.FailDirect(c)
		return
	}
	if !p.fallback && g.packets.SetRoute(p.scope.peerID, p.route) != nil {
		p.route.FailDirect(c)
		return
	}
	if p.direct != nil && p.direct != c {
		p.direct.Close()
	}
	if p.spare != nil && p.spare != c {
		p.spare.Close()
	}
	p.direct, p.spare, p.fallback = c, nil, true
	p.checkedDirect = activation.qualified.stability.last
	p.directHint = g.recordCandidateHint(s, c)
	p.failures, p.cooldown = 0, nil
	s.finished, s.rx = true, true
}

func negotiationHasProof(n *candidateNegotiation) bool {
	n.history.mu.Lock()
	defer n.history.mu.Unlock()
	return len(n.history.seen) != 0
}

func (g *transportSupervisor) tickSearch(p *supervisedRelay) {
	s := p.search
	if s == nil {
		return
	}
	if s.parent.check() != nil {
		g.finishSearch(p, !s.finished)
		return
	}
	if g.hints != nil && s.hintEpoch != 0 && !s.hintReady {
		if result, ready := g.hints.lookup(s.hintEpoch, g.current); ready {
			if errors.Is(result.err, ErrNoAuthority) && g.mapAvailable(g.current) {
				s.hintEpoch = g.hints.request(g.current)
			} else {
				s.hintReady = true
			}
			if result.err == nil {
				s.hintContext = result.context
				if s.candidate == nil && p.scope.localID < p.scope.peerID {
					if p.spare != nil && len(s.order) > 0 {
						s.order = append([]string{s.order[0]}, prioritizeHintProfiles(s.order[1:], result.preferred, p.scope.peerID)...)
					} else {
						s.order = prioritizeHintProfiles(s.order, result.preferred, p.scope.peerID)
					}
				}
			}
		}
		if !s.hintReady && s.candidate == nil && p.scope.localID < p.scope.peerID && s.hintBudget != nil && s.hintBudget.check() == nil {
			return
		}
	}
	if s.candidate == nil {
		if p.scope.localID > p.scope.peerID {
			return
		}
		for _, id := range s.order {
			if s.tried[id] {
				continue
			}
			for _, offer := range g.offers(p) {
				if offer.Profile.ID != id {
					continue
				}
				candidate, err := randomCandidateID()
				if err != nil {
					return
				}
				if err := g.prepareCandidate(p, offer, candidate); err != nil {
					// A map handoff can fence I/O while the pinned device is
					// opening. That is not a failed transport experiment and
					// must not exhaust this profile before its first probe.
					if errors.Is(err, ErrNoAuthority) || !g.lease.Permissions().NewFlows || !g.lease.MatchesMap(g.current) {
						return
					}
					if errors.Is(err, ErrTransportCapacity) {
						return
					}
					s.tried[id] = true
					continue
				}
				break
			}
			if s.candidate != nil {
				break
			}
			s.tried[id] = true
		}
		if s.candidate == nil {
			g.finishSearch(p, true)
			return
		}
	}
	if s.finished {
		if !s.hintSaved && p.direct == s.candidate && p.directHint == nil && p.route.Current() == peerRouteDirect {
			p.directHint = g.recordCandidateHint(s, s.candidate)
		}
		return
	}
	n := s.negotiation
	if !negotiationHasProof(n) {
		if n.probe != nil && n.probe.attempt.check() != nil || n.probe == nil && n.prepareBudget != nil && n.prepareBudget.check() != nil {
			g.releaseCandidate(p, true)
			return
		}
	}
	if err := n.Tick(); err != nil && !errors.Is(err, ErrTransportCapacity) && !errors.Is(err, ErrNoAuthority) {
		g.releaseCandidate(p, true)
		return
	}
	g.collectNegotiation(p)
}

func (g *transportSupervisor) tickDirect(now connectivity.ClockSample) {
	ids := make([]string, 0, len(g.peers))
	for id := range g.peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := g.peers[id]
		if !g.lease.Permissions().NewFlows || !g.lease.MatchesMap(g.current) {
			continue
		}
		g.healthDirect(p, now)
		if p.search == nil && p.channel != nil && p.route.Current() == peerRouteRelay && p.fallback {
			_ = g.startSearch(p)
		}
		g.tickSearch(p)
	}
}

func (g *transportSupervisor) healthDirect(p *supervisedRelay, now connectivity.ClockSample) {
	if p.directHealth != nil {
		if evidence, err := p.directHealth.Evidence(); err == nil {
			p.checkedDirect = evidence.At
			p.directCycle.cancel()
			p.directHealth, p.directCycle = nil, nil
			p.lastDirect = now
		} else if errors.Is(p.directHealth.attempt.check(), ErrProbeBudget) {
			// This was a locally suppressed check, not a failed network path.
			// Keep the last proof timestamp and selected application channel.
			p.directCycle.cancel()
			p.directHealth, p.directCycle = nil, nil
			p.lastDirect = now
		} else if p.directHealth.attempt.check() != nil {
			g.forgetSelectedHint(p)
			p.route.FailDirect(p.directHealth.channel)
			p.directCycle.cancel()
			p.directHealth, p.directCycle = nil, nil
			p.cooldown, p.failures = nil, 0
		}
	}
	if p.direct == nil || p.search != nil || p.route.Current() != peerRouteDirect || p.directHealth != nil || !negotiationDue(now, p.lastDirect, g.config.probeInterval) {
		return
	}
	if err := p.direct.serviceReady(); err != nil {
		p.lastDirect = now
		return
	}
	id, err := randomCandidateID()
	if err != nil {
		return
	}
	cycle, err := g.budget.newCycle(g.ctx, g.clock, g.profile, g.generation, id)
	if err != nil {
		return
	}
	probe, err := p.direct.BeginProbe(cycle, id)
	if err != nil {
		cycle.cancel()
		return
	}
	p.directHealth, p.directCycle = probe, cycle
	g.waiters.Add(1)
	go func() {
		defer g.waiters.Done()
		select {
		case <-probe.done:
		case <-probe.attempt.ctx.Done():
		case <-g.ctx.Done():
		}
		g.notify()
	}()
	if err := probe.Send(); err != nil {
		if !errors.Is(err, ErrProbeBudget) {
			g.forgetSelectedHint(p)
			p.route.FailDirect(p.direct)
		}
		p.lastDirect = now
		cycle.cancel()
		p.directHealth, p.directCycle = nil, nil
	}
}

func prioritizeHintProfiles(order []string, hints []state.TransportHintKey, peer string) []string {
	result := make([]string, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, h := range hints {
		if h.PeerNodeID != peer {
			continue
		}
		for _, id := range order {
			if id == h.TransportProfileID && !seen[id] {
				result = append(result, id)
				seen[id] = true
			}
		}
	}
	for _, id := range order {
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}

func preferProfile(order []string, profile string) []string {
	for i, id := range order {
		if id == profile {
			result := make([]string, 0, len(order))
			result = append(result, id)
			result = append(result, order[:i]...)
			return append(result, order[i+1:]...)
		}
	}
	return order
}

func (g *transportSupervisor) candidateHintKey(s *directSearch, c *assignedCipher) *state.TransportHintKey {
	if g.hints == nil || s == nil || !transport.ValidDigest(s.hintContext) {
		return nil
	}
	sample, err := g.clock.Sample()
	if err != nil {
		return nil
	}
	keys, err := hintKeys(g.current, g.profile, s.hintContext, sample.Wall)
	if err != nil {
		return nil
	}
	for _, key := range keys {
		if key.PeerNodeID == c.peerID && key.TransportProfileID == c.profileID {
			return &key
		}
	}
	return nil
}

func (g *transportSupervisor) recordCandidateHint(s *directSearch, c *assignedCipher) *state.TransportHintKey {
	if s.hintSaved {
		return nil
	}
	key := g.candidateHintKey(s, c)
	if key == nil || s.negotiation == nil {
		return nil
	}
	history := s.negotiation.history
	history.mu.Lock()
	success := state.TransportHint{Key: *key, SucceededAt: history.last.Wall, ExpiresAt: history.last.Wall.Add(state.TransportHintTTL), RTT: history.worstRTT, StableSpan: min(history.last.Uptime-history.first.UpperUptime, history.last.Wall.Sub(history.first.Wall)), SuccessfulProbes: uint32(len(history.seen))}
	history.mu.Unlock()
	g.hints.change(*key, &success)
	s.hintSaved = true
	return key
}

func (g *transportSupervisor) forgetCandidateHint(s *directSearch, c *assignedCipher) {
	if key := g.candidateHintKey(s, c); key != nil {
		g.hints.change(*key, nil)
	}
}

func (g *transportSupervisor) forgetSelectedHint(p *supervisedRelay) {
	if g.hints != nil && p.directHint != nil {
		g.hints.change(*p.directHint, nil)
		p.directHint = nil
	}
}
