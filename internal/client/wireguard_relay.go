package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

type wireGuardRelayBridge struct {
	mu        sync.Mutex
	timeout   time.Duration
	tlsConfig *tls.Config
	key       string
	status    RelayDataplaneBridgeStatus
	statusOK  bool
	lastErr   error
	cancel    context.CancelFunc
	done      chan error
}

func newWireGuardRelayBridge(timeout time.Duration, tlsConfig *tls.Config) *wireGuardRelayBridge {
	if timeout <= 0 {
		timeout = DefaultRelayDialTimeout
	}
	return &wireGuardRelayBridge{timeout: timeout, tlsConfig: tlsConfig}
}

func (b *wireGuardRelayBridge) Ensure(ctx context.Context, networkMap clientapi.RegisterNodeResponse, wireGuardListenAddr string) error {
	if b == nil {
		return nil
	}
	if len(networkMap.Peers) == 0 || len(networkMap.Relays) == 0 || networkMap.RelayCredential == nil {
		b.Stop()
		return nil
	}
	if strings.TrimSpace(wireGuardListenAddr) == "" {
		return errors.New("wireguard-go relay bridge requires a live UDP endpoint")
	}
	key := wireGuardRelayBridgeKey(networkMap, wireGuardListenAddr)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reapLocked()
	if b.cancel != nil && b.key == key {
		return nil
	}
	b.stopLocked()
	// The bridge belongs to the wireguard-go engine, not to a single configure
	// request. In particular, an IPC request context ends as soon as Connect
	// returns while the tunnel must remain active until Down or Close.
	bridgeCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan RelayDataplaneBridgeStatus, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunRelayDataplaneBridge(bridgeCtx, RelayDataplaneBridgeOptions{
			NetworkMap:          networkMap,
			WireGuardListenAddr: wireGuardListenAddr,
			Timeout:             b.timeout,
			TLSConfig:           b.tlsConfig,
			Ready: func(status RelayDataplaneBridgeStatus) {
				select {
				case ready <- status:
				default:
				}
			},
		})
	}()
	startTimeout := b.timeout + time.Second
	if startTimeout <= time.Second {
		startTimeout = 3 * time.Second
	}
	timer := time.NewTimer(startTimeout)
	defer timer.Stop()
	select {
	case status := <-ready:
		b.key = key
		b.status = status
		b.statusOK = true
		b.lastErr = nil
		b.cancel = cancel
		b.done = done
		return nil
	case err := <-done:
		cancel()
		if err == nil {
			err = errors.New("relay dataplane bridge stopped before ready")
		}
		b.lastErr = err
		return err
	case <-timer.C:
		cancel()
		b.lastErr = fmt.Errorf("relay dataplane bridge did not become ready within %s", startTimeout)
		return b.lastErr
	case <-ctx.Done():
		cancel()
		b.lastErr = ctx.Err()
		return b.lastErr
	}
}

func (b *wireGuardRelayBridge) Stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
	b.lastErr = nil
}

