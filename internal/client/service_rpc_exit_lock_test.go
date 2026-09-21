package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitRequiresSharedEffectLockBeforeCheckpoint(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	before := clonePersistentConfig(m.store.Read())
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		t.Error("dispatched without shared effect lock")
		return nil, 0, errors.New("unexpected dispatch")
	}}
	assertRPCFailure(t, m.reconcileExitChange(t.Context(), executor), ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
		t.Fatal("unsupported executor changed durable work")
	}
}

func TestExitWaitsForSharedEffectsAndRechecksCancellation(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	before := clonePersistentConfig(m.store.Read())
	shared := &sync.Mutex{}
	shared.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: shared, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		t.Error("cancelled exit reached native effects")
		return nil, 0, errors.New("unexpected dispatch")
	}}
	go func() { done <- m.reconcileExitChange(ctx, executor) }()
	select {
	case err := <-done:
		shared.Unlock()
		t.Fatal("exit did not wait for ongoing OS work", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	shared.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("lost cancellation while waiting", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exit did not release cancelled attempt")
	}
	if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
		t.Fatal("cancelled wait committed a dispatch checkpoint")
	}
}

func TestExitSharedEffectLockCoversApplyAndContainment(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	shared := &sync.Mutex{}
	assertHeld := func() {
		if shared.TryLock() {
			shared.Unlock()
			t.Fatal("native effects are not serialized with connection/map work")
		}
	}
	contained := false
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: shared,
		Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			assertHeld()
			// Admission remains possible while OS work is serialized. It changes
			// the context and must cause containment under the same effect lock.
			if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
				t.Fatal(err)
			}
			return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
		},
		Contain: func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
			assertHeld()
			contained = true
			return clientRPCExitContainment{OperationID: plan.OperationID, ProfileID: plan.ProfileID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, IPv4Blocked: true, IPv6Blocked: true, ExitRoutesRemoved: true}, nil
		},
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	if !shared.TryLock() {
		t.Fatal("completed exit retained effect lock")
	}
	defer shared.Unlock()
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || !contained || result.State != ipc.OperationState_OPERATION_STATE_CANCELLED || m.store.Read().RPCState.ExitChange != nil {
		t.Fatal("effect lock released without completed containment", err)
	}
}
