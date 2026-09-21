package client

import (
	"context"
	"crypto/sha256"
	"reflect"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/device"
)

// A receipt of authenticated transport to the selected exit peer, not proof of
// Internet forwarding. It can limit a future LAN kernel lease; it cannot open
// LAN by itself. Deadlines derive from evidence, never from repeated reads.
type exitLANPeerHealth struct {
	engine        *WireGuardEngine
	device        *device.Device
	configuration [32]byte
	uapi          [32]byte
	paths         [32]byte
	pathManager   *wireGuardRelayPathManager
	selection     ClientExitSelection
	observedAt    time.Time
	handshake     time.Time
	expires       time.Time
	relayBridge   *wireGuardRelayBridge
	relay         *wireGuardRelayPeerObservation
}

// Caller holds engine.mu. Production inspection is fixed to authenticated live
// UAPI readback; the private seam only supplies deterministic unit timestamps.
func (e *WireGuardEngine) observeExitLANPeerHealthLocked(ctx context.Context, cfg Config, selection *ClientExitSelection, now time.Time) (*exitLANPeerHealth, error) {
	return e.observeExitLANPeerHealthWithInspection(ctx, cfg, selection, now, resourceObservedUAPI)
}

func (e *WireGuardEngine) observeExitLANPeerHealthWithInspection(ctx context.Context, cfg Config, selection *ClientExitSelection, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) (*exitLANPeerHealth, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || inspect == nil || selection == nil || cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || e.pathMap.MapSignature == nil || !e.configured || e.runtimeSuspended || e.device == nil || e.relayPaths == nil || !exitSelectionConnectionReady(cfg) || !reflect.DeepEqual(cfg.ExitSelection, selection) || !reflect.DeepEqual(e.exitSelection, selection) || !sameExitControlIdentity(cfg, e.exitConfig) || e.runtimeIdentity != nativeExitAppliedIdentity(cfg, *cfg.CachedMap, e.interface_) || cfg.CachedMap.MapSignature.PayloadHash != e.pathMap.MapSignature.PayloadHash || !resourceHostExitFilterCurrent(e, cfg, now) {
		return nil, errExitLANPolicy
	}
	policy, err := compileExitPacketPolicy(cfg, *cfg.CachedMap, selection, now)
	if err != nil {
		return nil, errExitLANPolicy
	}
	var peer api.Peer
	count := 0
	for _, candidate := range cfg.CachedMap.Peers {
		if candidate.ID == selection.Host.NodeID && candidate.PublicKey == selection.Host.PublicKey {
			peer = candidate
			count++
		}
	}
	if count != 1 {
		return nil, errExitLANPolicy
	}
	inspection, err := inspect(e)
	if err != nil {
		return nil, errExitLANPolicy
	}
	live, found := wireGuardPeerForMapPeer(inspection, peer)
	if !found {
		return nil, errExitLANPolicy
	}
	handshake, complete := live.authenticatedHandshakeTime()
	if !complete || handshake.After(now) || !now.Before(handshake.Add(device.RejectAfterTime)) {
		return nil, errExitLANPolicy
	}
	paths := e.relayPaths.Statuses()
	var selected PeerPathStatus
	count = 0
	for _, path := range paths {
		if path.PeerID == peer.ID {
			selected = path
			count++
		}
	}
	if count != 1 {
		return nil, errExitLANPolicy
	}
	proof := &exitLANPeerHealth{engine: e, device: e.device, configuration: resourceObservationConfig(cfg), uapi: sha256.Sum256([]byte(e.uapi)), paths: resourceObservationPaths(e), pathManager: e.relayPaths, selection: *selection, observedAt: now, handshake: handshake, expires: handshake.Add(device.RejectAfterTime)}
	if resourceHostPathObserved(paths, peer.ID, live, now) {
		// The matching reachable path candidate has its own freshness limit.
		// Multiple matching observations can supply only the latest real check,
		// never a new deadline synthesized from this read's clock.
		var checked time.Time
		for _, candidate := range append([]PathCandidateStatus{selected.Direct}, selected.Candidates...) {
			at, err := time.Parse(time.RFC3339Nano, candidate.CheckedAt)
			if err == nil && candidate.Endpoint == live.Endpoint && candidate.State == "reachable" && !at.After(now) && now.Before(at.Add(device.RejectAfterTime)) && at.After(checked) {
				checked = at
			}
		}
		if checked.IsZero() {
			return nil, errExitLANPolicy
		}
		proof.expires = earliestExitLANDeadline(proof.expires, checked.Add(device.RejectAfterTime))
	} else {
		if e.relayBridge == nil {
			return nil, errExitLANPolicy
		}
		relay, ok := e.relayBridge.ObservePeer(*cfg.CachedMap, peer.ID)
		if !ok || !resourceHostRelayPathObserved(paths, peer, live, relay, now) {
			return nil, errExitLANPolicy
		}
		proof.relayBridge, proof.relay = e.relayBridge, &relay
	}
	proof.expires = earliestExitLANDeadline(proof.expires, policy.mapExpires, policy.grantExpires)
	if !now.Add(time.Since(started)).Before(proof.expires) {
		return nil, errExitLANPolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return proof, nil
}

func earliestExitLANDeadline(first time.Time, deadlines ...time.Time) time.Time {
	for _, deadline := range deadlines {
		if deadline.Before(first) {
			first = deadline
		}
	}
	return first
}

// A new observation may confirm continuing transport, but cannot extend this
// receipt's original absolute deadline. Caller holds the same engine.mu.
func (p *exitLANPeerHealth) currentLocked(ctx context.Context, cfg Config, now time.Time) bool {
	return p.currentWithInspection(ctx, cfg, now, resourceObservedUAPI)
}

func (p *exitLANPeerHealth) currentWithInspection(ctx context.Context, cfg Config, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) bool {
	started := time.Now()
	if p == nil || p.engine == nil || now.Before(p.observedAt) || !now.Before(p.expires) || resourceObservationConfig(cfg) != p.configuration {
		return false
	}
	e := p.engine
	if e.device != p.device || e.relayPaths != p.pathManager || sha256.Sum256([]byte(e.uapi)) != p.uapi || resourceObservationPaths(e) != p.paths {
		return false
	}
	if p.relay != nil && (e.relayBridge != p.relayBridge || !p.relayBridge.ObservationCurrent(*p.relay)) {
		return false
	}
	current, err := e.observeExitLANPeerHealthWithInspection(ctx, cfg, &p.selection, now, inspect)
	if err != nil || current.handshake.Before(p.handshake) {
		return false
	}
	relayCurrent := p.relay == nil || (e.relayBridge == p.relayBridge && p.relayBridge.ObservationCurrent(*p.relay))
	return relayCurrent && ctx.Err() == nil && now.Add(time.Since(started)).Before(p.expires)
}

// Preparation still needs independent native route/firewall readback and a
// topology lifetime guard before any kernel exception can be installed.
func (e *WireGuardEngine) prepareExitLANWithHealthLocked(ctx context.Context, cfg Config, topology *exitLANSource, now time.Time) (*exitLANPlan, *exitLANPeerHealth, error) {
	return e.prepareExitLANWithInspection(ctx, cfg, topology, now, resourceObservedUAPI)
}

func (e *WireGuardEngine) prepareExitLANWithInspection(ctx context.Context, cfg Config, topology *exitLANSource, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) (*exitLANPlan, *exitLANPeerHealth, error) {
	started := time.Now()
	if e == nil || topology == nil || topology.OwnInterface != e.interface_ {
		return nil, nil, errExitLANPolicy
	}
	health, err := e.observeExitLANPeerHealthWithInspection(ctx, cfg, cfg.ExitSelection, now, inspect)
	if err != nil {
		return nil, nil, err
	}
	retained, err := e.retainedExitLANDestinationsLocked()
	if err != nil {
		return nil, nil, err
	}
	plan, err := compileExitLANPlan(cfg, *cfg.CachedMap, cfg.ExitSelection, topology, retained, now)
	if err != nil {
		return nil, nil, err
	}
	plan.expires = earliestExitLANDeadline(plan.expires, health.expires)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	finished := now.Add(time.Since(started))
	if !finished.Before(plan.expires) || !health.currentWithInspection(ctx, cfg, finished, inspect) {
		return nil, nil, errExitLANPolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !now.Add(time.Since(started)).Before(plan.expires) {
		return nil, nil, errExitLANPolicy
	}
	return plan, health, nil
}
