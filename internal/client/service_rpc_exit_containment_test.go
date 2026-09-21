package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitContainmentRequiresBoundProofAndRecovers(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	applies, contains := 0, 0
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{}, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		applies++
		if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
			t.Fatal(err)
		}
		return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}, Contain: func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
		contains++
		stored := reopenRPCStoreFromDisk(t, m.store).Read()
		if !stored.RPCState.ExitChange.Containing || stored.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
			t.Fatal("containment preceded durable checkpoint")
		}
		return clientRPCExitContainment{}, errors.New("native failure must remain private")
	}}
	assertRPCFailure(t, m.reconcileExitChange(t.Context(), executor), ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	executor.Apply = nil // Recovery must not require or redispatch Apply.
	for _, mode := range []string{"ipv4", "ipv6", "routes", "wrong_id", "wrong_profile", "wrong_node", "wrong_network", "complete"} {
		executor.Contain = func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
			contains++
			proof := clientRPCExitContainment{OperationID: plan.OperationID, ProfileID: plan.ProfileID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, IPv4Blocked: true, IPv6Blocked: true, ExitRoutesRemoved: true}
			switch mode {
			case "ipv4":
				proof.IPv4Blocked = false
			case "ipv6":
				proof.IPv6Blocked = false
			case "routes":
				proof.ExitRoutesRemoved = false
			case "wrong_id":
				proof.OperationID = "different"
			case "wrong_profile":
				proof.ProfileID = "different"
			case "wrong_node":
				proof.NodeID = "different"
			case "wrong_network":
				proof.NetworkID = "different"
			}
			return proof, nil
		}
		err = m.reconcileExitChange(t.Context(), executor)
		if mode != "complete" {
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			if m.store.Read().RPCState.ExitChange == nil {
				t.Fatal("invalid proof released guard")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_CANCELLED || result.GetFailure().GetReasonKey() != "exit_superseded_by_disconnect" || m.store.Read().RPCState.ExitChange != nil || applies != 1 || contains != 9 {
		t.Fatal("containment recovery lost cause or redispatched", err, result)
	}
	if m.store.Read().ConnectionIntent.Reason != "user_disconnect" {
		t.Fatal("containment overwrote user intent")
	}
}
