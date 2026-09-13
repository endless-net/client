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
	for _, scenario := range []string{"success", "failure", "identity change", "read failure", "cancel", "writer failure"} {
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
			if scenario == "success" && (result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || calls != 3 || reports != 3) {
				t.Fatal("waiting state was duplicated or terminal state lost")
			}
			if (scenario == "cancel" || scenario == "writer failure") && calls != 0 {
				t.Fatal("poll continued after cancellation/output failure")
			}
		})
	}
}
