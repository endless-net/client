package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/tailscale/wireguard-go/device"
)

func exitLANHealthFixture(t *testing.T, relayed bool) (*WireGuardEngine, Config, time.Time, func(*WireGuardEngine) (WireGuardInspection, error)) {
	t.Helper()
	n, cfg, _ := nativeExitResumeFixture(t)
	signing, _, key := signedApplicationFixture(t, false)
	cfg.MapSigningTrust = signing.MapSigningTrust
	cfg.ExitSelection.LAN = api.ExitLANAllow
	cfg.CachedMap.Network.ClientPolicy.ExitNodes[0].AllowedLANAccess = []api.ExitLANAccess{api.ExitLANAllow}
	if relayed {
		cfg.CachedMap.Relays = []relay.Endpoint{{ID: "relay", Addr: "192.0.2.10:443", Protocol: relay.EndpointProtocolTLS}}
		cfg.CachedMap.RelayCredential = nil
	}
	resignApplicationMap(t, cfg.CachedMap, key)
	cfg = clonePersistentConfig(cfg)
	e := n.engine
	e.pathCancel = func() {}
	// The private engine stages the signed policy with the physical guard still
	// BLOCK-only. This bypasses no public admission and installs no LAN exception.
	if result, err := e.configureExit(t.Context(), cfg, *cfg.CachedMap, cfg.ExitSelection, e.exitGuard); err != nil || !result.OK {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handshake := now.Add(-device.RejectAfterTime + 20*time.Second)
	peer := cfg.CachedMap.Peers[0]
	e.mu.Lock()
	path := PeerPathStatus{PeerID: peer.ID, SelectedPath: "direct", SelectedEndpoint: peer.Endpoint, LastTransitionAt: handshake.Add(-time.Second).Format(time.RFC3339Nano), Direct: PathCandidateStatus{Endpoint: peer.Endpoint, State: "reachable", CheckedAt: now.Add(-time.Second).Format(time.RFC3339Nano)}}
	if relayed {
		selected := cfg.CachedMap.Relays[0]
		generation := newWireGuardRelayGeneration(*cfg.CachedMap)
		generation.readyAt = handshake.Add(-2 * time.Second)
		e.relayBridge = &wireGuardRelayBridge{generation: generation, cancel: func() {}, statusOK: true, status: RelayDataplaneBridgeStatus{Relay: RelayDialResult{Selected: &selected}, PeerEndpoints: map[string]string{peer.ID: peer.Endpoint}}}
		path.SelectedPath, path.SelectedEndpoint = "relay", selected.Addr
		path.Relay = PathCandidateStatus{Endpoint: selected.Addr, State: "reachable", RelayID: selected.ID, Protocol: selected.Protocol}
	}
	e.relayPaths.statuses = []PeerPathStatus{path}
	e.mu.Unlock()
	inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
		result, err := resourceObservedUAPI(e)
		for i := range result.Peers {
			if result.Peers[i].PublicKey == peer.PublicKey {
				result.Peers[i].LatestHandshakeUnix = handshake.Unix()
				result.Peers[i].latestHandshakeNanos = int64(handshake.Nanosecond())
				result.Peers[i].handshakeTimeComplete = true
			}
		}
		return result, err
	}
	return e, cfg, now, inspect
}

func TestExitLANHealthCannotRenewAnOldHandshake(t *testing.T) {
	for _, relayed := range []bool{false, true} {
		e, cfg, now, inspect := exitLANHealthFixture(t, relayed)
		e.mu.Lock()
		proof, err := e.observeExitLANPeerHealthWithInspection(t.Context(), cfg, cfg.ExitSelection, now, inspect)
		if err != nil || !proof.expires.Equal(now.Add(20*time.Second)) || !proof.currentWithInspection(t.Context(), cfg, now, inspect) {
			e.mu.Unlock()
			t.Fatal("selected exit transport not bound to original handshake deadline", err)
		}
		again, err := e.observeExitLANPeerHealthWithInspection(t.Context(), cfg, cfg.ExitSelection, now.Add(time.Second), inspect)
		if err != nil || !again.expires.Equal(proof.expires) {
			e.mu.Unlock()
			t.Fatal("read renewed health without a new handshake", err)
		}
		if proof.currentWithInspection(t.Context(), cfg, proof.expires, inspect) || proof.currentWithInspection(t.Context(), cfg, now.Add(-time.Nanosecond), inspect) {
			e.mu.Unlock()
			t.Fatal("receipt survived expiry or clock rollback")
		}
		// A synthetic unit receipt cannot replace the production device's missing
		// handshake. APPLIED configuration and getter calls are insufficient.
		if proof.currentLocked(t.Context(), cfg, now) {
			e.mu.Unlock()
			t.Fatal("configuration alone counted as live native health")
		}
		if _, err := e.observeExitLANPeerHealthLocked(t.Context(), cfg, cfg.ExitSelection, now); err == nil {
			e.mu.Unlock()
			t.Fatal("native preparation manufactured peer health")
		}
		e.mu.Unlock()
	}
}

