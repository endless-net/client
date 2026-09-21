package client

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type exitLANTestStream struct {
	changed chan struct{}
	once    sync.Once
	err     error
	closed  bool
}

func (s *exitLANTestStream) Changed() <-chan struct{} { return s.changed }
func (s *exitLANTestStream) Err() error               { return s.err }
func (s *exitLANTestStream) Close() error {
	s.closed = true
	s.once.Do(func() { close(s.changed) })
	return nil
}

func exitLANTestLifetime(t *testing.T) *exitLANSourceLifetime {
	t.Helper()
	stream := &exitLANTestStream{changed: make(chan struct{})}
	t.Cleanup(func() { _ = stream.Close() })
	return &exitLANSourceLifetime{ctx: t.Context(), stream: stream}
}

func TestExitLANWatchCaptureOwnershipAndInvalidation(t *testing.T) {
	for _, scenario := range []string{"stable", "before", "during", "lost", "capture_error", "wrong_scope", "cancelled", "open_error", "no_stream"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stream := &exitLANTestStream{changed: make(chan struct{})}
			opened, captured := false, false
			open := func(context.Context) (exitLANChangeStream, error) {
				opened = true
				if scenario == "before" {
					_ = stream.Close()
				}
				if scenario == "open_error" {
					return stream, errExitLANSource
				}
				if scenario == "no_stream" {
					return nil, nil
				}
				return stream, nil
			}
			capture := func(_ context.Context, own string) (*exitLANSource, error) {
				if !opened {
					t.Fatal("snapshot preceded subscription")
				}
				captured = true
				switch scenario {
				case "during":
					_ = stream.Close()
				case "lost":
					stream.err = errors.New("receive loss")
				case "capture_error":
					return nil, errExitLANSource
				case "wrong_scope":
					own = "foreign"
				case "cancelled":
					cancel()
				}
				return &exitLANSource{OwnInterface: own}, nil
			}
			source, err := captureWatchedExitLANSource(ctx, "endlessnet", open, capture)
			if scenario != "stable" {
				if err == nil || source != nil {
					t.Fatal("invalid observation survived", err)
				}
				if scenario != "no_stream" && !stream.closed {
					t.Fatal("abandoned subscription leaked")
				}
				if (scenario == "before" || scenario == "open_error" || scenario == "no_stream") && captured {
					t.Fatal("snapshot ran without live subscription")
				}
				return
			}
			if err != nil || !source.lifetime.current() || stream.closed {
				t.Fatal("stable receipt lost", err)
			}
			copy := *source
			if err := source.close(); err != nil || copy.lifetime.current() {
				t.Fatal("copy outlived subscription", err)
			}
			source.lifetime = exitLANTestLifetime(t)
			if copy.lifetime.current() {
				t.Fatal("replacement revived original receipt")
			}
		})
	}
}

func TestExitLANPreparationRejectsTopologyLossInsideReadback(t *testing.T) {
	for _, scenario := range []string{"absent", "during", "after", "expiry"} {
		t.Run(scenario, func(t *testing.T) {
			e, cfg, now, inspect := exitLANHealthFixture(t, false)
			e.mu.Lock()
			defer e.mu.Unlock()
			topology := &exitLANSource{OwnInterface: e.interface_, ValidUntil: now.Add(time.Minute), Links: []exitLANLink{{Index: 2, LinkIndex: 2, Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}, Routes: []exitLANDirectRoute{{Prefix: netip.MustParsePrefix("192.0.2.0/24")}}}}}
			if scenario != "absent" {
				topology.lifetime = exitLANTestLifetime(t)
			}
			calls := 0
			modified := func(e *WireGuardEngine) (WireGuardInspection, error) {
				calls++
				if scenario == "during" && calls == 2 {
					_ = topology.close()
				}
				return inspect(e)
			}
			plan, _, err := e.prepareExitLANWithInspection(t.Context(), cfg, topology, now, modified)
			if scenario == "absent" || scenario == "during" {
				if err == nil || plan != nil {
					t.Fatal("unconfirmed topology produced plan")
				}
				if scenario == "during" && calls != 2 {
					t.Fatal("final readback not reached")
				}
				return
			}
			if err != nil || !plan.topologyCurrent(now) {
				t.Fatal("live plan rejected", err)
			}
			if scenario == "expiry" {
				if plan.topologyCurrent(plan.expires) {
					t.Fatal("expired plan survived")
				}
				return
			}
			_ = topology.close()
			if plan.topologyCurrent(now) {
				t.Fatal("cloned plan survived topology loss")
			}
			if _, err := compileExitLANPlan(cfg, *cfg.CachedMap, cfg.ExitSelection, topology, nil, now); err == nil {
				t.Fatal("closed topology recompiled")
			}
			topology.lifetime = exitLANTestLifetime(t)
			if plan.topologyCurrent(now) {
				t.Fatal("caller replacement revived old plan")
			}
		})
	}
}
