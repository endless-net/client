package dataplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type supervisorConfig struct {
	relay                RelayConfig
	probeInterval, retry time.Duration
	mtu, maxPeers        int
	advertise            func()
	listenCandidate      func(string, string, uint16) (candidateSocket, error)
	hintStore            *state.Store
	networkContext       func(context.Context, []byte) (string, error)
}

type supervisedRelay struct {
	channel                     *relayCipher
	route                       *peerRoute
	fallback                    bool
	health                      *relayHealthProbe
	cycle                       *probeCycle
	last                        connectivity.ClockSample
	scope                       packetBinding
	direct, spare               *assignedCipher
	search                      *directSearch
	directHealth                *pathChallenge
	directCycle                 *probeCycle
	lastDirect                  connectivity.ClockSample
	cooldown                    *connectivity.RetryDelay
	retryBlocked                bool
	failures                    uint32
	resumeProfile               string
	profileSet                  map[string]string
	checkedRelay, checkedDirect connectivity.ClockSample
	directHint                  *state.TransportHintKey
}

// transportSupervisor owns runtime transports, not the native adapter or WFP.
// Updates coalesce without resurrecting an old lease/map. One actor owns all
// ciphers; its public snapshot contains output choices, never credentials/keys.
type transportSupervisor struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	wake         chan struct{}
	waiters      sync.WaitGroup
	desired      connectivity.Map
	clock        connectivity.Clock
	lease        *connectivity.Lease
	profile      string
	generation   uint64
	private      []byte
	budget       *transportBudget
	packets      *packetSwitch
	primary      *Engine
	config       supervisorConfig
	mux          *relayMux
	peers        map[string]*supervisedRelay
	current      connectivity.Map
	applied      bool
	snapshot     coordinator.NetworkMapSnapshot
	lastDial     connectivity.ClockSample
	dial         *supervisorDial
	edge, cursor int
	peerCursor   int
	openRelay    func(context.Context, connectivity.Map, string) (*RelayTransport, error)
	listeners    []preparedListener
	status       []*clientv0.PeerTransportStatus
	statusCursor connectivity.Map
	hints        *transportHintRuntime
}

type relayOpener func(context.Context, connectivity.Map, string) (*RelayTransport, error)

type supervisorDial struct {
	cancel context.CancelFunc
	done   chan supervisorDialResult
	result *supervisorDialResult
}
type supervisorDialResult struct {
	transport *RelayTransport
	err       error
}

func (*transportSupervisor) String() string   { return "[private peer transport supervisor]" }
func (*transportSupervisor) GoString() string { return "[private peer transport supervisor]" }

func newTransportSupervisor(parent context.Context, value connectivity.Map, lease *connectivity.Lease, clock connectivity.Clock, profile string, generation uint64, private []byte, budget *transportBudget, packets *packetSwitch, primary *Engine, config supervisorConfig, open relayOpener) (*transportSupervisor, error) {
	if (config.hintStore == nil) != (config.networkContext == nil) {
		return nil, ErrConfiguration
	}
	if parent == nil || parent.Err() != nil || lease == nil || clock == nil || profile == "" || generation == 0 || generation != value.IntentGeneration() || len(private) != 32 || budget == nil || packets == nil || primary == nil || !lease.MatchesMap(value) || config.relay.Validate() != nil || config.probeInterval < time.Millisecond || config.probeInterval > 30*time.Second || config.retry < time.Millisecond || config.retry > 30*time.Second || config.mtu < 576 || config.mtu > relayMTUMax || config.maxPeers != budget.maxPeers {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	g := &transportSupervisor{ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), desired: value, clock: clock, lease: lease, profile: profile, generation: generation, private: append([]byte(nil), private...), budget: budget, packets: packets, primary: primary, config: config, peers: make(map[string]*supervisedRelay)}
	if open == nil {
		open = func(ctx context.Context, m connectivity.Map, edge string) (*RelayTransport, error) {
			t, err := NewRelayTransport(g.ctx, m, lease, edge, config.relay)
			if err != nil {
				return nil, err
			}
			if err := t.Open(ctx); err != nil {
				t.Close()
				return nil, err
			}
			return t, nil
		}
	}
	g.openRelay = open
	if config.hintStore != nil && config.networkContext != nil {
		g.hints = newTransportHintRuntime(ctx, config.hintStore, clock, lease, profile, generation, g.private, config.networkContext, g.notify)
	}
	go g.run()
	return g, nil
}

