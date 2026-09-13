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
	adjust                   func(string, *ipc.Operation)
	missing                  string
}

func (c *managedRPCTestClient) Enroll(context.Context, *connect.Request[ipc.EnrollRequest]) (*connect.Response[ipc.EnrollResponse], error) {
	c.enrolls++
	if c.missing == "enroll" {
		return nil, nil
	}
	op := &ipc.Operation{Id: "enrollment", RequestId: "enroll-request", ProfileId: "profile", Kind: ipc.OperationKind_OPERATION_KIND_ENROLL, State: ipc.OperationState_OPERATION_STATE_PENDING, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 3}}
	if c.adjust != nil {
		c.adjust("enroll", op)
	}
	return connect.NewResponse(&ipc.EnrollResponse{Operation: op}), nil
}
func (c *managedRPCTestClient) GetOperation(_ context.Context, req *connect.Request[ipc.GetOperationRequest]) (*connect.Response[ipc.GetOperationResponse], error) {
	c.reads++
	if req.Msg.GetOperationId() != "enrollment" {
		return nil, errors.New("unexpected operation lookup")
	}
	op := &ipc.Operation{Id: "enrollment", RequestId: "enroll-request", Kind: ipc.OperationKind_OPERATION_KIND_ENROLL, State: ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER, ProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 7}}
	if c.reads == 2 {
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Metadata = &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 9}
		op.Outcome = &ipc.Operation_Enrollment{Enrollment: &ipc.EnrollmentResult{ProfileId: "profile", NodeId: "node"}}
	}
	if c.fail {
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED}}
	}
	if c.adjust != nil {
		c.adjust("read", op)
	}
	return connect.NewResponse(&ipc.GetOperationResponse{Operation: op}), nil
}
func (c *managedRPCTestClient) Connect(_ context.Context, req *connect.Request[ipc.ConnectRequest]) (*connect.Response[ipc.ConnectResponse], error) {
	c.connects++
	c.connectRevision = req.Msg.Mutation.ExpectedRevision
	if c.missing == "connect" {
		return nil, nil
	}
	op := &ipc.Operation{Id: "connection", RequestId: "connect-request", ProfileId: "profile", Kind: ipc.OperationKind_OPERATION_KIND_CONNECT, State: ipc.OperationState_OPERATION_STATE_SUCCEEDED, Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: req.Msg.Mutation.ExpectedRevision}}
	if c.adjust != nil {
		c.adjust("connect", op)
	}
	return connect.NewResponse(&ipc.ConnectResponse{Operation: op}), nil
}

