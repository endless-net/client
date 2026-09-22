package client

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/device"
)

// ResourceHostObservation is immutable transport evidence for HOST entries.
// It says nothing about a remote application or service. Native routes remain
// point-in-time observations: route notifications invalidate the receipt, but
// asynchronous delivery is not an OS lease or packet-time enforcement.
type ResourceHostObservation struct {
	engine        *WireGuardEngine
	device        *device.Device
	configuration [32]byte
	uapi          [32]byte
	paths         [32]byte
	pathManager   *wireGuardRelayPathManager
	observedAt    time.Time
	expires       time.Time
	hosts         map[string]bool
	relayBridge   *wireGuardRelayBridge
	relays        []wireGuardRelayPeerObservation
	lifetime      *exitLANSourceLifetime
	close         func() error
}

// Close releases the one-shot topology subscription and permanently revokes
// the receipt. Callers must retain it through their final publication check.
func (o *ResourceHostObservation) Close() error {
	if o == nil || o.close == nil {
		return nil
	}
	return o.close()
}

func (o *ResourceHostObservation) HostConfirmed(id string) bool { return o != nil && o.hosts[id] }

func resourceObservationConfig(cfg Config) [32]byte {
	raw, err := json.Marshal(clonePersistentConfig(cfg))
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(raw)
}

func resourceObservationPaths(e *WireGuardEngine) [32]byte {
	if e.relayPaths == nil {
		return [32]byte{}
	}
	raw, _ := json.Marshal(e.relayPaths.Statuses())
	return sha256.Sum256(raw)
}

func (o *ResourceHostObservation) Current(cfg Config, now time.Time) bool {
	return o.currentWithInspection(cfg, now, resourceObservedUAPI)
}

// The production entry point always reads the live device. This private seam
// makes changes during the final readback reproducible without native traffic.
func (o *ResourceHostObservation) currentWithInspection(cfg Config, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) bool {
	started := time.Now()
	if o == nil || o.engine == nil || inspect == nil || !o.lifetime.current() || now.Before(o.observedAt) || !now.Before(o.expires) || resourceObservationConfig(cfg) != o.configuration {
		return false
	}
	e := o.engine
	if !e.mu.TryLock() {
		return false
	}
	defer e.mu.Unlock()
	if !e.configured || e.runtimeSuspended || e.device != o.device || e.relayPaths != o.pathManager || sha256.Sum256([]byte(e.uapi)) != o.uapi || resourceObservationPaths(e) != o.paths || cfg.CachedMap == nil || e.runtimeIdentity != nativeExitAppliedIdentity(cfg, *cfg.CachedMap, e.interface_) || !resourceHostExitFilterCurrent(e, cfg, now) {
		return false
	}
	expires := resourceHostReceiptDeadline(e, cfg, o.expires)
	if len(o.relays) != 0 {
		if e.relayBridge != o.relayBridge || e.relayBridge == nil {
			return false
		}
		for _, relay := range o.relays {
			if !e.relayBridge.ObservationCurrent(relay) {
				return false
			}
		}
	}
	eligible, err := resourceHostFilterEligibility(e, cfg, now)
	if err != nil {
		return false
	}
	for id := range o.hosts {
		if !eligible[id] {
			return false
		}
	}
	if _, err = inspect(e); err != nil {
		return false
	}
	// The bridge can end independently of engine.mu while filters or device
	// readback are in progress. A successful earlier check is not a lease.
	for _, relay := range o.relays {
		if e.relayBridge != o.relayBridge || !o.relayBridge.ObservationCurrent(relay) {
			return false
		}
	}
	return o.lifetime.current() && now.Add(time.Since(started)).Before(expires)
}

