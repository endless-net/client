package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCDisconnectFullJournalIndependentOutcomesAndRetention(t *testing.T) {
	m := newRPCStoreTest(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://control.test"
	profile, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	// Seed terminal records directly to exercise real admission at capacity
	// without thousands of unrelated domain operations and atomic disk writes.
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = profile.ProfileId
		for len(cfg.RPCState.Operations) < rpcMaxOperationRecords {
			id, err := newRPCUUID()
			if err != nil {
				return err
			}
			encoded, err := proto.Marshal(&ipc.Operation{
				Id: id, RequestId: id, Kind: ipc.OperationKind_OPERATION_KIND_CONNECT,
				State:      ipc.OperationState_OPERATION_STATE_CANCELLED,
				Continuity: ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN,
				Outcome:    &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}},
			})
			if err != nil {
				return err
			}
			completed := now
			cfg.RPCState.Operations[id] = clientRPCOperationRecord{
				Owner: peer.Identity, Digest: make([]byte, 32), Operation: encoded, CompletedAt: &completed,
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := m.Metadata()
	prepared := false
	_, _, err = m.acceptAs(peer, rpcCreateProfile, rpcCreateRequest(t, m), func(*Config, *ipc.Operation) error {
		prepared = true
		return nil
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	if prepared || !proto.Equal(before, m.Metadata()) {
		t.Fatal("full journal rejection changed state")
	}
	first := &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profile.ProfileId}}
	firstOp, err := m.disconnectAs(peer, first)
	if err != nil {
		t.Fatal("full normal journal blocked Disconnect", err)
	}
	retry, err := m.disconnectAs(peer, first)
	if err != nil || !proto.Equal(retry, firstOp) {
		t.Fatal("full journal lost idempotent Disconnect", err)
	}
	second := &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profile.ProfileId}}
	_, err = m.disconnectAs(peer, second)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	if len(m.store.Read().RPCState.Operations) != rpcMaxOperationRecords+1 {
		t.Fatal("busy distinct request was retained or old work was evicted")
	}
	stops := 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	if err := m.ReconcileDisconnect(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	// The busy request was not accepted. Submit its same identity with fresh
	// revision after the serialized predecessor completes; never alias IDs.
	now = now.Add(time.Hour)
	second.Mutation.ExpectedRevision = m.Metadata().Revision
	secondOp, err := m.disconnectAs(peer, second)
	if err != nil || secondOp.Id == firstOp.Id || secondOp.RequestId != second.Mutation.RequestId {
		t.Fatal("distinct Disconnect lost its own identity", err)
	}
	if err := m.ReconcileDisconnect(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	if stops != 2 || len(m.store.Read().RPCState.Operations) != rpcMaxOperationRecords+2 {
		t.Fatal("separate outcomes were coalesced or old work was evicted")
	}
	store, err := OpenConfigStore(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now }
	lookup := func(id string) (*ipc.Operation, error) {
		return reopened.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: id}})
	}
	now = now.Add(23*time.Hour - time.Nanosecond)
	for _, expected := range []*ipc.Operation{firstOp, secondOp} {
		got, err := lookup(expected.RequestId)
		if err != nil || got.Id != expected.Id || got.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			t.Fatal("reopened full journal lost retained Disconnect outcome", err)
		}
	}
	now = now.Add(time.Nanosecond)
	_, err = lookup(firstOp.RequestId)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	if _, err := lookup(secondOp.RequestId); err != nil {
		t.Fatal("first expiry also expired the later independent outcome", err)
	}
	now = now.Add(time.Hour)
	_, err = lookup(secondOp.RequestId)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
}
