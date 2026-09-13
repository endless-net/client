package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type managedRPCTestClient struct {
	enrolls, connects, reads int
	fail                     bool
	connectRevision          uint64
}

func (c *managedRPCTestClient) Enroll(context.Context, *connect.Request[ipc.EnrollRequest]) (*connect.Response[ipc.EnrollResponse], error) {
	c.enrolls++
	return connect.NewResponse(&ipc.EnrollResponse{Operation: &ipc.Operation{Id: "enrollment", RequestId: "enroll-request", Kind: ipc.OperationKind_OPERATION_KIND_ENROLL, State: ipc.OperationState_OPERATION_STATE_PENDING}}), nil
}
func (c *managedRPCTestClient) GetOperation(_ context.Context, req *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
	c.reads++
	if req.Msg.GetOperationId() != "enrollment" {
		return nil, errors.New("unexpected operation lookup")
	}
	op := &ipc.Operation{Id: "enrollment", RequestId: "enroll-request", Kind: ipc.OperationKind_OPERATION_KIND_ENROLL, State: ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER}
	if c.reads == 2 {
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Metadata = &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 9}
	}
	if c.fail {
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED}}
	}
	return connect.NewResponse(&ipc.GetOperationResponse{Operation: op}), nil
}
func (c *managedRPCTestClient) Connect(_ context.Context, req *connect.Request[ipc.ConnectRequest]) (*connect.Response[ipc.ConnectResponse], error) {
	c.connects++
	c.connectRevision = req.Msg.Mutation.ExpectedRevision
	return connect.NewResponse(&ipc.ConnectResponse{Operation: &ipc.Operation{Id: "connection", RequestId: "connect-request", Kind: ipc.OperationKind_OPERATION_KIND_CONNECT, State: ipc.OperationState_OPERATION_STATE_SUCCEEDED}}), nil
}

func TestManagedNativeUpDoesNotReplayEnrollment(t *testing.T) {
	for _, scenario := range []string{"success", "approval rejected", "connect only", "output failed"} {
		t.Run(scenario, func(t *testing.T) {
			consumer := &managedRPCTestClient{fail: scenario == "approval rejected"}
			enrollment := &ipc.EnrollRequest{Mutation: &ipc.MutationContext{RequestId: "enroll-request"}}
			if scenario == "connect only" {
				enrollment = nil
			}
			connection := &ipc.ConnectRequest{Mutation: &ipc.MutationContext{RequestId: "connect-request", ExpectedRevision: 3}}
			err := runManagedUp(t.Context(), consumer, enrollment, connection, time.Millisecond, func(*ipc.Operation) error {
				if scenario == "output failed" {
					return errors.New("synthetic output failure")
				}
				return nil
			})
			wantSuccess := scenario == "success" || scenario == "connect only"
			if (err == nil) != wantSuccess {
				t.Fatal("wrong managed outcome", err)
			}
			if consumer.enrolls > 1 || (scenario != "connect only" && consumer.enrolls != 1) {
				t.Fatal("managed up replayed enrollment")
			}
			if !wantSuccess && consumer.connects != 0 {
				t.Fatal("connection started after failed registration/reporting")
			}
			if wantSuccess && consumer.connects != 1 {
				t.Fatal("connection not dispatched exactly once")
			}
			if scenario == "success" && (consumer.reads != 2 || consumer.connectRevision != 9) {
				t.Fatal("approval was not polled or terminal revision not used")
			}
			if connection.Mutation.ExpectedRevision != 3 {
				t.Fatal("caller mutation context changed")
			}
		})
	}
}

func TestManagedNativeUpRequiresExplicitOperationIdentity(t *testing.T) {
	for _, args := range [][]string{nil, {"--server", "https://example.test"},
		{"--profile-id", "profile", "--expected-instance-id", "instance", "--expected-revision", "1", "--connect-request-id", "A550FD02-8B66-434F-AC47-570B0FDF0FEE", "--enroll-request-id", "a550fd02-8b66-434f-ac47-570b0fdf0fee"},
	} {
		if err := cmdManagedUp(args); err == nil {
			t.Fatal("invalid or ambiguous managed command accepted")
		}
	}
}
