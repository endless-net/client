package headless

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type upFake struct {
	*fakeService
	current, result *clientv0.EnrollmentAttempt
	observed        *clientv0.EnrollmentAttempt
	started         *clientv0.EnrollRequest
	lookups         []*clientv0.GetEnrollmentAttemptRequest
	connected       *clientv0.ConnectRequest
	lost            bool
	connectErr      error
	connectCalls    int
}

func (f *upFake) GetEnrollmentAttempt(_ context.Context, req *clientv0.GetEnrollmentAttemptRequest, _ ...grpc.CallOption) (*clientv0.GetEnrollmentAttemptResponse, error) {
	f.lookups = append(f.lookups, proto.Clone(req).(*clientv0.GetEnrollmentAttemptRequest))
	if req.GetCurrent() {
		if f.current == nil {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return &clientv0.GetEnrollmentAttemptResponse{Attempt: f.current}, nil
	}
	if f.observed != nil {
		return &clientv0.GetEnrollmentAttemptResponse{Attempt: f.observed}, nil
	}
	return &clientv0.GetEnrollmentAttemptResponse{Attempt: f.result}, nil
}

func (f *upFake) Enroll(_ context.Context, req *clientv0.EnrollRequest, _ ...grpc.CallOption) (*clientv0.EnrollResponse, error) {
	if f.started != nil {
		panic("blind mutation retry")
	}
	f.started = proto.Clone(req).(*clientv0.EnrollRequest)
	f.result.RequestId, f.result.Mode, f.result.Hostname = req.RequestId, req.Mode, req.Hostname
	if f.lost {
		return nil, status.Error(codes.Unavailable, "private backend diagnostic")
	}
	return &clientv0.EnrollResponse{Attempt: f.result}, nil
}

func (f *upFake) Connect(_ context.Context, req *clientv0.ConnectRequest, _ ...grpc.CallOption) (*clientv0.ConnectResponse, error) {
	f.connected = proto.Clone(req).(*clientv0.ConnectRequest)
	f.connectCalls++
	return &clientv0.ConnectResponse{}, f.connectErr
}

func TestUpLostConnectAckReadsActualStateWithoutRepeatingMutation(t *testing.T) {
	for _, applied := range []bool{true, false} {
		t.Run(map[bool]string{true: "applied", false: "pending"}[applied], func(t *testing.T) {
			before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
			after := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, 8, clientv0.DesiredState_DESIRED_STATE_CONNECTED)
			if !applied {
				after.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING
			}
			f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before, after}}, connectErr: status.Error(codes.Unavailable, "private diagnostic")}
			var out bytes.Buffer
			err := Run(t.Context(), f, "up", &out)
			if (err == nil) != applied || f.connectCalls != 1 || f.reads != 2 || err != nil && strings.Contains(err.Error(), "private") || !applied && out.Len() != 0 {
				t.Fatal("lost ACK was retried or confused with applied state", err)
			}
		})
	}
}

func TestUpLostConnectAckReportsObservedLocalFailure(t *testing.T) {
	before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
	after := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED, 8, clientv0.DesiredState_DESIRED_STATE_CONNECTED)
	after.ServiceState = clientv0.ServiceState_SERVICE_STATE_ERROR
	after.Failures = []*clientv0.Failure{{ReasonKey: "client.connection.apply_failed"}}
	f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before, after}}, connectErr: status.Error(codes.Unavailable, "private diagnostic")}
	var out bytes.Buffer
	if err := Run(t.Context(), f, "up", &out); err == nil || f.connectCalls != 1 || f.reads != 2 || !strings.Contains(out.String(), "Не удалось настроить сетевой туннель Windows") || strings.Contains(out.String(), "private diagnostic") {
		t.Fatal("lost ACK hid observed failure or repeated mutation", err)
	}
}

