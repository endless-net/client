package headless

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct {
	reads         int
	snapshots     []*clientv0.Status
	mutation      *clientv0.DisconnectRequest
	readErr       error
	disconnectErr error
}

func (f *fakeService) GetStatus(ctx context.Context, _ *clientv0.GetStatusRequest, _ ...grpc.CallOption) (*clientv0.GetStatusResponse, error) {
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second {
		return nil, errors.New("missing call budget")
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := min(f.reads, len(f.snapshots)-1)
	f.reads++
	return &clientv0.GetStatusResponse{Status: f.snapshots[index]}, nil
}

func (f *fakeService) Disconnect(_ context.Context, request *clientv0.DisconnectRequest, _ ...grpc.CallOption) (*clientv0.DisconnectResponse, error) {
	f.mutation = request
	return &clientv0.DisconnectResponse{}, f.disconnectErr
}

func snapshot(phase clientv0.ConnectionPhase, revision uint64, desired clientv0.DesiredState) *clientv0.Status {
	return &clientv0.Status{Metadata: &clientv0.SnapshotMetadata{InstanceId: "runtime", Revision: revision},
		ActiveProfileId: "profile", ConnectionPhase: phase,
		Intent:       &clientv0.ConnectionIntent{DesiredState: desired},
		ServiceState: clientv0.ServiceState_SERVICE_STATE_DISCONNECTED,
		ControlState: clientv0.ControlState_CONTROL_STATE_REGISTERED}
}

func TestDownUsesCurrentMetadataAndConfirmsState(t *testing.T) {
	f := &fakeService{snapshots: []*clientv0.Status{
		snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_CONNECTED),
		snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 8, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)}}
	var out bytes.Buffer
	if err := Run(t.Context(), f, "down", &out); err != nil {
		t.Fatal(err)
	}
	if f.mutation == nil || f.mutation.Mutation.ExpectedInstanceId != "runtime" || f.mutation.Mutation.ExpectedRevision != 7 || f.mutation.Profile.ProfileId != "profile" || f.reads != 2 {
		t.Fatal("command lost caller snapshot fence")
	}
	if !strings.Contains(out.String(), "CONNECTION_PHASE_DISCONNECTED") {
		t.Fatal(out.String())
	}
}

func TestDownClearsConnectedIntentEvenWhenTunnelAlreadyAbsent(t *testing.T) {
	f := &fakeService{snapshots: []*clientv0.Status{
		snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_CONNECTED),
		snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 8, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)}}
	if err := Run(t.Context(), f, "down", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if f.mutation == nil {
		t.Fatal("restart would restore unwanted connected intent")
	}
}

func TestDownDoesNotTreatAckOrNewRuntimeAsDisconnection(t *testing.T) {
	for _, kind := range []string{"ack", "runtime", "profile", "revision", "intent"} {
		t.Run(kind, func(t *testing.T) {
			before := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_CONNECTED)
			after := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 8, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
			switch kind {
			case "ack":
				after.ConnectionPhase = clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED
			case "runtime":
				after.Metadata.InstanceId = "other"
			case "profile":
				after.ActiveProfileId = "other"
			case "revision":
				after.Metadata.Revision = 6
			case "intent":
				after.Intent.DesiredState = clientv0.DesiredState_DESIRED_STATE_CONNECTED
			}
			f := &fakeService{snapshots: []*clientv0.Status{before, after}}
			var out bytes.Buffer
			if err := Run(t.Context(), f, "down", &out); err == nil || out.Len() != 0 {
				t.Fatal("false success")
			}
		})
	}
}

func TestUncertainMutationIsNeverRepeatedOrLeaked(t *testing.T) {
	for _, code := range []codes.Code{codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.PermissionDenied} {
		f := &fakeService{snapshots: []*clientv0.Status{snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED, 7, clientv0.DesiredState_DESIRED_STATE_CONNECTED)}, disconnectErr: status.Error(code, "secret-fixture")}
		var out bytes.Buffer
		err := Run(t.Context(), f, "down", &out)
		if err == nil || strings.Contains(err.Error(), "secret-fixture") || out.Len() != 0 || f.reads != 1 {
			t.Fatal("mutation error retried or disclosed")
		}
	}
}

func TestStatusAndAlreadyDownAreReadOnly(t *testing.T) {
	for _, command := range []string{"status", "down"} {
		f := &fakeService{snapshots: []*clientv0.Status{snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 0, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)}}
		if err := Run(t.Context(), f, command, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if f.mutation != nil || f.reads != 1 {
			t.Fatal("unexpected mutation")
		}
	}
}

func TestUpCannotFallBackToLegacyTrustOrEnrollment(t *testing.T) {
	f := &fakeService{snapshots: []*clientv0.Status{snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 0, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)}}
	var out bytes.Buffer
	if err := Run(t.Context(), f, "up", &out); !errors.Is(err, ErrPrerequisite) || f.mutation != nil || out.Len() != 0 {
		t.Fatal("unsafe up")
	}
}

func TestCancelledReadAndInvalidSnapshotDoNotSucceed(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := &fakeService{snapshots: []*clientv0.Status{nil}}
	if err := Run(ctx, f, "status", &bytes.Buffer{}); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	if err := Run(t.Context(), f, "status", &bytes.Buffer{}); err == nil {
		t.Fatal("nil snapshot succeeded")
	}
}

func TestDownCannotTreatForeignRedactedStatusAsDisconnection(t *testing.T) {
	s := snapshot(clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, 1, clientv0.DesiredState_DESIRED_STATE_DISCONNECTED)
	s.ActiveProfileId = ""
	s.ControlState = clientv0.ControlState_CONTROL_STATE_UNSPECIFIED
	f := &fakeService{snapshots: []*clientv0.Status{s}}
	if err := Run(t.Context(), f, "down", &bytes.Buffer{}); err == nil || f.mutation != nil {
		t.Fatal("redacted foreign status was treated as confirmed disconnection")
	}
}
