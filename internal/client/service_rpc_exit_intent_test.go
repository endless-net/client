package client

import (
	"context"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitApplyCannotCommitAfterAcceptedDisconnect(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	accepted, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	executor := clientRPCExitExecutor{Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		calls++
		if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
			t.Fatal(err)
		}
		return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	err = m.reconcileExitChange(t.Context(), executor)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	cfg := m.store.Read()
	if cfg.RPCState.ExitChange == nil || cfg.RPCState.ExitChange.PreviousIntent.DesiredState != ConnectionIntentDesiredConnected || cfg.ConnectionIntent.Reason != "user_disconnect" {
		t.Fatal("exit completion consumed disconnect or lost recovery guard")
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: accepted.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_RUNNING {
		t.Fatal("stale applied exit falsely became terminal", err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	err = m.reconcileExitChange(t.Context(), executor)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if calls != 1 || m.store.Read().RPCState.ExitChange == nil {
		t.Fatal("restart redispatched superseded exit or lost guard")
	}
}
