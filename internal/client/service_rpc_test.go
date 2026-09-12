package client

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

const rpcCreateProfile = "/client.v0.ClientService/CreateProfile"

func newRPCStoreTest(t *testing.T) *ClientRPCMutations {
	t.Helper()
	store, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func rpcCreateRequest(t *testing.T, m *ClientRPCMutations) *ipc.CreateProfileRequest {
	t.Helper()
	id, err := newRPCUUID()
	if err != nil {
		t.Fatal(err)
	}
	metadata := m.Metadata()
	return &ipc.CreateProfileRequest{Mutation: &ipc.MutationContext{RequestId: id, ExpectedInstanceId: metadata.InstanceId, ExpectedRevision: metadata.Revision}, DisplayName: "test"}
}

func rpcPrepareTest(cfg *Config, operation *ipc.Operation) error {
	// Stand-in domain intent; it must commit atomically with the journal record.
	cfg.ExitLANPolicy = ExitLANPolicyBlock
	operation.ProfileId = "profile"
	return nil
}

func assertRPCFailure(t *testing.T, err error, code ipc.ErrorCode) {
	t.Helper()
	if rpc.FailureFromError(err).GetCode() != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestRPCOwnershipClaimOneWinner(t *testing.T) {
	m := newRPCStoreTest(t)
	requests := []*ipc.CreateProfileRequest{rpcCreateRequest(t, m), rpcCreateRequest(t, m)}
	peers := []local.Peer{{Identity: "uid:1000"}, {Identity: "uid:1001"}}
	results := make([]error, 2)
	var wg sync.WaitGroup
	for i := range peers {
		wg.Go(func() { _, _, results[i] = m.acceptAs(peers[i], rpcCreateProfile, requests[i], rpcPrepareTest) })
	}
	wg.Wait()
	winners := 0
	for i, err := range results {
		if err == nil {
			winners++
			if m.store.Read().LocalOwnerID != peers[i].Identity {
				t.Fatal("operation and owner committed separately")
			}
		} else {
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
		}
	}
	if winners != 1 || len(m.store.Read().RPCState.Operations) != 1 {
		t.Fatalf("claim winners=%d", winners)
	}
}

func TestRPCClaimAuthorizationAndRollback(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	_, _, err := m.Accept(t.Context(), rpcCreateProfile, request, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	_, _, err = m.acceptAs(peer, rpcCreateProfile, request, func(cfg *Config, _ *ipc.Operation) error {
		cfg.ExitLANPolicy = ExitLANPolicyBlock
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	cfg := m.store.Read()
	if cfg.LocalOwnerID != "" || cfg.RPCState != nil || cfg.ExitLANPolicy != "" {
		t.Fatal("failed prepare leaked ownership, intent or acceptance")
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "existing-enrollment"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, _, err = m.acceptAs(peer, rpcCreateProfile, request, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED)
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/Connect"), Config{}); err == nil {
		t.Fatal("Connect claimed ownership")
	}
}

func TestRPCDurableReplayBeforeCAS(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	first, reused, err := m.acceptAs(peer, rpcCreateProfile, request, rpcPrepareTest)
	if err != nil || reused {
		t.Fatalf("accept: %v reused=%v", err, reused)
	}
	loaded, err := loadConfigFile(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewClientRPCMutations(&ConfigStore{path: m.store.path, config: loaded, revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.instanceID == m.instanceID {
		t.Fatal("restart reused instance identity")
	}
	second, reused, err := restarted.acceptAs(peer, rpcCreateProfile, request, func(*Config, *ipc.Operation) error { t.Fatal("replay ran preparation"); return nil })
	if err != nil || !reused || !proto.Equal(first, second) {
		t.Fatalf("replay: %v reused=%v", err, reused)
	}
	request.Mutation.ExpectedInstanceId = restarted.instanceID
	request.Mutation.ExpectedRevision = restarted.Metadata().Revision
	_, reused, err = restarted.acceptAs(peer, rpcCreateProfile, request, rpcPrepareTest)
	if err != nil || !reused {
		t.Fatalf("CAS metadata changed semantic digest: %v", err)
	}
	request.DisplayName = "different"
	_, _, err = restarted.acceptAs(peer, rpcCreateProfile, request, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	newRequest := rpcCreateRequest(t, restarted)
	newRequest.Mutation.ExpectedInstanceId = m.instanceID
	_, _, err = restarted.acceptAs(peer, rpcCreateProfile, newRequest, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
}

func TestRPCRequestDigestDoesNotPersistEnrollmentToken(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := &ipc.EnrollRequest{Mutation: rpcCreateRequest(t, m).Mutation, Authentication: &ipc.EnrollRequest_EnrollmentToken{EnrollmentToken: "test-only-sensitive-token"}}
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/Enroll", request, rpcPrepareTest)
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != ipc.OperationKind_OPERATION_KIND_ENROLL {
		t.Fatal("wrong operation kind")
	}
	state := m.store.Read().RPCState
	if len(state.DigestKey) != 32 {
		t.Fatal("missing keyed digest")
	}
	for _, record := range state.Operations {
		if len(record.Digest) != 32 || bytes.Contains(record.Operation, []byte(request.GetEnrollmentToken())) {
			t.Fatal("journal retained token")
		}
	}
	otherMethod := &ipc.CreateProfileRequest{Mutation: request.Mutation}
	_, _, err = m.acceptAs(peer, rpcCreateProfile, otherMethod, rpcPrepareTest)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}

func TestRPCOperationLifecycleAndRetention(t *testing.T) {
	m := newRPCStoreTest(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	op, _, err := m.acceptAs(peer, rpcCreateProfile, request, rpcPrepareTest)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: request.Mutation.RequestId}}
	_, err = m.operationAs(local.Peer{Identity: "administrator", Administrator: true}, lookup)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	now = now.Add(48 * time.Hour)
	if _, err := m.operationAs(peer, lookup); err != nil {
		t.Fatalf("pending operation expired: %v", err)
	}
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		return nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER
		return nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: "profile"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	now = now.Add(24*time.Hour - time.Nanosecond)
	if _, err := m.operationAs(peer, lookup); err != nil {
		t.Fatalf("terminal expired before 24h: %v", err)
	}
	now = now.Add(time.Nanosecond)
	_, err = m.operationAs(peer, lookup)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
}

func TestRPCInvalidMutationCannotClaim(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	for _, mutation := range []*ipc.MutationContext{nil, {}, {RequestId: "not-uuid", ExpectedInstanceId: m.instanceID, ExpectedRevision: 1}} {
		_, _, err := m.acceptAs(peer, rpcCreateProfile, &ipc.CreateProfileRequest{Mutation: mutation}, rpcPrepareTest)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if m.store.Read().LocalOwnerID != "" {
		t.Fatal("invalid request claimed owner")
	}
	if _, err := NewClientRPCMutations(nil); err == nil {
		t.Fatal("accepted nil store")
	}
}
