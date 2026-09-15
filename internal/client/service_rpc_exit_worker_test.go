package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitWorkerResumesAndRetriesWithoutRequestReplay(t *testing.T) {
	for _, firstResult := range []string{"error", "missing", "partial"} {
		t.Run(firstResult, func(t *testing.T) { testExitWorkerResumesAndRetries(t, firstResult) })
	}
}

func testExitWorkerResumesAndRetries(t *testing.T, firstResult string) {
	t.Helper()
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ExitSelection = &ClientExitSelection{ID: "previous", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	service := NewClientRPCService(m, nil)
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	first := make(chan struct{})
	executor := clientRPCExitExecutor{Lock: &sync.Mutex{},
		Apply: func(_ context.Context, id string, cfg Config, _ *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			if id != op.Id || cfg.RPCState.ExitChange.OperationID != op.Id {
				t.Error("retry changed durable operation identity")
			}
			if calls.Add(1) == 1 {
				close(first)
				switch firstResult {
				case "missing":
					return nil, 0, nil
				case "partial":
					status := appliedExitTestStatus(profile.ProfileId, false)
					status.Ipv6.ApplyState = ipc.ApplyState_APPLY_STATE_PENDING
					return status, 0, nil
				default:
					return nil, 0, errors.New("ambiguous native apply")
				}
			}
			return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
		},
		Contain: func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error) {
			t.Error("unexpected containment")
			return clientRPCExitContainment{}, errors.New("unexpected containment")
		},
	}
	done, err := service.startExitWorker(ctx, executor)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("startup did not resume saved work")
	}
	stored := m.store.Read()
	if stored.ExitSelection == nil || stored.ExitSelection.ID != "previous" || stored.RPCState.ExitChange == nil || stored.RPCState.ExitChange.NextAttemptAt.IsZero() {
		t.Fatal("incomplete clear discarded previous selection or retry checkpoint")
	}
	if _, err := service.startExitWorker(ctx, executor); err == nil {
		t.Fatal("started duplicate exit worker")
	}
	deadline := time.Now().Add(8 * time.Second)
	for m.store.Read().RPCState.ExitChange != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || calls.Load() != 2 || m.store.Read().ExitSelection != nil {
		t.Fatal("worker did not retry and complete original operation", err, result, calls.Load())
	}
}

func TestExitWorkerCancellationRetainsDispatchedWork(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	service := NewClientRPCService(m, nil)
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	executor := clientRPCExitExecutor{Lock: &sync.Mutex{},
		Apply: func(ctx context.Context, _ string, _ Config, _ *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			close(entered)
			<-ctx.Done()
			return nil, 0, ctx.Err()
		},
		Contain: func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error) {
			t.Error("shutdown must not remove protection")
			return clientRPCExitContainment{}, nil
		},
	}
	done, err := service.startExitWorker(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not dispatch")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not join")
	}
	stored := reopenRPCStoreFromDisk(t, m.store).Read()
	if stored.RPCState.ExitChange == nil || stored.RPCState.ExitChange.NextAttemptAt.IsZero() {
		t.Fatal("shutdown lost ambiguous native effects or retry checkpoint")
	}
	service.exitMu.Lock()
	active := service.exitWorker != nil
	service.exitMu.Unlock()
	if active {
		t.Fatal("joined worker retained ownership")
	}
}

func TestExitWorkerRequiresCompleteAdapter(t *testing.T) {
	m, _, _ := rpcConnectFixture(t)
	service := NewClientRPCService(m, nil)
	for _, executor := range []clientRPCExitExecutor{{}, {Lock: &sync.Mutex{}}, {Lock: &sync.Mutex{}, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		return nil, 0, nil
	}}} {
		_, err := service.startExitWorker(t.Context(), executor)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	for _, code := range []ipc.ErrorCode{ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, ipc.ErrorCode_ERROR_CODE_INTERNAL, ipc.ErrorCode_ERROR_CODE_STALE_STATE, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED} {
		want := code == ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || code == ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED || code == ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED
		if exitWorkerRetryable(rpc.Error(connect.CodeUnavailable, code)) != want {
			t.Fatal("wrong worker failure classification", code)
		}
	}
	if exitWorkerRetryable(errors.New("unexpected error")) || exitWorkerRetryable(context.Canceled) {
		t.Fatal("unexpected worker errors must stop the host")
	}
}