func TestExitLANHealthRejectsChangedContextPathAndIncompleteEvidence(t *testing.T) {
	for _, scenario := range []string{"owner", "profile", "map", "selection", "device", "path_manager", "endpoint", "transition", "duplicate_path", "incomplete", "future", "wrong_peer", "filter", "suspended", "cancelled", "late_cancel", "relay_ended", "relay_replaced"} {
		t.Run(scenario, func(t *testing.T) {
			relayed := scenario == "relay_ended" || scenario == "relay_replaced"
			e, cfg, now, inspect := exitLANHealthFixture(t, relayed)
			e.mu.Lock()
			defer e.mu.Unlock()
			proof, err := e.observeExitLANPeerHealthWithInspection(t.Context(), cfg, cfg.ExitSelection, now, inspect)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			modified := inspect
			switch scenario {
			case "owner":
				cfg.LocalOwnerID = "foreign"
			case "profile":
				cfg.RPCState.ActiveProfileID = "foreign"
			case "map":
				cfg.CachedMap.MapSignature.PayloadHash = "foreign"
			case "selection":
				cfg.ExitSelection.ID = "foreign"
			case "device":
				original := e.device
				e.device = nil
				defer func() { e.device = original }()
			case "path_manager":
				copy := *e.relayPaths
				e.relayPaths = &copy
			case "endpoint":
				e.relayPaths.statuses[0].SelectedEndpoint = "127.0.0.1:54321"
			case "transition":
				e.relayPaths.statuses[0].LastTransitionAt = now.Format(time.RFC3339Nano)
			case "duplicate_path":
				e.relayPaths.statuses = append(e.relayPaths.statuses, e.relayPaths.statuses[0])
			case "filter":
				e.exitFilter.withdraw()
			case "suspended":
				e.runtimeSuspended = true
			case "cancelled":
				cancel()
			case "relay_ended":
				close(e.relayBridge.generation.ended)
			case "relay_replaced":
				e.relayBridge.generation = newWireGuardRelayGeneration(*cfg.CachedMap)
				e.relayBridge.generation.readyAt = now
			default:
				modified = func(e *WireGuardEngine) (WireGuardInspection, error) {
					result, err := inspect(e)
					for i := range result.Peers {
						switch scenario {
						case "incomplete":
							result.Peers[i].handshakeTimeComplete = false
						case "future":
							result.Peers[i].LatestHandshakeUnix = now.Add(time.Hour).Unix()
						case "wrong_peer":
							result.Peers[i].PublicKey = testWireGuardEnginePublicKey(3)
						case "late_cancel":
							cancel()
						}
					}
					return result, err
				}
			}
			if proof.currentWithInspection(ctx, cfg, now, modified) {
				t.Fatal("changed/absent evidence retained LAN peer health")
			}
		})
	}
}

func TestExitLANPreparationIntersectsHealthTopologyAndPolicy(t *testing.T) {
	e, cfg, now, inspect := exitLANHealthFixture(t, false)
	e.mu.Lock()
	defer e.mu.Unlock()
	topology := &exitLANSource{OwnInterface: e.interface_, ValidUntil: now.Add(10 * time.Second), Links: []exitLANLink{{Index: 2, LinkIndex: 2, Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}, Routes: []exitLANDirectRoute{{Prefix: netip.MustParsePrefix("192.0.2.0/24")}}}}}
	topology.lifetime = exitLANTestLifetime(t)
	plan, health, err := e.prepareExitLANWithInspection(t.Context(), cfg, topology, now, inspect)
	if err != nil || !plan.expires.Equal(topology.ValidUntil) || !health.expires.Equal(now.Add(20*time.Second)) {
		t.Fatal("topology and handshake deadlines not intersected", err)
	}
	topology.ValidUntil = now.Add(time.Hour)
	plan, _, err = e.prepareExitLANWithInspection(t.Context(), cfg, topology, now, inspect)
	if err != nil || !plan.expires.Equal(health.expires) {
		t.Fatal("long topology lifetime extended old handshake", err)
	}
	e.relayPaths.statuses[0].Direct.CheckedAt = now.Add(-device.RejectAfterTime + 5*time.Second).Format(time.RFC3339Nano)
	plan, _, err = e.prepareExitLANWithInspection(t.Context(), cfg, topology, now, inspect)
	if err != nil || !plan.expires.Equal(now.Add(5*time.Second)) {
		t.Fatal("direct check deadline was not preserved", err)
	}
	if _, _, err := e.prepareExitLANWithHealthLocked(t.Context(), cfg, topology, now); err == nil {
		t.Fatal("production preparation accepted absent actual handshake")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := e.prepareExitLANWithInspection(ctx, cfg, topology, now, inspect); !errors.Is(err, context.Canceled) {
		t.Fatal("preparation ignored cancellation", err)
	}
}

func TestExitLANReadbackCannotCrossEvidenceDeadline(t *testing.T) {
	for _, preparation := range []bool{false, true} {
		e, cfg, now, inspect := exitLANHealthFixture(t, false)
		e.mu.Lock()
		proof, err := e.observeExitLANPeerHealthWithInspection(t.Context(), cfg, cfg.ExitSelection, now, inspect)
		if err != nil {
			e.mu.Unlock()
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		proof.expires = deadline
		calls := 0
		delayed := func(e *WireGuardEngine) (WireGuardInspection, error) {
			calls++
			if !preparation || calls == 2 {
				time.Sleep(time.Until(deadline) + time.Millisecond)
			}
			return inspect(e)
		}
		if preparation {
			topology := &exitLANSource{OwnInterface: e.interface_, ValidUntil: deadline, Links: []exitLANLink{{Index: 2, LinkIndex: 2, Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}, Routes: []exitLANDirectRoute{{Prefix: netip.MustParsePrefix("192.0.2.0/24")}}}}}
			topology.lifetime = exitLANTestLifetime(t)
			_, _, err = e.prepareExitLANWithInspection(t.Context(), cfg, topology, time.Now(), delayed)
			if err == nil || calls != 2 {
				e.mu.Unlock()
				t.Fatal("final readback did not reject expired topology", err, calls)
			}
		} else if proof.currentWithInspection(t.Context(), cfg, time.Now(), delayed) {
			e.mu.Unlock()
			t.Fatal("readback extended original receipt deadline")
		}
		e.mu.Unlock()
	}
}