// Caller holds engine.mu and has verified resourceHostExitFilterCurrent. The
// committed policy supplies the authenticated grant deadline even when a HOST
// is otherwise ordinary traffic. No read may extend the original receipt.
func resourceHostReceiptDeadline(e *WireGuardEngine, cfg Config, expires time.Time) time.Time {
	if cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil {
		return time.Time{}
	}
	if cfg.CachedMap.MapSignature.ExpiresAt.Before(expires) {
		expires = cfg.CachedMap.MapSignature.ExpiresAt
	}
	if cfg.ExitSelection != nil {
		if e.exitFilter == nil {
			return time.Time{}
		}
		e.exitFilter.mu.RLock()
		defer e.exitFilter.mu.RUnlock()
		if e.exitFilter.current == nil {
			return time.Time{}
		}
		if e.exitFilter.current.grantExpires.Before(expires) {
			expires = e.exitFilter.current.grantExpires
		}
	}
	return expires
}

// The caller holds the shared effect lock and performs a fresh store check
// before publishing. No commands run on unsupported platforms. Bounds cover the
// whole collection, including catalogs with up to 4096 individual host targets.
func (e *WireGuardEngine) ObserveResourceHosts(ctx context.Context, cfg Config) (*ResourceHostObservation, error) {
	return e.observeResourceHosts(ctx, cfg, nil, time.Now())
}

func (e *WireGuardEngine) observeResourceHosts(ctx context.Context, cfg Config, runner CommandRunner, now time.Time) (*ResourceHostObservation, error) {
	return e.observeResourceHostsWithInspection(ctx, cfg, runner, now, resourceObservedUAPI)
}

// The private inspection seam permits deterministic captured-handshake fixtures;
// every production call fixes it to actual authenticated device readback above.
func (e *WireGuardEngine) observeResourceHostsWithInspection(ctx context.Context, cfg Config, runner CommandRunner, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error)) (*ResourceHostObservation, error) {
	return e.observeResourceHostsWithTopology(ctx, cfg, runner, now, inspect, openResourceHostRouteWatch)
}

