package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type serviceOperationReadFunc func(context.Context, *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error)

func (f serviceOperationReadFunc) GetOperation(ctx context.Context, req *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
	return f(ctx, req)
}

func TestWaitServiceOperationOnlyReadsOriginalIdentity(t *testing.T) {
	initial := &ipc.Operation{Id: "operation", RequestId: "request", ProfileId: "profile", Kind: ipc.OperationKind_OPERATION_KIND_ENROLL, State: ipc.OperationState_OPERATION_STATE_PENDING}
	for _, scenario := range []string{"success", "failure", "identity change", "read failure", "cancel", "writer failure", "cancelled operation", "nil response", "nil message", "nil operation"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, reports := 0, 0
			reader := serviceOperationReadFunc(func(_ context.Context, req *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
				calls++
				if req.Msg.GetOperationId() != initial.Id || req.Msg.GetRequestId() != "" {
					t.Fatal("poll changed lookup identity")
				}
				if scenario == "read failure" {
					return nil, errors.New("synthetic read failure")
				}
				switch scenario {
				case "nil response":
					return nil, nil
				case "nil message":
					return &connect.Response[ipc.GetOperationResponse]{}, nil
				case "nil operation":
					return connect.NewResponse(&ipc.GetOperationResponse{}), nil
				}
				op := proto.Clone(initial).(*ipc.Operation)
				op.State = ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER
				if calls >= 3 {
					op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
				}
				if scenario == "identity change" {
					op.RequestId = "another request"
				}
				if scenario == "failure" {
					op.State = ipc.OperationState_OPERATION_STATE_FAILED
					op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}}
				}
				if scenario == "cancelled operation" {
					op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
				}
				return connect.NewResponse(&ipc.GetOperationResponse{Operation: op}), nil
			})
			result, err := waitServiceOperation(ctx, reader, initial, time.Millisecond, func(*ipc.Operation) error {
				reports++
				if scenario == "cancel" {
					cancel()
				}
				if scenario == "writer failure" {
					return errors.New("synthetic writer failure")
				}
				return nil
			})
			if (err == nil) != (scenario == "success") {
				t.Fatal("wrong wait outcome", err)
			}
			if scenario == "cancelled operation" && (result.State != ipc.OperationState_OPERATION_STATE_CANCELLED || calls != 1) {
				t.Fatal("cancelled operation was not returned as terminal")
			}
			if scenario == "success" && (result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || calls != 3 || reports != 3) {
				t.Fatal("waiting state was duplicated or terminal state lost")
			}
			if (scenario == "cancel" || scenario == "writer failure") && calls != 0 {
				t.Fatal("poll continued after cancellation/output failure")
			}
		})
	}
}

func TestWaitServiceOperationReporterCannotRewriteIdentity(t *testing.T) {
	initial := &ipc.Operation{Id: "operation", RequestId: "request", ProfileId: "profile", Kind: ipc.OperationKind_OPERATION_KIND_CONNECT, State: ipc.OperationState_OPERATION_STATE_PENDING}
	calls := 0
	reader := serviceOperationReadFunc(func(_ context.Context, request *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
		calls++
		if request.Msg.GetOperationId() != "operation" {
			t.Fatal("reporter rewrote lookup identity")
		}
		terminal := proto.Clone(initial).(*ipc.Operation)
		terminal.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		return connect.NewResponse(&ipc.GetOperationResponse{Operation: terminal}), nil
	})
	result, err := waitServiceOperation(t.Context(), reader, initial, time.Millisecond, func(operation *ipc.Operation) error {
		operation.Id = "changed"
		operation.RequestId = "changed"
		operation.State = ipc.OperationState_OPERATION_STATE_FAILED
		return nil
	})
	if err != nil || result.Id != "operation" || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || calls != 1 {
		t.Fatal("reporter changed wait outcome", err)
	}
	if initial.Id != "operation" || initial.RequestId != "request" || initial.State != ipc.OperationState_OPERATION_STATE_PENDING {
		t.Fatal("accepted caller operation was modified")
	}
}

func TestWaitServiceOperationPreCanceledContextDoesNoWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := serviceOperationReadFunc(func(context.Context, *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
		t.Fatal("read after cancellation")
		return nil, nil
	})
	initial := &ipc.Operation{Id: "operation", RequestId: "request", State: ipc.OperationState_OPERATION_STATE_PENDING}
	_, err := waitServiceOperation(ctx, reader, initial, time.Millisecond, func(*ipc.Operation) error {
		t.Fatal("report after cancellation")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("missing cancellation", err)
	}
}
