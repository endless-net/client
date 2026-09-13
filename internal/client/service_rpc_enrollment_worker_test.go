package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCEnrollmentWorkerRecoveryAndShutdown(t *testing.T) {
	m, peer, req := enrollmentAdmissionTest(t)
	s := NewClientRPCService(m, nil)
	_, err := s.Enroll(t.Context(), connect.NewRequest(req))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	op, err := m.enrollAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	provider := func(ctx context.Context, _ Config, _ ClientRPCEnrollmentInput, _ func(Config) error) (*ipc.UserAction, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done, err := s.StartEnrollmentWorker(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not resume enrollment")
	}
	_, err = s.StartEnrollmentWorker(ctx, provider)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker failed to stop")
	}
	if m.store.Read().RPCState.Enrollment == nil {
		t.Fatal("shutdown discarded registration")
	}
	resumeCtx, stop := context.WithCancel(t.Context())
	defer stop()
	done, err = s.StartEnrollmentWorker(resumeCtx, func(_ context.Context, cfg Config, _ ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		cfg.NodeID = "resumed-node"
		cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: cfg.NodeID}}
		return nil, save(cfg)
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if result.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("resumed enrollment did not complete")
		}
	}
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("resumed worker failed to stop")
	}
}
