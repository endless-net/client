package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/tailscale/wireguard-go/device"
)

func TestResourceHostCurrentRechecksDeadlineAfterReadback(t *testing.T) {
	cfg, _, proof := resourceHostProjectionFixture(t)
	now := time.Now()
	proof.expires = now.Add(time.Second)
	called := false
	inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
		called = true
		result, err := resourceObservedUAPI(e)
		time.Sleep(time.Until(proof.expires) + time.Millisecond)
		return result, err
	}
	if proof.currentWithInspection(cfg, now, inspect) || !called {
		t.Fatal("readback crossed immutable receipt deadline", called)
	}
}

func TestResourceHostCurrentCannotCrossCommittedExitGrantDeadline(t *testing.T) {
	cfg, e, proof := resourceHostProjectionFixture(t)
	grant := cfg.CachedMap.Network.ClientPolicy.ExitNodes[0].ExpiresAt
	if !grant.Before(cfg.CachedMap.MapSignature.ExpiresAt) {
		t.Fatal("fixture requires grant shorter than map")
	}
	// Keep the receipt itself long-lived so only the signed grant can expire.
	// The logical observation clock starts one second before that real deadline.
	proof.expires = cfg.CachedMap.MapSignature.ExpiresAt
	at := grant.Add(-time.Second)
	e.mu.Lock()
	deadline := resourceHostReceiptDeadline(e, cfg, proof.expires)
	e.mu.Unlock()
	if !deadline.Equal(grant) || !proof.Current(cfg, at) {
		t.Fatal("committed grant deadline not bound")
	}
	called := false
	inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
		called = true
		result, err := resourceObservedUAPI(e)
		time.Sleep(time.Second + time.Millisecond)
		return result, err
	}
	if proof.currentWithInspection(cfg, at, inspect) || !called {
		t.Fatal("readback outlived signed exit grant", called)
	}
}

func TestResourceHostCurrentRechecksRelayAfterReadback(t *testing.T) {
	for _, ended := range []bool{false, true} {
		cfg, e, proof := resourceHostProjectionFixture(t)
		// A private synthetic receipt isolates asynchronous generation lifetime;
		// signed collection/path provenance has separate collector tests.
		network := *cfg.CachedMap
		selected := relay.Endpoint{ID: "relay", Addr: "relay.example:443", Protocol: relay.EndpointProtocolTLS}
		network.Relays = []relay.Endpoint{selected}
		generation := newWireGuardRelayGeneration(network)
		generation.readyAt = time.Now().Add(-time.Second)
		bridge := &wireGuardRelayBridge{generation: generation, cancel: func() {}, statusOK: true, status: RelayDataplaneBridgeStatus{Relay: RelayDialResult{Selected: &selected}, PeerEndpoints: map[string]string{network.Peers[0].ID: "127.0.0.1:1234"}}}
		snapshot, ok := bridge.ObservePeer(network, network.Peers[0].ID)
		if !ok {
			t.Fatal("missing fixture receipt")
		}
		e.mu.Lock()
		e.relayBridge = bridge
		proof.relayBridge, proof.relays = bridge, []wireGuardRelayPeerObservation{snapshot}
		e.mu.Unlock()
		called := false
		inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
			called = true
			result, err := resourceObservedUAPI(e)
			if ended {
				close(generation.ended)
			}
			return result, err
		}
		if proof.currentWithInspection(cfg, time.Now(), inspect) == ended || !called {
			t.Fatal("late bridge completion did not revoke receipt", ended, called)
		}
	}
}

func TestResourceHostCollectorRejectsLateReadbackExpiryAndCancellation(t *testing.T) {
	for _, scenario := range []string{"current", "expiry", "cancel", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, _ := resourceHostProjectionFixture(t)
			now := time.Now().UTC()
			handshake := now.Add(-device.RejectAfterTime + time.Second)
			deadline := handshake.Add(device.RejectAfterTime)
			e.mu.Lock()
			inspection, err := resourceObservedUAPI(e)
			if err != nil {
				e.mu.Unlock()
				t.Fatal(err)
			}
			peer := cfg.CachedMap.Peers[0]
			live, ok := wireGuardPeerForMapPeer(inspection, peer)
			if !ok {
				e.mu.Unlock()
				t.Fatal("fixture peer missing")
			}
			e.relayPaths.statuses = []PeerPathStatus{{PeerID: peer.ID, SelectedPath: "direct", SelectedEndpoint: live.Endpoint, LastTransitionAt: handshake.Add(-time.Second).Format(time.RFC3339Nano), Direct: PathCandidateStatus{Endpoint: live.Endpoint, State: "reachable", CheckedAt: now.Format(time.RFC3339Nano)}}}
			iface, local := e.interface_, e.routerCfg.Addresses[0].Addr().String()
			e.mu.Unlock()
			runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "rule show") {
					return []byte(`[{"priority":32766,"src":"all","table":"254"}]`), nil
				}
				if strings.Contains(joined, "address show") {
					return []byte(fmt.Sprintf(`[{"ifindex":7,"ifname":%q,"flags":["UP"],"addr_info":[{"local":%q}]}]`, iface, local)), nil
				}
				return []byte(fmt.Sprintf(`[{"dst":%q,"dev":%q,"prefsrc":%q,"flags":[]}]`, args[5], iface, local)), nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
				calls++
				value, err := resourceObservedUAPI(e)
				for i := range value.Peers {
					value.Peers[i].LatestHandshakeUnix = handshake.Unix()
					value.Peers[i].latestHandshakeNanos = int64(handshake.Nanosecond())
					value.Peers[i].handshakeTimeComplete = true
				}
				if calls == 2 {
					switch scenario {
					case "expiry":
						time.Sleep(time.Until(deadline) + time.Millisecond)
					case "cancel":
						cancel()
					case "failure":
						return WireGuardInspection{}, errResourceHostObservation
					}
				}
				return value, err
			}
			proof, err := observeResourceHostsTest(t, e, ctx, cfg, runner, now, inspect)
			if calls != 2 {
				t.Fatal("final inspection was not reached", calls, err)
			}
			if scenario == "current" {
				id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peer.ID)
				if err != nil || !proof.HostConfirmed(id) || !proof.Current(cfg, time.Now()) {
					t.Fatal("valid receipt rejected", err)
				}
			} else if err == nil || proof != nil {
				t.Fatal("late invalidation returned receipt", scenario, err)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("late cancellation lost", err)
			}
		})
	}
}