func TestManagedNativeUpDoesNotReplayEnrollment(t *testing.T) {
	for _, scenario := range []string{"success", "approval rejected", "connect only", "output failed"} {
		t.Run(scenario, func(t *testing.T) {
			consumer := &managedRPCTestClient{fail: scenario == "approval rejected"}
			enrollment := &ipc.EnrollRequest{Profile: &ipc.ProfileRef{ProfileId: "profile"}, Mutation: &ipc.MutationContext{RequestId: "enroll-request", ExpectedInstanceId: "instance", ExpectedRevision: 3}}
			if scenario == "connect only" {
				enrollment = nil
			}
			connection := &ipc.ConnectRequest{Profile: &ipc.ProfileRef{ProfileId: "profile"}, Mutation: &ipc.MutationContext{RequestId: "connect-request", ExpectedInstanceId: "instance", ExpectedRevision: 3}}
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

func managedTestRequests() (*ipc.EnrollRequest, *ipc.ConnectRequest) {
	return &ipc.EnrollRequest{Profile: &ipc.ProfileRef{ProfileId: "profile"}, Mutation: &ipc.MutationContext{RequestId: "enroll-request", ExpectedInstanceId: "instance", ExpectedRevision: 3}},
		&ipc.ConnectRequest{Profile: &ipc.ProfileRef{ProfileId: "profile"}, Mutation: &ipc.MutationContext{RequestId: "connect-request", ExpectedInstanceId: "instance", ExpectedRevision: 3}}
}

func TestManagedUpRejectsUnboundAcceptanceAndProgress(t *testing.T) {
	changes := map[string]func(*ipc.Operation){
		"request":          func(op *ipc.Operation) { op.RequestId = "other" },
		"profile":          func(op *ipc.Operation) { op.ProfileId = "other" },
		"kind":             func(op *ipc.Operation) { op.Kind = ipc.OperationKind_OPERATION_KIND_LOGOUT },
		"instance":         func(op *ipc.Operation) { op.Metadata.InstanceId = "other" },
		"missing metadata": func(op *ipc.Operation) { op.Metadata = nil },
		"old revision":     func(op *ipc.Operation) { op.Metadata.Revision = 2 },
	}
	for _, stage := range []string{"enroll", "read", "connect"} {
		for name, change := range changes {
			t.Run(stage+"/"+name, func(t *testing.T) {
				consumer := &managedRPCTestClient{adjust: func(current string, op *ipc.Operation) {
					if current == stage {
						change(op)
					}
				}}
				enrollment, connection := managedTestRequests()
				err := runManagedUp(t.Context(), consumer, enrollment, connection, time.Millisecond, func(*ipc.Operation) error { return nil })
				if err == nil {
					t.Fatal("unbound operation accepted")
				}
				if consumer.enrolls != 1 || (stage != "connect" && consumer.connects != 0) || consumer.connects > 1 {
					t.Fatal("invalid enrollment advanced or mutation replayed")
				}
				if enrollment.Mutation.ExpectedRevision != 3 || connection.Mutation.ExpectedRevision != 3 {
					t.Fatal("caller CAS was modified")
				}
			})
		}
	}
}

func TestManagedUpRejectsMissingResponseAndInvalidEnrollmentResult(t *testing.T) {
	for _, scenario := range []string{"enroll", "connect", "missing node", "wrong result profile", "regressing revision"} {
		t.Run(scenario, func(t *testing.T) {
			consumer := &managedRPCTestClient{missing: scenario, adjust: func(stage string, op *ipc.Operation) {
				if stage != "read" || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
					return
				}
				switch scenario {
				case "missing node":
					op.Outcome = nil
				case "wrong result profile":
					op.GetEnrollment().ProfileId = "other"
				case "regressing revision":
					op.Metadata.Revision = 6 // Prior progress was revision 7.
				}
			}}
			enrollment, connection := managedTestRequests()
			err := runManagedUp(t.Context(), consumer, enrollment, connection, time.Millisecond, func(*ipc.Operation) error { return nil })
			if err == nil || consumer.enrolls != 1 || (scenario != "connect" && consumer.connects != 0) || consumer.connects > 1 {
				t.Fatal("invalid/missing result advanced managed up", err)
			}
		})
	}
}

func TestManagedUpRejectsCrossProfilePlanBeforeDispatch(t *testing.T) {
	for _, mismatch := range []string{"profile", "instance", "request", "missing context", "canceled"} {
		consumer := &managedRPCTestClient{}
		enrollment, connection := managedTestRequests()
		ctx, cancel := context.WithCancel(t.Context())
		switch mismatch {
		case "profile":
			enrollment.Profile.ProfileId = "other"
		case "instance":
			enrollment.Mutation.ExpectedInstanceId = "other"
		case "request":
			enrollment.Mutation.RequestId = connection.Mutation.RequestId
		case "missing context":
			enrollment.Mutation = nil
		case "canceled":
			cancel()
		}
		err := runManagedUp(ctx, consumer, enrollment, connection, time.Millisecond, func(*ipc.Operation) error { return nil })
		cancel()
		if err == nil || consumer.enrolls != 0 || consumer.connects != 0 {
			t.Fatal("invalid plan dispatched", mismatch, err)
		}
	}
}