func (b *wireGuardRelayBridge) Status() (RelayDataplaneBridgeStatus, bool, error) {
	if b == nil {
		return RelayDataplaneBridgeStatus{}, false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reapLocked()
	if b.cancel == nil || !b.statusOK {
		return RelayDataplaneBridgeStatus{}, false, b.lastErr
	}
	return b.status, true, nil
}

func (b *wireGuardRelayBridge) reapLocked() {
	if b.done == nil {
		return
	}
	select {
	case err := <-b.done:
		if err != nil {
			log.Printf("wireguard-go relay bridge stopped: %v", err)
		}
		b.cancel = nil
		b.done = nil
		b.key = ""
		b.status = RelayDataplaneBridgeStatus{}
		b.statusOK = false
		b.lastErr = err
	default:
	}
}

func (b *wireGuardRelayBridge) stopLocked() {
	if b.cancel == nil {
		return
	}
	cancel := b.cancel
	done := b.done
	b.cancel = nil
	b.done = nil
	b.key = ""
	b.status = RelayDataplaneBridgeStatus{}
	b.statusOK = false
	cancel()
	if done == nil {
		return
	}
	select {
	case err := <-done:
		if err != nil {
			log.Printf("wireguard-go relay bridge stopped: %v", err)
		}
	case <-time.After(2 * time.Second):
		log.Printf("wireguard-go relay bridge stop timed out")
	}
}

func wireGuardRelayBridgeKey(networkMap clientapi.RegisterNodeResponse, wireGuardListenAddr string) string {
	parts := []string{
		strings.TrimSpace(networkMap.Network.ID),
		strconv.FormatUint(networkMap.Network.Revision, 10),
		strings.TrimSpace(networkMap.Node.ID),
		strings.TrimSpace(wireGuardListenAddr),
	}
	for _, relay := range networkMap.Relays {
		parts = append(parts, strings.TrimSpace(relay.ID), strings.TrimSpace(relay.Addr), strings.TrimSpace(relay.Protocol))
	}
	for _, peer := range networkMap.Peers {
		parts = append(parts, strings.TrimSpace(peer.ID), strings.TrimSpace(peer.Hostname))
	}
	return strings.Join(parts, "\x00")
}

type wireGuardRelayPathManager struct {
	peers    map[string]*wireGuardPeerPathSelection
	statuses []PeerPathStatus
}

type wireGuardPeerPathSelection struct {
	selectedPath     string
	selectedEndpoint string
	lastTransition   time.Time
	reason           string
	candidates       map[string]*wireGuardDirectCandidateHealth
}

type wireGuardDirectCandidateHealth struct {
	reachable           bool
	rtt                 time.Duration
	checkedAt           time.Time
	lastReachableAt     time.Time
	consecutiveFailures int
	lastError           string
}

const wireGuardDirectFailureThreshold = 2

func newWireGuardRelayPathManager(_ time.Duration) *wireGuardRelayPathManager {
	return &wireGuardRelayPathManager{
		peers: map[string]*wireGuardPeerPathSelection{},
	}
}

// Bootstrap keeps the data plane on relay while authenticated direct probes
// run beside it. Networks without a usable relay retain the best signed direct
// endpoint so relay is never a hard dependency.
func (m *wireGuardRelayPathManager) Bootstrap(networkMap clientapi.RegisterNodeResponse, relayOverrides map[string]string, relayResult RelayDialResult, relayErr error, now time.Time) map[string]string {
	activePeers := make(map[string]bool, len(networkMap.Peers))
	overrides := map[string]string{}
	for _, peer := range networkMap.Peers {
		peerID := strings.TrimSpace(peer.ID)
		if peerID == "" {
			continue
		}
		activePeers[peerID] = true
		state := m.peerState(peerID)
		m.updateCandidateHealth(state, peer, nil)
		relayEndpoint := strings.TrimSpace(relayOverrides[peerID])
		if relayEndpoint != "" && relayResult.Selected != nil {
			m.transition(state, "relay", "", "relay-first path established while direct candidates are probed", now)
			overrides[peerID] = relayEndpoint
			continue
		}
		directEndpoint := preferredPeerEndpoint(peer, LocalInterfaceStatuses())
		if directEndpoint != "" {
			m.transition(state, "direct", directEndpoint, "relay unavailable; using the best signed direct candidate", now)
			overrides[peerID] = directEndpoint
		} else {
			m.transition(state, "none", "", firstNonEmptyString(errorString(relayErr), "no relay or direct endpoint is available"), now)
		}
	}
	m.removeInactivePeers(activePeers)
	m.statuses = m.buildStatuses(networkMap, relayResult, relayErr)
	return overrides
}

// Reconcile applies authenticated probe results, uses RTT hysteresis when two
// direct paths are healthy, and falls back to relay after consecutive direct
// failures. Returned trigger targets should receive one overlay datagram so
// WireGuard starts a handshake immediately after a direct-path transition.
func (m *wireGuardRelayPathManager) Reconcile(networkMap clientapi.RegisterNodeResponse, relayOverrides map[string]string, relayResult RelayDialResult, relayErr error, probes map[string][]DirectEndpointProbe, now time.Time) (map[string]string, []string) {
	activePeers := make(map[string]bool, len(networkMap.Peers))
	overrides := map[string]string{}
	triggerTargets := []string{}
	for _, peer := range networkMap.Peers {
		peerID := strings.TrimSpace(peer.ID)
		if peerID == "" {
			continue
		}
		activePeers[peerID] = true
		state := m.peerState(peerID)
		m.updateCandidateHealth(state, peer, probes[peerID])
		relayEndpoint := strings.TrimSpace(relayOverrides[peerID])
		relayReachable := relayEndpoint != "" && relayResult.Selected != nil
		beforePath, beforeEndpoint := state.selectedPath, state.selectedEndpoint
		m.selectPath(state, peer, relayReachable, relayErr, now)
		if state.selectedPath == "relay" && relayReachable {
			overrides[peerID] = relayEndpoint
		} else if state.selectedPath == "direct" && state.selectedEndpoint != "" {
			overrides[peerID] = state.selectedEndpoint
		}
		if state.selectedPath == "direct" && (beforePath != "direct" || beforeEndpoint != state.selectedEndpoint) {
			if target := PeerRouteTarget(peer); target != "" {
				triggerTargets = append(triggerTargets, target)
			}
		}
	}
	m.removeInactivePeers(activePeers)
	m.statuses = m.buildStatuses(networkMap, relayResult, relayErr)
	return overrides, triggerTargets
}

func (m *wireGuardRelayPathManager) peerState(peerID string) *wireGuardPeerPathSelection {
	state := m.peers[peerID]
	if state == nil {
		state = &wireGuardPeerPathSelection{candidates: map[string]*wireGuardDirectCandidateHealth{}}
		m.peers[peerID] = state
	}
	return state
}

func (m *wireGuardRelayPathManager) removeInactivePeers(activePeers map[string]bool) {
	for peerID := range m.peers {
		if !activePeers[peerID] {
			delete(m.peers, peerID)
		}
	}
}

func (m *wireGuardRelayPathManager) updateCandidateHealth(state *wireGuardPeerPathSelection, peer clientapi.Peer, probes []DirectEndpointProbe) {
	active := map[string]bool{}
	for _, endpoint := range peerDirectEndpoints(peer) {
		active[endpoint] = true
		if state.candidates[endpoint] == nil {
			state.candidates[endpoint] = &wireGuardDirectCandidateHealth{}
		}
	}
	for endpoint := range state.candidates {
		if !active[endpoint] {
			delete(state.candidates, endpoint)
		}
	}
	for _, probe := range probes {
		endpoint := strings.TrimSpace(probe.Endpoint)
		health := state.candidates[endpoint]
		if health == nil {
			continue
		}
		health.checkedAt = probe.CheckedAt.UTC()
		if health.checkedAt.IsZero() {
			health.checkedAt = time.Now().UTC()
		}
		if probe.Reachable {
			health.reachable = true
			health.lastReachableAt = health.checkedAt
			health.consecutiveFailures = 0
			health.lastError = ""
			if health.rtt <= 0 {
				health.rtt = probe.RTT
			} else {
				health.rtt = (health.rtt*3 + probe.RTT) / 4
			}
			continue
		}
		health.reachable = false
		health.consecutiveFailures++
		health.lastError = strings.TrimSpace(probe.Error)
	}
}

func (m *wireGuardRelayPathManager) selectPath(state *wireGuardPeerPathSelection, peer clientapi.Peer, relayReachable bool, relayErr error, now time.Time) {
	bestEndpoint, bestHealth := bestDirectCandidate(state, "")
	switch state.selectedPath {
	case "relay":
		if bestEndpoint != "" && bestHealth.reachable {
			m.transition(state, "direct", bestEndpoint, "authenticated direct path selected after successful candidate probe", now)
			return
		}
		if !relayReachable {
			fallback := preferredPeerEndpoint(peer, LocalInterfaceStatuses())
			if fallback != "" {
				m.transition(state, "direct", fallback, "relay unavailable; retrying a signed direct candidate", now)
			} else {
				m.transition(state, "none", "", firstNonEmptyString(errorString(relayErr), "relay is unavailable and no direct candidate exists"), now)
			}
		}
	case "direct":
		current := state.candidates[state.selectedEndpoint]
		if current == nil {
			if bestEndpoint != "" && bestHealth.reachable {
				m.transition(state, "direct", bestEndpoint, "previous direct candidate was removed; selected a reachable replacement", now)
			} else if relayReachable {
				m.transition(state, "relay", "", "selected direct candidate was removed", now)
			} else {
				m.transition(state, "none", "", "selected direct candidate was removed and relay is unavailable", now)
			}
			return
		}
		if current.consecutiveFailures >= wireGuardDirectFailureThreshold {
			alternative, health := bestDirectCandidate(state, state.selectedEndpoint)
			if alternative != "" && health.reachable {
				m.transition(state, "direct", alternative, "selected direct path failed; switched to another reachable candidate", now)
			} else if relayReachable {
				m.transition(state, "relay", "", "selected direct path failed consecutive authenticated probes", now)
			} else {
				state.reason = "selected direct path is degraded and relay is unavailable"
			}
			return
		}
		if bestEndpoint != "" && bestEndpoint != state.selectedEndpoint && directCandidateMateriallyBetter(current, bestHealth) {
			m.transition(state, "direct", bestEndpoint, "switched to a materially lower-latency direct candidate", now)
		}
	default:
		if relayReachable {
			m.transition(state, "relay", "", "relay-first path established while direct candidates are probed", now)
		} else if bestEndpoint != "" && bestHealth.reachable {
			m.transition(state, "direct", bestEndpoint, "authenticated direct path is reachable", now)
		} else if fallback := preferredPeerEndpoint(peer, LocalInterfaceStatuses()); fallback != "" {
			m.transition(state, "direct", fallback, "relay unavailable; using the best signed direct candidate", now)
		} else {
			m.transition(state, "none", "", firstNonEmptyString(errorString(relayErr), "no path is available"), now)
		}
	}
}

func bestDirectCandidate(state *wireGuardPeerPathSelection, excluded string) (string, *wireGuardDirectCandidateHealth) {
	var bestEndpoint string
	var best *wireGuardDirectCandidateHealth
	for endpoint, health := range state.candidates {
		if endpoint == excluded || !health.reachable || health.rtt <= 0 {
			continue
		}
		if best == nil || health.rtt < best.rtt || health.rtt == best.rtt && endpoint < bestEndpoint {
			bestEndpoint, best = endpoint, health
		}
	}
	return bestEndpoint, best
}

func directCandidateMateriallyBetter(current, challenger *wireGuardDirectCandidateHealth) bool {
	if current == nil || challenger == nil || !challenger.reachable || challenger.rtt <= 0 {
		return false
	}
	if !current.reachable || current.rtt <= 0 {
		return true
	}
	absoluteImprovement := current.rtt - challenger.rtt
	return absoluteImprovement >= 10*time.Millisecond && challenger.rtt*5 <= current.rtt*4
}

func (m *wireGuardRelayPathManager) transition(state *wireGuardPeerPathSelection, selectedPath, endpoint, reason string, now time.Time) {
	if state.selectedPath == selectedPath && state.selectedEndpoint == endpoint {
		if strings.TrimSpace(reason) != "" {
			state.reason = reason
		}
		return
	}
	state.selectedPath = selectedPath
	state.selectedEndpoint = endpoint
	state.lastTransition = now.UTC()
	state.reason = reason
}

func (m *wireGuardRelayPathManager) buildStatuses(networkMap clientapi.RegisterNodeResponse, relayResult RelayDialResult, relayErr error) []PeerPathStatus {
	relay := relayCandidateStatus(networkMap, relayResult, relayErr)
	statuses := make([]PeerPathStatus, 0, len(networkMap.Peers))
	for _, peer := range networkMap.Peers {
		state := m.peers[strings.TrimSpace(peer.ID)]
		if state == nil {
			continue
		}
		candidates := make([]PathCandidateStatus, 0, len(state.candidates))
		for _, endpoint := range peerDirectEndpoints(peer) {
			health := state.candidates[endpoint]
			if health == nil {
				continue
			}
			candidate := directHealthStatus(peer, endpoint, health)
			candidates = append(candidates, candidate)
		}
		direct := PathCandidateStatus{Type: "direct", State: "missing", Reason: "peer endpoint is not published in the network map"}
		if len(candidates) > 0 {
			direct = candidates[0]
			if state.selectedPath == "direct" {
				for _, candidate := range candidates {
					if candidate.Endpoint == state.selectedEndpoint {
						direct = candidate
						break
					}
				}
			} else {
				for _, candidate := range candidates[1:] {
					if candidate.State == "reachable" && (direct.State != "reachable" || candidate.RTTMS < direct.RTTMS) {
						direct = candidate
					}
				}
			}
		}
		selectedEndpoint := state.selectedEndpoint
		if state.selectedPath == "relay" {
			selectedEndpoint = relay.Endpoint
		}
		status := PeerPathStatus{
			PeerID:           peer.ID,
			Hostname:         peer.Hostname,
			Direct:           direct,
			Candidates:       candidates,
			Relay:            relay,
			SelectedPath:     state.selectedPath,
			SelectedEndpoint: selectedEndpoint,
			SelectionReason:  state.reason,
		}
		if !state.lastTransition.IsZero() {
			status.LastTransitionAt = state.lastTransition.UTC().Format(time.RFC3339Nano)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func directHealthStatus(peer clientapi.Peer, endpoint string, health *wireGuardDirectCandidateHealth) PathCandidateStatus {
	status := PathCandidateStatus{
		Type:                "direct",
		Tier:                directEndpointTier(peer, endpoint),
		Priority:            directEndpointPriority(peer, endpoint),
		State:               "untested",
		Endpoint:            endpoint,
		ConsecutiveFailures: health.consecutiveFailures,
	}
	if !health.checkedAt.IsZero() {
		status.CheckedAt = health.checkedAt.UTC().Format(time.RFC3339Nano)
	}
	if !health.lastReachableAt.IsZero() {
		status.LastReachableAt = health.lastReachableAt.UTC().Format(time.RFC3339Nano)
	}
	if health.rtt > 0 {
		status.RTTMS = float64(health.rtt) / float64(time.Millisecond)
	}
	if health.reachable {
		status.State = "reachable"
		return status
	}
	if health.consecutiveFailures > 0 {
		status.State = "degraded"
		if health.consecutiveFailures >= wireGuardDirectFailureThreshold {
			status.State = "failed"
		}
		status.Reason = firstNonEmptyString(health.lastError, "authenticated direct-path probe failed")
	}
	return status
}

func (m *wireGuardRelayPathManager) Statuses() []PeerPathStatus {
	out := make([]PeerPathStatus, len(m.statuses))
	copy(out, m.statuses)
	for index := range out {
		out[index].Candidates = append([]PathCandidateStatus(nil), m.statuses[index].Candidates...)
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
