package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitClearReleaseCheckpointSurvivesRestart(t *testing.T) {
	for _, outcome := range []string{"error", "partial", "cancel_after_release"} {
		t.Run(outcome, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ExitSelection = &ClientExitSelection{ID: "previous", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			m.now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			applies, releases := 0, 0
			lock := &sync.Mutex{}
			executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: lock,
				Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
					applies++
					return preparedExitClearTestStatus(profile.ProfileId), 0, nil
				},
				Release: func(_ context.Context, id string, input Config) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
					releases++
					if lock.TryLock() {
						lock.Unlock()
						t.Fatal("release escaped shared effect lock")
					}
					stored := reopenRPCStoreFromDisk(t, m.store).Read()
					if id != op.Id || !input.RPCState.ExitChange.Releasing || !stored.RPCState.ExitChange.Releasing || stored.ExitSelection == nil {
						t.Fatal("release preceded durable checkpoint or discarded ownership")
					}
					status := appliedExitTestStatus(profile.ProfileId, false)
					if releases == 1 {
						switch outcome {
						case "error":
							return nil, 0, errors.New("ambiguous release")
						case "partial":
							status.Ipv6.FailClosed = true
						case "cancel_after_release":
							cancel()
						}
					}
					return status, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
				},
			}
			err = m.reconcileExitChange(ctx, executor)
			if outcome == "cancel_after_release" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if cfg.ExitSelection == nil || cfg.RPCState.ExitChange == nil || !cfg.RPCState.ExitChange.Releasing {
				t.Fatal("uncertain release lost restart marker")
			}
			now = now.Add(10 * time.Second)
			m.now = func() time.Time { return now }
			executor.Apply = nil
			if err := m.reconcileExitChange(t.Context(), executor); err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || applies != 1 || releases != 2 || m.store.Read().RPCState.ExitChange != nil || m.store.Read().ExitSelection != nil {
				t.Fatal("restart failed to resume only release and commit original clear", err)
			}
		})
	}
}

func TestExitClearCannotReleaseWithoutClosedCheckpoint(t *testing.T) {
	for _, outcome := range []string{"already_open", "ipv4_open", "ipv6_open", "wrong_profile", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{},
				Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
					status := preparedExitClearTestStatus(profile.ProfileId)
					switch outcome {
					case "already_open":
						status = appliedExitTestStatus(profile.ProfileId, false)
					case "ipv4_open":
						status.Ipv4.FailClosed = false
					case "ipv6_open":
						status.Ipv6.FailClosed = false
					case "wrong_profile":
						status.ProfileId = "other"
					case "cancelled":
						cancel()
					}
					return status, 0, nil
				},
				Release: func(context.Context, string, Config) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
					t.Fatal("invalid checkpoint released firewall")
					return nil, 0, nil
				},
			}
			err = m.reconcileExitChange(ctx, executor)
			if outcome == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			stored := reopenRPCStoreFromDisk(t, m.store).Read()
			if stored.RPCState.ExitChange == nil || stored.RPCState.ExitChange.Releasing {
				t.Fatal("invalid result authorized release")
			}
			_, err = m.completeExitChange(op.Id, appliedExitTestStatus(profile.ProfileId, false), 0)
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		})
	}
}

func TestExitClearReleaseRejectsChangedCheckpointScope(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReconcileOperation(op.Id, func(cfg *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		recordExitTestProtection(cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.checkpointExitRelease(t.Context(), op.Id, preparedExitClearTestStatus(profile.ProfileId)); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "replacement"; return nil }); err != nil {
		t.Fatal(err)
	}
	input, err := m.exitReleaseInput(t.Context(), op.Id)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if input.RPCState != nil || m.store.Read().RPCState.ExitChange == nil {
		t.Fatal("changed context acquired release scope or lost recovery marker")
	}
}
