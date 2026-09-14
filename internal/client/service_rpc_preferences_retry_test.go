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
	"google.golang.org/protobuf/proto"
)

func TestPreferenceWorkerRetriesTemporaryCleanupWithoutReplay(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var starts, stops atomic.Int32
	first := make(chan struct{})
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { starts.Add(1); return errors.New("synthetic apply failure") }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		if stops.Add(1) == 1 {
			close(first)
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	done, err := s.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not begin cleanup")
	}
	if cfg := m.store.Read(); cfg.RPCState.NetworkPreferenceChange == nil || !cfg.RPCState.NetworkPreferenceChange.Containing || cfg.NetworkPreferences != nil {
		t.Fatal("retry lost durable containment")
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			t.Fatal("temporary cleanup stopped worker")
		case <-deadline.C:
			t.Fatal("cleanup did not retry automatically")
		case <-tick.C:
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			if !rpcOperationTerminal(result.State) {
				continue
			}
			if result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || starts.Load() != 1 || stops.Load() != 2 || m.store.Read().NetworkPreferences != nil {
				t.Fatal("retry changed cause, reapplied candidate or committed failed preference")
			}
			return
		}
	}
}

func TestPreferenceCleanupRetryClassification(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	for _, containing := range []bool{false, true} {
		if err := m.store.Update(func(cfg *Config) error {
			cfg.RPCState.NetworkPreferenceChange = &clientRPCNetworkPreferenceChange{Containing: containing}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for _, code := range []ipc.ErrorCode{ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, ipc.ErrorCode_ERROR_CODE_STALE_STATE, ipc.ErrorCode_ERROR_CODE_INTERNAL} {
			want := containing && (code == ipc.ErrorCode_ERROR_CODE_UNAVAILABLE || code == ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED || code == ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
			if m.preferenceCleanupRetryable(rpc.Error(connect.CodeUnavailable, code)) != want {
				t.Fatal("wrong retry classification")
			}
		}
		if m.preferenceCleanupRetryable(errors.New("unavailable")) || m.preferenceCleanupRetryable(context.Canceled) {
			t.Fatal("raw or cancellation error treated as retryable")
		}
	}
}
