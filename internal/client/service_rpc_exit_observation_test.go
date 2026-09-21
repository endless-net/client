package client

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func exitObservationTestStatus(profile, id string) *ipc.ExitNodeStatus {
	family := func() *ipc.ExitFamilyStatus {
		return &ipc.ExitFamilyStatus{RequestedExitNodeId: proto.String(id), EffectiveExitNodeId: proto.String(id), ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: true}
	}
	return &ipc.ExitNodeStatus{ProfileId: profile, RequestedExitNodeId: proto.String(id), EffectiveExitNodeId: proto.String(id), RequestedFamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, RequestedLanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK, EffectiveLanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK, ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: true, Ipv4: family(), Ipv6: family()}
}

func TestExitObservationPublishesOnlyMatchingLiveEnforcement(t *testing.T) {
	for _, scenario := range []string{"applied", "error", "mismatch", "stopped", "source_replaced", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ExitSelection = &ClientExitSelection{ID: "selected", Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}
				if scenario == "pending" {
					cfg.RPCState.ExitChange = &clientRPCExitChange{ProfileID: profile.ProfileId, Requested: cloneExitSelection(cfg.ExitSelection)}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			source := &clientRPCExitObservationSource{ctx: ctx, lock: &sync.Mutex{}}
			source.observe = func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
				calls++
				if !m.mu.TryLock() {
					t.Fatal("native observation called under mutation lock")
				}
				m.mu.Unlock()
				if source.lock.TryLock() {
					source.lock.Unlock()
					t.Fatal("native observation ran outside effect lock")
				}
				if scenario == "error" {
					return nil, errors.New("private native command output")
				}
				if scenario == "source_replaced" {
					s.exitMu.Lock()
					s.exitObservation = nil
					s.exitMu.Unlock()
				}
				id := "selected"
				if scenario == "mismatch" {
					id = "another"
				}
				return exitObservationTestStatus(profile.ProfileId, id), nil
			}
			s.exitObservation = source
			if scenario == "stopped" {
				cancel()
			}
			status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if status.GetRequestedExitNodeId() != "selected" || status.Metadata == nil {
				t.Fatal("durable intent or metadata replaced")
			}
			if scenario == "applied" {
				if status.ApplyState != ipc.ApplyState_APPLY_STATE_APPLIED || status.GetEffectiveExitNodeId() != "selected" || !status.FailClosed || !status.Ipv4.FailClosed || !status.Ipv6.FailClosed || status.Failure != nil {
					t.Fatal("confirmed enforcement not projected", status)
				}
			} else if status.EffectiveExitNodeId != nil || status.FailClosed || status.ApplyState == ipc.ApplyState_APPLY_STATE_APPLIED {
				t.Fatal("unconfirmed observation published", status)
			}
			if (scenario == "stopped" || scenario == "pending") && calls != 0 {
				t.Fatal("unavailable source or pending operation invoked observer")
			}
			if strings.Contains(status.String(), "private") {
				t.Fatal("native error text leaked")
			}
		})
	}
}

func TestExitObservationAuthorizesBeforeNativeAndAfterConcurrentChange(t *testing.T) {
	for _, scenario := range []string{"unauthorized", "stale", "owner_changed"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ExitSelection = &ClientExitSelection{ID: "selected", Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			calls := 0
			s.exitObservation = &clientRPCExitObservationSource{ctx: t.Context(), lock: &sync.Mutex{}, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
				calls++
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "owner_changed" {
						cfg.LocalOwnerID = "different-owner"
					} else {
						cfg.RPCState.Revision++
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return exitObservationTestStatus(profile.ProfileId, "selected"), nil
			}}
			caller := owner
			if scenario == "unauthorized" {
				caller = local.Peer{Identity: "other"}
			}
			status, err := s.exitNodeAs(t.Context(), caller, &ipc.GetExitNodeRequest{Profile: profile})
			if status != nil || err == nil {
				t.Fatal("changed or unauthorized observation published")
			}
			if scenario == "stale" {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			} else {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
			}
			if scenario == "unauthorized" && calls != 0 {
				t.Fatal("unauthorized read invoked native observer")
			}
		})
	}
}

func TestExitObservationEffectLockWaitHonorsCancellation(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	lock := &sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()
	s.exitObservation = &clientRPCExitObservationSource{ctx: t.Context(), lock: lock, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
		t.Error("cancelled waiter invoked native observer")
		return nil, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.exitNodeAs(ctx, owner, &ipc.GetExitNodeRequest{Profile: profile}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestExitObservationReauthorizesAfterCancelledEffectLockWait(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ExitSelection = &ClientExitSelection{ID: "private-selection", Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	lock := &sync.Mutex{}
	lock.Lock()
	defer lock.Unlock()
	sourceCtx, stopSource := context.WithCancel(t.Context())
	defer stopSource()
	bound := make(chan struct{})
	s.exitObservation = &clientRPCExitObservationSource{ctx: &exitObservationNotifyingContext{Context: sourceCtx, bound: bound}, lock: lock, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
		t.Error("cancelled source invoked observer")
		return nil, nil
	}}
	type result struct {
		status *ipc.ExitNodeStatus
		err    error
	}
	done := make(chan result, 1)
	go func() {
		status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
		done <- result{status, err}
	}()
	awaitUnderlayLeaseSignal(t, bound)
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "different-owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	stopSource()
	select {
	case result := <-done:
		if result.status != nil {
			t.Fatal("revoked caller received old private intent")
		}
		assertRPCFailure(t, result.err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	case <-time.After(3 * time.Second):
		t.Fatal("observation did not stop")
	}
}

type exitObservationNotifyingContext struct {
	context.Context
	once  sync.Once
	bound chan struct{}
}

func (c *exitObservationNotifyingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.bound) })
	return c.Context.Done()
}

func TestExitWorkerObservationSourceIsClearedOnStop(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	ctx, cancel := context.WithCancel(t.Context())
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{}, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		return nil, 0, nil
	}, Contain: func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error) {
		return clientRPCExitContainment{}, nil
	}, Release: releaseExitTestCallback, Observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) { return nil, nil }}
	done, err := s.startExitWorker(ctx, executor)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s.exitMu.Lock()
	source := s.exitObservation
	s.exitMu.Unlock()
	if source == nil {
		cancel()
		<-done
		t.Fatal("worker did not publish observation source")
	}
	cancel()
	<-done
	s.exitMu.Lock()
	remaining := s.exitObservation
	s.exitMu.Unlock()
	if remaining != nil || source.ctx.Err() == nil {
		t.Fatal("stopped worker retained a live observation source")
	}
}
