package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExitObservationEventsInvalidateOnlyNormalizedTransitions(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ExitSelection = &ClientExitSelection{ID: "selected", Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	confirmed := false
	count := 0
	source := &clientRPCExitObservationSource{ctx: t.Context(), lock: &sync.Mutex{}, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
		count++
		if !m.mu.TryLock() {
			t.Fatal("observation under mutation lock")
		}
		m.mu.Unlock()
		if !confirmed {
			return nil, errors.New("private changing native output")
		}
		status := exitObservationTestStatus(profile.ProfileId, "selected")
		status.Metadata = &ipc.SnapshotMetadata{Revision: uint64(count), GeneratedAt: timestamppb.Now()}
		return status, nil
	}}
	s.exitObservation = source
	state := &clientRPCExitObservationEvents{source: source}
	if err := s.publishExitObservation(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := sub.next(ctx); err != nil {
		t.Fatal(err)
	}
	for _, next := range []bool{true, false, true} {
		confirmed = next
		before := m.Metadata().Revision
		if err := s.publishExitObservation(t.Context(), state); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("transition did not advance revision")
		}
		for {
			event, err := sub.next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if invalid := event.GetInvalidated(); invalid != nil {
				if invalid.Domain != ipc.Domain_DOMAIN_EXIT_NODE || invalid.ProfileId != profile.ProfileId {
					t.Fatal("wrong invalidation")
				}
				break
			}
		}
		if err := s.publishExitObservation(t.Context(), state); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("metadata/time or unchanged outcome emitted noise")
		}
	}
	confirmed = false
	if _, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile}); err != nil {
		t.Fatal(err)
	}
	// The digest cannot supply evidence to a read after native observation fails.
	status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
	if err != nil || status.FailClosed || status.EffectiveExitNodeId != nil {
		t.Fatal("event cache fabricated Get evidence")
	}
}

func TestExitObservationEventsDiscardStaleSourceScopeAndPending(t *testing.T) {
	for _, scenario := range []string{"scope", "source", "pending", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			source := &clientRPCExitObservationSource{ctx: ctx, lock: &sync.Mutex{}}
			source.observe = func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
				calls++
				switch scenario {
				case "scope":
					if err := m.store.Update(func(cfg *Config) error { cfg.NetworkID = "changed"; return nil }); err != nil {
						t.Fatal(err)
					}
				case "source":
					s.exitMu.Lock()
					s.exitObservation = nil
					s.exitMu.Unlock()
				case "cancel":
					cancel()
				}
				return nil, nil
			}
			s.exitObservation = source
			if scenario == "pending" {
				if err := m.store.Update(func(cfg *Config) error {
					cfg.RPCState.ExitChange = &clientRPCExitChange{ProfileID: profile.ProfileId}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.Metadata().Revision
			state := &clientRPCExitObservationEvents{source: source, previous: &[32]byte{1}}
			if err := s.publishExitObservation(t.Context(), state); err != nil {
				t.Fatal(err)
			}
			if m.Metadata().Revision != before {
				t.Fatal("stale/pending observation published")
			}
			if scenario == "pending" && calls != 0 {
				t.Fatal("pending state invoked native observer")
			}
		})
	}
}

func TestExitObservationEventsReauthorizeSubscribers(t *testing.T) {
	m, owner, _ := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	source := &clientRPCExitObservationSource{ctx: t.Context(), lock: &sync.Mutex{}, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) { return nil, nil }}
	s.exitObservation = source
	state := &clientRPCExitObservationEvents{source: source}
	if err := s.publishExitObservation(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "different-owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.publishExitObservation(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	sub.mu.Lock()
	closed := sub.closed
	failure := sub.err
	sub.mu.Unlock()
	if !closed || failure == nil {
		t.Fatal("old owner subscription received new scope events")
	}
}

func TestExitObservationEventsStopJoinsNativeCheck(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	entered, exited := make(chan struct{}), make(chan struct{})
	source := &clientRPCExitObservationSource{ctx: t.Context(), lock: &sync.Mutex{}, observe: func(ctx context.Context, _ Config) (*ipc.ExitNodeStatus, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}}
	s.exitObservation = source
	stop := s.startExitObservationEventTicks(t.Context(), source, nil, nil)
	awaitUnderlayLeaseSignal(t, entered)
	stop()
	select {
	case <-exited:
	default:
		t.Fatal("stop returned before observer finished")
	}
	stop()
}

func TestExitObservationEventsPublishInitialConfirmedAndAfterPending(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "confirmed"}[confirmed], func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ExitSelection = &ClientExitSelection{ID: "selected", Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			calls := 0
			source := &clientRPCExitObservationSource{ctx: t.Context(), lock: &sync.Mutex{}, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
				calls++
				if !confirmed {
					return nil, errors.New("unavailable")
				}
				return exitObservationTestStatus(profile.ProfileId, "selected"), nil
			}}
			s.exitObservation = source
			state := &clientRPCExitObservationEvents{source: source}
			sub, err := m.subscribe(owner, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if _, err := sub.next(ctx); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				before := m.Metadata().Revision
				if err := s.publishExitObservation(t.Context(), state); err != nil {
					t.Fatal(err)
				}
				want := before
				if confirmed {
					want++
				}
				if m.Metadata().Revision != want {
					t.Fatal("initial evidence transition lost or unknown baseline emitted")
				}
				if confirmed {
					for {
						event, err := sub.next(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if invalid := event.GetInvalidated(); invalid != nil {
							if invalid.Domain != ipc.Domain_DOMAIN_EXIT_NODE || invalid.ProfileId != profile.ProfileId {
								t.Fatal("wrong first evidence event")
							}
							break
						}
					}
				}
				if err := s.publishExitObservation(t.Context(), state); err != nil {
					t.Fatal(err)
				}
				if m.Metadata().Revision != want {
					t.Fatal("unchanged first evidence repeated")
				}
				if pass == 0 {
					if err := m.store.Update(func(cfg *Config) error {
						cfg.RPCState.ExitChange = &clientRPCExitChange{ProfileID: profile.ProfileId}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					beforeCalls := calls
					if err := s.publishExitObservation(t.Context(), state); err != nil {
						t.Fatal(err)
					}
					if state.previous != nil || calls != beforeCalls {
						t.Fatal("pending did not reset observation baseline")
					}
					if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ExitChange = nil; return nil }); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