func (e *WireGuardEngine) observeResourceHostsWithTopology(ctx context.Context, cfg Config, runner CommandRunner, now time.Time, inspect func(*WireGuardEngine) (WireGuardInspection, error), open func(context.Context) (exitLANChangeStream, error)) (*ResourceHostObservation, error) {
	started := time.Now()
	if e == nil || inspect == nil || open == nil {
		return nil, errResourceHostObservation
	}
	runner, err := resourceRouteRunner(runner)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	keep := false
	var stream exitLANChangeStream
	closeObservation := sync.OnceValue(func() error {
		cancel()
		if stream != nil {
			return stream.Close()
		}
		return nil
	})
	defer func() {
		if !keep {
			_ = closeObservation()
		}
	}()
	if _, err := compileResourceDenials(cfg, now); err != nil {
		return nil, errResourceHostObservation
	}
	if cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected || cfg.RPCState == nil || cfg.RPCState.ActiveProfileID == "" || cfg.LocalOwnerID == "" {
		return nil, errResourceHostObservation
	}
	if !e.mu.TryLock() {
		return nil, errResourceHostObservation
	}
	defer e.mu.Unlock()
	if !e.configured || e.device == nil || e.runtimeSuspended || e.pathMap.MapSignature == nil || e.pathMap.MapSignature.PayloadHash != cfg.CachedMap.MapSignature.PayloadHash || e.pathMap.Node.ID != cfg.NodeID || e.pathMap.Network.ID != cfg.NetworkID || e.relayPaths == nil {
		return nil, errResourceHostObservation
	}
	if e.runtimeIdentity != nativeExitAppliedIdentity(cfg, *cfg.CachedMap, e.interface_) || !resourceHostExitFilterCurrent(e, cfg, now) {
		return nil, errResourceHostObservation
	}
	// Subscribe before any route/interface/rule collection. A change followed by
	// restoration must invalidate the whole batch, even if final dumps match.
	stream, err = open(ctx)
	if err != nil {
		return nil, err
	}
	lifetime := &exitLANSourceLifetime{ctx: ctx, stream: stream}
	if !lifetime.current() {
		return nil, errResourceHostObservation
	}
	if cfg.ExitSelection != nil {
		if !nativeExitResumeContext(cfg, e.interface_) || e.exitGuard == nil || e.exitGuard.Observe(ctx, cfg.ExitSelection.Family) != nil {
			return nil, errResourceHostObservation
		}
	}
	inspection, err := inspect(e)
	if err != nil {
		return nil, err
	}
	eligible, err := resourceHostFilterEligibility(e, cfg, now)
	if err != nil {
		return nil, err
	}
	rules, err := observeResourceHostRules(ctx, e.routerCfg.FirewallMark, runner)
	if err != nil {
		return nil, err
	}
	own, err := resourceRouteInterface(ctx, e.interface_, runner)
	if err != nil {
		return nil, err
	}
	proof := &ResourceHostObservation{engine: e, device: e.device, configuration: resourceObservationConfig(cfg), uapi: sha256.Sum256([]byte(e.uapi)), paths: resourceObservationPaths(e), pathManager: e.relayPaths, observedAt: now, expires: now.Add(5 * time.Second), hosts: map[string]bool{}}
	proof.expires = resourceHostReceiptDeadline(e, cfg, proof.expires)
	proof.lifetime, proof.close = lifetime, closeObservation
	paths := e.relayPaths.Statuses()
	count := 0
	for _, peer := range cfg.CachedMap.Peers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !eligible[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)] {
			continue
		}
		live, ok := wireGuardPeerForMapPeer(inspection, peer)
		if !ok {
			continue
		}
		var relayProof wireGuardRelayPeerObservation
		relayConfirmed := false
		if !resourceHostPathObserved(paths, peer.ID, live, now) {
			if e.relayBridge == nil {
				continue
			}
			relayProof, ok = e.relayBridge.ObservePeer(*cfg.CachedMap, peer.ID)
			if !ok || !resourceHostRelayPathObserved(paths, peer, live, relayProof, now) {
				continue
			}
			relayConfirmed = true
		}
		addresses := []netip.Addr{}
		for _, raw := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				return nil, errResourceHostObservation
			}
			if prefix.IsSingleIP() {
				if !slices.Contains(live.AllowedIPs, prefix.String()) {
					addresses = nil
					break
				}
				addresses = append(addresses, prefix.Addr())
			}
		}
		if len(addresses) == 0 {
			continue
		}
		confirmed := true
		for _, target := range addresses {
			count++
			if count > 4096 {
				return nil, errResourceHostObservation
			}
			if err := observeResourceHostRoute(ctx, target, own, e.routerCfg.Addresses, runner); err != nil {
				confirmed = false
				break
			}
		}
		if confirmed {
			proof.hosts[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)] = true
			if relayConfirmed {
				proof.relayBridge = e.relayBridge
				proof.relays = append(proof.relays, relayProof)
			}
			handshake, _ := live.authenticatedHandshakeTime()
			expires := handshake.Add(device.RejectAfterTime)
			if expires.Before(proof.expires) {
				proof.expires = expires
			}
			for _, path := range paths {
				if path.PeerID == peer.ID && path.SelectedPath == "direct" {
					for _, candidate := range append([]PathCandidateStatus{path.Direct}, path.Candidates...) {
						if candidate.Endpoint == live.Endpoint && candidate.State == "reachable" {
							checked, err := time.Parse(time.RFC3339Nano, candidate.CheckedAt)
							if err == nil {
								expiry := checked.Add(device.RejectAfterTime)
								if expiry.Before(proof.expires) {
									proof.expires = expiry
								}
							}
						}
					}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Detect interface replacement/address loss during the lookup batch.
	after, err := resourceRouteInterface(ctx, e.interface_, runner)
	if err != nil || !reflect.DeepEqual(own, after) {
		return nil, errResourceHostObservation
	}
	afterRules, err := observeResourceHostRules(ctx, e.routerCfg.FirewallMark, runner)
	if err != nil || afterRules != rules {
		return nil, errResourceHostObservation
	}
	if _, err := inspect(e); err != nil {
		return nil, err
	}
	for _, relay := range proof.relays {
		if !e.relayBridge.ObservationCurrent(relay) {
			return nil, errResourceHostObservation
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !lifetime.current() || !now.Add(time.Since(started)).Before(proof.expires) {
		return nil, errResourceHostObservation
	}
	keep = true
	return proof, nil
}

func resourceHostExitFilterCurrent(e *WireGuardEngine, cfg Config, now time.Time) bool {
	if e.exitFilter == nil {
		return cfg.ExitSelection == nil && e.exitGuard == nil && e.exitSelection == nil
	}
	if cfg.CachedMap == nil || !reflect.DeepEqual(e.exitSelection, cfg.ExitSelection) {
		return false
	}
	want, err := compileExitPacketPolicy(cfg, *cfg.CachedMap, cfg.ExitSelection, now)
	if err != nil {
		return false
	}
	e.exitFilter.mu.RLock()
	defer e.exitFilter.mu.RUnlock()
	return !e.exitFilter.closed && !e.exitFilter.applying && e.exitFilter.pending == nil && reflect.DeepEqual(e.exitFilter.current, want)
}

func resourceHostPathObserved(paths []PeerPathStatus, id string, peer WireGuardPeerInspection, now time.Time) bool {
	handshake, complete := peer.authenticatedHandshakeTime()
	if !complete || handshake.After(now) || !now.Before(handshake.Add(device.RejectAfterTime)) {
		return false
	}
	for _, path := range paths {
		if path.PeerID != id || path.SelectedEndpoint == "" || path.SelectedEndpoint != peer.Endpoint {
			continue
		}
		transition, err := time.Parse(time.RFC3339Nano, path.LastTransitionAt)
		if err != nil || !handshake.After(transition) {
			continue
		}
		// Relay proof uses the separate bridge-generation binding above.
		if path.SelectedPath == "direct" {
			for _, candidate := range append([]PathCandidateStatus{path.Direct}, path.Candidates...) {
				checked, err := time.Parse(time.RFC3339Nano, candidate.CheckedAt)
				if err == nil && candidate.Endpoint == peer.Endpoint && candidate.State == "reachable" && !checked.After(now) && now.Before(checked.Add(device.RejectAfterTime)) {
					return true
				}
			}
		}
	}
	return false
}

func resourceObservedUAPI(e *WireGuardEngine) (WireGuardInspection, error) {
	var inspection WireGuardInspection
	if e.device == nil {
		return inspection, errResourceHostObservation
	}
	raw, err := e.device.IpcGet()
	if err != nil {
		return inspection, errResourceHostObservation
	}
	// Pinned WireGuard omits a zero fwmark from its live get response.
	markPresent := false
	for _, line := range strings.Split(raw, "\n") {
		markPresent = markPresent || strings.HasPrefix(line, "fwmark=")
	}
	if !markPresent {
		raw = "fwmark=0\n" + raw
	}
	want, err := parseNativeExitUAPI(e.uapi)
	if err != nil {
		return inspection, errResourceHostObservation
	}
	live, err := parseNativeExitUAPI(raw)
	if err != nil || live.mark != want.mark || live.localPublic != e.pathMap.Node.PublicKey || want.localPublic != live.localPublic || len(live.peers) != len(want.peers) {
		return inspection, errResourceHostObservation
	}
	owners := map[netip.Prefix]string{}
	for key, peer := range live.peers {
		expected, ok := want.peers[key]
		if !ok || !peer.pskSeen || peer.endpoint != expected.endpoint || !reflect.DeepEqual(peer.routes, expected.routes) || subtle.ConstantTimeCompare(peer.pskDigest[:], expected.pskDigest[:]) != 1 {
			return inspection, errResourceHostObservation
		}
		for prefix := range peer.routes {
			if old, ok := owners[prefix]; ok && old != key {
				return inspection, errResourceHostObservation
			}
			owners[prefix] = key
		}
	}
	if parseWireGuardEngineIPC(&inspection, raw) != nil {
		return inspection, errResourceHostObservation
	}
	inspection.OK = true
	return inspection, nil
}