func upAttempt() *clientv0.EnrollmentAttempt {
	return &clientv0.EnrollmentAttempt{RequestId: "original-request", AttemptId: "original-attempt", ProfileId: "profile", Mode: clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER, Hostname: "original-host", Phase: clientv0.EnrollmentPhase_ENROLLMENT_PHASE_AWAITING_LOGIN, Deadline: timestamppb.New(time.Now().Add(time.Minute)), Metadata: &clientv0.SnapshotMetadata{InstanceId: "runtime", Revision: 8}, PollAfterSeconds: 1}
}

func TestUpReattachesOriginalInputsAndPollsAutomaticServerDefault(t *testing.T) {
	before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
	before.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
	a := upAttempt()
	result := proto.Clone(a).(*clientv0.EnrollmentAttempt)
	result.Phase = clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CHOOSING_CONTEXT
	finished := proto.Clone(a).(*clientv0.EnrollmentAttempt)
	finished.Phase = clientv0.EnrollmentPhase_ENROLLMENT_PHASE_REGISTERED
	f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before}}, current: a, result: result, observed: finished}
	err := Run(t.Context(), f, "up", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "регистрация ещё не подтверждена") || f.started.RequestId != a.RequestId || f.started.Hostname != "original-host" || f.started.Mode != clientv0.EnrollmentMode_ENROLLMENT_MODE_SERVER || f.started.Mutation.ExpectedRevision != 8 || f.connected != nil || len(f.lookups) != 2 || !f.lookups[0].GetCurrent() || f.lookups[1].GetRequestId() != a.RequestId {
		t.Fatalf("lost original attempt: %v", err)
	}
}

func TestUpLostEnrollAckReadsSameRequestWithoutAnotherMutation(t *testing.T) {
	before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 1, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
	before.ActiveProfileId = ""
	before.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
	a := upAttempt()
	a.Restriction = &clientv0.Restriction{ReasonKey: "client.server_identity.unavailable"}
	f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before}}, result: a, lost: true}
	var out bytes.Buffer
	err := Run(t.Context(), f, "up", &out)
	if err == nil || strings.Contains(err.Error(), "private") || len(f.lookups) != 1 || f.lookups[0].GetRequestId() != f.started.RequestId || out.Len() != 0 || f.connected != nil {
		t.Fatalf("uncertain enrollment retried/leaked: %v", err)
	}
}

func TestUpRejectsMissingOriginalInputsBeforeReplay(t *testing.T) {
	for _, kind := range []string{"mode", "hostname", "expired", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
			before.ControlState = clientv0.ControlState_CONTROL_STATE_NOT_REGISTERED
			a := upAttempt()
			switch kind {
			case "mode":
				a.Mode = 0
			case "hostname":
				a.Hostname = ""
			case "expired":
				a.Deadline = timestamppb.New(time.Now().Add(-time.Second))
			case "runtime":
				a.Metadata.InstanceId = "new-runtime"
			}
			f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before}}, current: a}
			if err := Run(t.Context(), f, "up", &bytes.Buffer{}); err == nil || f.started != nil || f.connected != nil {
				t.Fatal("invalid recovery started new mutation")
			}
		})
	}
}

func TestUpRegisteredUsesCurrentConnectBasisAndActualStatus(t *testing.T) {
	for _, connected := range []bool{true, false} {
		t.Run(map[bool]string{true: "applied", false: "ack-only"}[connected], func(t *testing.T) {
			before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
			after := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, 8, clientv0.DesiredState_DESIRED_STATE_CONNECTED)
			if !connected {
				after.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING
			}
			f := &upFake{fakeService: &fakeService{snapshots: []*clientv0.Status{before, after}}}
			var out bytes.Buffer
			err := Run(t.Context(), f, "up", &out)
			if (err == nil) != connected || f.connected == nil || f.connected.Mutation.ExpectedRevision != 7 || f.connected.Mutation.ExpectedInstanceId != "runtime" || f.started != nil || len(f.lookups) != 0 || !connected && out.Len() != 0 {
				t.Fatalf("false Connected or stale mutation: %v", err)
			}
		})
	}
}
