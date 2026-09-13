package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCEnrollmentExecutorRetriesAmbiguousResponseWithOriginalPlan(t *testing.T) {
	m, peer, request := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	var first ClientRPCEnrollmentInput
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, cfg Config, input ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		first = input
		cfg.PendingDirectRegistration = &PendingDirectRegistration{Origin: "https://control.test", Request: clientapi.RegisterNodeRequest{IdempotencyID: input.OperationID}}
		if err := save(cfg); err != nil {
			return nil, err
		}
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	})
	if err != nil || m.store.Read().RPCState.Enrollment == nil {
		t.Fatal("temporary failure discarded plan", err)
	}
	current, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || current.State != ipc.OperationState_OPERATION_STATE_RUNNING || current.Outcome != nil {
		t.Fatal("temporary failure was terminalized", err)
	}
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, cfg Config, input ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		if input != first || cfg.PendingDirectRegistration == nil || cfg.PendingDirectRegistration.Request.IdempotencyID != input.OperationID {
			t.Fatal("retry replaced original request")
		}
		cfg.PendingDirectRegistration = nil
		cfg.NodeID = "recovered-node"
		cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: cfg.NodeID}}
		return nil, save(cfg)
	})
	if err != nil || m.store.Read().RPCState.Enrollment != nil {
		t.Fatal("retry did not finish", err)
	}
}

func TestRPCEnrollmentActionRejectsUnsafeBrowserURLs(t *testing.T) {
	for _, address := range []string{"", "http://control.test/approve", "https://user:password@control.test/approve", "javascript:alert(1)", "https://control.test/\n"} {
		if validEnrollmentAction(&ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: address}) {
			t.Fatal("unsafe approval URL accepted")
		}
	}
}

func TestRPCEnrollmentExecutorWaitThenResume(t *testing.T) {
	m, peer, request := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, cfg Config, input ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		if input.OperationID != op.Id || input.Token != request.GetEnrollmentToken() {
			t.Fatal("runtime plan input lost")
		}
		cfg.EnrollmentRequestID = "approval-request"
		if err := save(cfg); err != nil {
			return nil, err
		}
		return &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: "https://control.test/approve"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || waiting.State != ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER {
		t.Fatal("approval did not persist waiting state")
	}
	// A fresh coordinator instance resumes the protected plan, not a caller replay.
	m, err = NewClientRPCMutations(m.store)
	if err != nil {
		t.Fatal(err)
	}
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, cfg Config, _ ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		if cfg.EnrollmentRequestID != "approval-request" {
			t.Fatal("pending approval lost")
		}
		cfg.NodeID = "verified-node"
		cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: cfg.NodeID}}
		return nil, save(cfg)
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || result.GetEnrollment().GetNodeId() != "verified-node" || m.store.Read().RPCState.Enrollment != nil {
		t.Fatal("resumed enrollment not completed")
	}
}

func TestRPCEnrollmentExecutorCancellationRetainsPlan(t *testing.T) {
	m, peer, request := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err = m.ReconcileEnrollment(ctx, func(_ context.Context, _ Config, _ ClientRPCEnrollmentInput, _ func(Config) error) (*ipc.UserAction, error) {
		cancel()
		return nil, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || m.store.Read().RPCState.Enrollment == nil {
		t.Fatal("lifecycle cancellation lost recoverable plan")
	}
	result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_RUNNING {
		t.Fatal("cancellation was made terminal")
	}
}

func TestRPCEnrollmentExecutorSanitizesProviderFailure(t *testing.T) {
	m, peer, request := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	err = m.ReconcileEnrollment(t.Context(), func(_ context.Context, _ Config, _ ClientRPCEnrollmentInput, _ func(Config) error) (*ipc.UserAction, error) {
		return nil, errors.New("synthetic-secret provider diagnostic")
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_FAILED || strings.Contains(result.String(), "synthetic-secret") || m.store.Read().RPCState.Enrollment != nil {
		t.Fatal("unsafe provider failure outcome")
	}
}