func (g *transportSupervisor) notify() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

func (g *transportSupervisor) Update(value connectivity.Map) {
	g.mu.Lock()
	g.desired = value
	g.mu.Unlock()
	g.notify()
}

func (g *transportSupervisor) Close() { g.cancel(); <-g.done }

func (g *transportSupervisor) outputChoices() map[string]peerRouteKind {
	return g.packets.outputChoices()
}

func (g *transportSupervisor) retire(id string) {
	p := g.peers[id]
	if p == nil {
		return
	}
	if p.cycle != nil {
		p.cycle.cancel()
	}
	if p.channel != nil {
		p.route.FailRelay(p.channel)
		p.channel.Close()
	}
	p.channel, p.health, p.cycle = nil, nil, nil
	g.finishSearch(p, false)
}

func (g *transportSupervisor) dropPeer(id string) {
	p := g.peers[id]
	if p == nil {
		return
	}
	g.retire(id)
	delete(g.peers, id)
	if p.directCycle != nil {
		p.directCycle.cancel()
	}
	if p.direct != nil {
		p.direct.Close()
	}
	if p.spare != nil && p.spare != p.direct {
		p.spare.Close()
	}
	p.route.Invalidate()
}

func (g *transportSupervisor) applyDesired() {
	g.mu.Lock()
	value := g.desired
	g.mu.Unlock()
	if g.applied && value.Cursor() == g.current.Cursor() || !g.mapAvailable(value) {
		return
	}
	snapshot, err := value.Snapshot()
	if err != nil || value.Denied() {
		return
	}
	g.current, g.snapshot = value, snapshot
	if g.hints != nil {
		g.hints.request(value)
	}
	g.applied = false
	if g.mux != nil && g.mux.Update(value) != nil {
		if !g.mapAvailable(value) {
			return
		}
		g.mux.Close()
		g.mux = nil
	}
	for id, p := range g.peers {
		g.packets.mu.Lock()
		scope, exists := g.packets.pairs[id]
		g.packets.mu.Unlock()
		if !exists || scope != p.scope {
			g.dropPeer(id)
			continue
		}
		if !g.updateDirect(p, value) {
			return
		}
		if p.channel != nil && (p.channel.bind.update(value) != nil || g.mux == nil) {
			if !g.mapAvailable(value) {
				return
			}
			g.retire(id)
			continue
		}
		if p.health != nil && !p.health.scopeCurrent() {
			p.cycle.cancel()
			p.health, p.cycle = nil, nil
			p.last = connectivity.ClockSample{}
		}
		g.refreshProfileSet(p)
	}
	g.applied = g.mapAvailable(value)
}

func (g *transportSupervisor) mapAvailable(value connectivity.Map) bool {
	return g.lease.MatchesMap(value) && g.lease.Permissions().NewFlows
}

