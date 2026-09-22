package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

type resourceHostTopologyTestStream struct {
	changed chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func (s *resourceHostTopologyTestStream) Changed() <-chan struct{} { return s.changed }
func (s *resourceHostTopologyTestStream) Err() error               { return nil }
func (s *resourceHostTopologyTestStream) invalidate()              { s.once.Do(func() { close(s.changed) }) }
func (s *resourceHostTopologyTestStream) Close() error             { s.closes.Add(1); s.invalidate(); return nil }

// Actual engine/device setup is retained; only native topology input and a
// captured authenticated handshake are injected, as in the existing observer
// fixtures. No native route, watch socket or remote application is contacted.
func resourceHostTopologyFixture(t *testing.T) (Config, *WireGuardEngine, time.Time, CommandRunner, func(*WireGuardEngine) (WireGuardInspection, error)) {
	t.Helper()
	cfg, e, _ := resourceHostProjectionFixture(t)
	now := time.Now().UTC()
	handshake := now.Add(-time.Second)
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
	inspect := func(e *WireGuardEngine) (WireGuardInspection, error) {
		value, err := resourceObservedUAPI(e)
		for i := range value.Peers {
			value.Peers[i].LatestHandshakeUnix = handshake.Unix()
			value.Peers[i].latestHandshakeNanos = int64(handshake.Nanosecond())
			value.Peers[i].handshakeTimeComplete = true
		}
		return value, err
	}
	return cfg, e, now, runner, inspect
}

func TestResourceHostTopologyCollectorRejectsLostObservation(t *testing.T) {
	for _, scenario := range []string{"stable", "watch_error", "partial_watch_error", "watch_closed", "batch", "final_readback", "cancel", "inspect_error", "route_error"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, now, runner, inspect := resourceHostTopologyFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := &resourceHostTopologyTestStream{changed: make(chan struct{})}
			opened := false
			open := func(context.Context) (exitLANChangeStream, error) {
				opened = true
				if scenario == "watch_error" {
					return nil, errResourceHostObservation
				}
				if scenario == "partial_watch_error" {
					return stream, errResourceHostObservation
				}
				if scenario == "watch_closed" {
					stream.invalidate()
				}
				return stream, nil
			}
			routeReads := 0
			read := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if !opened {
					t.Fatal("native snapshot preceded watcher")
				}
				if strings.Contains(strings.Join(args, " "), "route get") {
					routeReads++
					if scenario == "batch" {
						stream.invalidate()
					}
					if scenario == "route_error" {
						return nil, errResourceHostObservation
					}
				}
				return runner(ctx, name, args...)
			}
			inspections := 0
			readDevice := func(engine *WireGuardEngine) (WireGuardInspection, error) {
				inspections++
				value, err := inspect(engine)
				if inspections == 2 {
					switch scenario {
					case "final_readback":
						stream.invalidate()
					case "cancel":
						cancel()
					case "inspect_error":
						return WireGuardInspection{}, errResourceHostObservation
					}
				}
				return value, err
			}
			proof, err := e.observeResourceHostsWithTopology(ctx, cfg, read, now, readDevice, open)
			if scenario == "route_error" {
				if err != nil || proof == nil || proof.HostConfirmed(rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)) || routeReads == 0 {
					t.Fatal("failed route produced positive HOST evidence", err)
				}
				_ = proof.Close()
				_ = proof.Close()
				if stream.closes.Load() != 1 {
					t.Fatal("empty receipt did not release watcher exactly once")
				}
				return
			}
			if scenario == "stable" {
				if err != nil || proof == nil || !proof.Current(cfg, time.Now()) || !proof.HostConfirmed(rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)) || stream.closes.Load() != 0 {
					t.Fatal("live topology receipt failed", err)
				}
				_ = proof.Close()
				_ = proof.Close()
				if proof.Current(cfg, time.Now()) || stream.closes.Load() != 1 {
					t.Fatal("receipt Close did not revoke/release exactly once")
				}
				return
			}
			if err == nil || proof != nil {
				t.Fatal("lost topology returned HOST receipt", scenario)
			}
			wantCloses := int32(1)
			if scenario == "watch_error" {
				wantCloses = 0
			}
			if stream.closes.Load() != wantCloses {
				t.Fatal("failed collection leaked/reclosed watcher", stream.closes.Load())
			}
			if scenario == "batch" && routeReads == 0 {
				t.Fatal("batch invalidation not exercised")
			}
			if (scenario == "final_readback" || scenario == "cancel" || scenario == "inspect_error") && inspections != 2 {
				t.Fatal("final readback not exercised")
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("late cancellation lost", err)
			}
		})
	}
}

func TestResourceHostTopologyCurrentRejectsChangesAndCancellation(t *testing.T) {
	for _, scenario := range []string{"before_current", "during_current", "context_cancel", "copied_close"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, e, now, runner, inspect := resourceHostTopologyFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := &resourceHostTopologyTestStream{changed: make(chan struct{})}
			proof, err := e.observeResourceHostsWithTopology(ctx, cfg, runner, now, inspect, func(context.Context) (exitLANChangeStream, error) { return stream, nil })
			if err != nil || proof == nil || !proof.Current(cfg, time.Now()) {
				t.Fatal("baseline topology proof unavailable", err)
			}
			switch scenario {
			case "before_current":
				stream.invalidate()
			case "context_cancel":
				cancel()
			case "copied_close":
				copy := *proof
				_ = copy.Close()
			}
			called := false
			current := proof.currentWithInspection(cfg, time.Now(), func(engine *WireGuardEngine) (WireGuardInspection, error) {
				called = true
				value, err := resourceObservedUAPI(engine)
				if scenario == "during_current" {
					stream.invalidate()
				}
				return value, err
			})
			if current || scenario == "during_current" && !called {
				t.Fatal("topology loss did not invalidate Current")
			}
			_ = proof.Close()
			_ = proof.Close()
			if stream.closes.Load() != 1 {
				t.Fatal("receipt copies/repeated Close leaked or double-closed watcher")
			}
		})
	}
}
