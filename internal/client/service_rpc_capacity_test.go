package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCNonterminalCapacityBeforeSideEffects(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	var first *ipc.CreateProfileRequest
	var firstOp *ipc.Operation
	for i := 0; i < rpcMaxNonterminalOperations; i++ {
		r := rpcCreateRequest(t, m)
		op, _, err := m.acceptAs(peer, rpcCreateProfile, r, rpcPrepareTest)
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		if i == 0 {
			first, firstOp = r, op
		}
	}
	before := m.Metadata()
	prepared := false
	_, _, err := m.acceptAs(peer, rpcCreateProfile, rpcCreateRequest(t, m), func(*Config, *ipc.Operation) error { prepared = true; return nil })
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	if prepared || before.Revision != m.Metadata().Revision || before.InstanceId != m.Metadata().InstanceId || len(m.store.Read().RPCState.Operations) != rpcMaxNonterminalOperations {
		t.Fatal("capacity rejection changed state")
	}
	retry, reused, err := m.acceptAs(peer, rpcCreateProfile, first, rpcPrepareTest)
	if err != nil || !reused || !proto.Equal(retry, firstOp) {
		t.Fatal("capacity rejected an idempotent retry", err)
	}
	conflict := proto.Clone(first).(*ipc.CreateProfileRequest)
	conflict.DisplayName = "conflict"
	_, _, err = m.acceptAs(peer, rpcCreateProfile, conflict, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	if _, err := m.ReconcileOperation(firstOp.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.acceptAs(peer, rpcCreateProfile, rpcCreateRequest(t, m), rpcPrepareTest); err != nil {
		t.Fatal("terminal operation did not release capacity", err)
	}
	if len(m.store.Read().RPCState.Operations) != rpcMaxNonterminalOperations+1 {
		t.Fatal("terminal journal record was discarded to release capacity")
	}
}