func (g *transportSupervisor) ensureMux(now connectivity.ClockSample) {
	if g.dial != nil {
		if g.dial.result == nil {
			select {
			case result := <-g.dial.done:
				g.dial.cancel()
				g.dial.result = &result
			default:
				return
			}
		}
		result := g.dial.result
		if t := result.transport; t != nil {
			if result.err == nil && t.connected() && !t.authorityGone() {
				// A dial result is one bounded owned socket, not a path proof.
				// Retain it fenced until the latest signed map has been applied.
				if !g.mapAvailable(g.current) {
					return
				}
				if t.Update(g.current) == nil {
					mux, err := newRelayMux(g.ctx, t, g.config.maxPeers)
					if err == nil {
						g.mux = mux
					} else if !g.mapAvailable(g.current) {
						return
					}
				} else if !g.mapAvailable(g.current) {
					return
				}
			}
			if g.mux == nil {
				t.Close()
			}
		}
		g.dial = nil
	}
	if g.mux != nil && g.mux.transport.connected() && !g.mux.transport.authorityGone() {
		return
	}
	if g.mux != nil {
		g.mux.Close()
		g.mux = nil
	}
	for id := range g.peers {
		g.retire(id)
	}
	if !g.lease.Permissions().NewFlows || !g.lease.MatchesMap(g.current) || g.snapshot.RelayCredential == nil || len(g.snapshot.Relays) == 0 || !negotiationDue(now, g.lastDial, g.config.retry) {
		return
	}
	g.lastDial = now
	edge := g.snapshot.Relays[g.edge%len(g.snapshot.Relays)]
	g.edge++
	ctx, cancel := context.WithTimeout(g.ctx, g.config.relay.DialTimeout)
	op := &supervisorDial{cancel: cancel, done: make(chan supervisorDialResult, 1)}
	g.dial = op
	value := g.current
	g.waiters.Add(1)
	go func() {
		defer g.waiters.Done()
		t, err := g.openRelay(ctx, value, edge.ID)
		op.done <- supervisorDialResult{t, err}
		g.notify()
	}()
}

func (g *transportSupervisor) ensurePeers() {
	if g.mux == nil || !g.lease.Permissions().NewFlows || !g.lease.MatchesMap(g.current) || g.snapshot.Node.Transport == nil || g.snapshot.Node.Transport.IntentGeneration != g.generation {
		return
	}
	created := 0
	if len(g.snapshot.Peers) == 0 {
		return
	}
	start := g.peerCursor % len(g.snapshot.Peers)
	for i := range g.snapshot.Peers {
		index := (start + i) % len(g.snapshot.Peers)
		peer := g.snapshot.Peers[index]
		// Construct only the next two probe candidates before scheduling own
		// health. A large peer set must not postpone the first usable Relay
		// behind construction of every other peer's cryptographic device.
		if created >= 2 {
			break
		}
		if g.peers[peer.ID] != nil && g.peers[peer.ID].channel != nil || peer.Transport == nil || peer.Transport.LocalIntentGeneration != g.generation || !pairGranted(peer) {
			continue
		}
		sink, err := g.packets.Sink(g.current, peer.ID)
		if err != nil {
			continue
		}
		c, err := g.budget.newRelay(g.ctx, g.mux, g.current, g.lease, peer.ID, g.private, g.config.mtu, sink)
		if err != nil {
			continue
		}
		p := g.peers[peer.ID]
		if p == nil {
			route, err := newPeerRoute(c, g.clock, g.profile, g.generation)
			if err != nil {
				c.Close()
				continue
			}
			g.packets.mu.Lock()
			scope := g.packets.pairs[peer.ID]
			g.packets.mu.Unlock()
			p = &supervisedRelay{route: route, scope: scope}
			g.peers[peer.ID] = p
		} else if p.route.ReplaceRelay(c) != nil {
			c.Close()
			g.dropPeer(peer.ID)
			continue
		}
		p.channel = c
		created++
		g.peerCursor = index + 1
	}
}

func (g *transportSupervisor) watchHealth(p *relayHealthProbe) {
	g.waiters.Add(1)
	go func() {
		defer g.waiters.Done()
		select {
		case <-p.done:
		case <-p.attempt.ctx.Done():
		case <-g.ctx.Done():
		}
		g.notify()
	}()
}

func (g *transportSupervisor) collectHealth(now connectivity.ClockSample, legacy map[string]bool) {
	if !g.mapAvailable(g.current) {
		return
	}
	overrides := g.packets.outputChoices()
	for id, p := range g.peers {
		if p.channel == nil {
			continue
		}
		if p.health != nil {
			completed := false
			select {
			case <-p.health.done:
				completed = true
			default:
			}
			if completed {
				if p.route.ConfirmRelay(p.health) != nil {
					if !g.mapAvailable(g.current) {
						return
					}
					p.route.FailRelay(p.channel)
				} else {
					p.checkedRelay = now
				}
				p.last = now
				p.cycle.cancel()
				p.health, p.cycle = nil, nil
			} else if p.health.attempt.check() != nil {
				p.route.mu.Lock()
				known := p.route.relayConfirmed
				p.route.mu.Unlock()
				if g.lease.Permissions().NewFlows && (!known || p.channel.sessionLost.Load()) {
					// Stop unconfirmed WG handshake retries with the actual
					// cipher, before another peer acquires this probe slot.
					g.retire(id)
					continue
				}
				p.cycle.cancel()
				p.health, p.cycle = nil, nil
				p.last = now
			} else if p.health.resendDue(now) {
				// Reuse the original nonce/deadline if an unrelated map handoff
				// dropped a packet. This does not extend the three-second probe.
				_ = p.health.Send()
			}
		}
		_, alreadyOverridden := overrides[id]
		if !p.fallback && (!legacy[id] || alreadyOverridden) && p.route.Current() == peerRouteRelay {
			if g.packets.SetRoute(id, p.route) == nil {
				p.fallback = true
			}
		}
		channel := p.channel
	drain:
		for range portQueueFrames {
			select {
			case event, ok := <-channel.Events():
				if !ok {
					g.retire(id)
					break drain
				}
				g.handlePeerControl(p, event)
			default:
			}
		}
	}
}

func (g *transportSupervisor) startHealth(now connectivity.ClockSample) {
	if !g.mapAvailable(g.current) {
		return // offline retains only already-authorized app flows
	}
	ids := make([]string, 0, len(g.peers))
	for id := range g.peers {
		if g.peers[id].channel != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return
	}
	start := g.cursor % len(ids)
	for i := range ids {
		index := (start + i) % len(ids)
		p := g.peers[ids[index]]
		p.route.mu.Lock()
		known := p.route.relayConfirmed
		p.route.mu.Unlock()
		if known && p.search != nil {
			continue
		}
		// Give the lower-ID peer the first cryptographic initiation. Both
		// peers still need their own Echo proof, but simultaneous first WG
		// initiations must not force a five-second engine retry outside our
		// original three-second probe. The other peer can respond before it
		// starts its own probe, then test the established encrypted session.
		if p.scope.localID > p.scope.peerID && (!p.channel.hadSession.Load() || p.channel.sessionLost.Load()) {
			continue
		}
		if p.health != nil || !negotiationDue(now, p.last, g.config.probeInterval) {
			continue
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return
		}
		cycle, err := g.budget.newCycle(g.ctx, g.clock, g.profile, g.generation, hex.EncodeToString(id))
		if err != nil {
			return
		}
		probe, err := p.channel.BeginHealth(cycle)
		if err != nil {
			cycle.cancel()
			continue
		}
		p.health, p.cycle = probe, cycle
		g.watchHealth(probe)
		if probe.Send() != nil {
			if g.mapAvailable(g.current) {
				g.retire(ids[index])
			}
		}
		g.cursor = index + 1
	}
}

func (g *transportSupervisor) step() {
	g.applyDesired()
	now, err := g.clock.Sample()
	if err != nil {
		return
	}
	g.ensureMux(now)
	g.ensurePeers()
	legacy := make(map[string]bool)
	paths, _, _ := g.primary.direct.snapshot()
	for _, p := range paths {
		legacy[p.PeerID] = p.Authenticated
	}
	g.collectHealth(now, legacy)
	g.tickDirect(now)
	g.startHealth(now)
	g.publishListeners()
	g.publishTransportStatus(now)
}

func (g *transportSupervisor) run() {
	defer close(g.done)
	defer func() {
		for id := range g.peers {
			g.dropPeer(id)
		}
		if g.mux != nil {
			g.mux.Close()
		}
		if g.dial != nil {
			g.dial.cancel()
		}
		g.waiters.Wait()
		if g.hints != nil {
			<-g.hints.done
		}
		if g.dial != nil {
			result := g.dial.result
			if result == nil {
				completed := <-g.dial.done
				result = &completed
			}
			if result.transport != nil {
				result.transport.Close()
			}
		}
		clear(g.private)
	}()
	tick := time.NewTicker(min(g.config.probeInterval, negotiationRetry))
	defer tick.Stop()
	for g.ctx.Err() == nil {
		g.step()
		select {
		case <-g.ctx.Done():
			return
		case <-g.wake:
		case <-tick.C:
		}
	}
}
