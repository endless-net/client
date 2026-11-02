package localapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type observedConnection struct {
	value connectivity.Observation
	calls int
}

func (o *observedConnection) ConnectionSnapshot() connectivity.Observation {
	o.calls++
	return o.value
}

func TestConnectionStatusRequiresCurrentGenerationMeasuredApplyAndPath(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	online := connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{NewFlows: true, ExistingFlows: true}, DirectPaths: 1, MapRevision: 8, Address: "100.64.0.7", PeerCount: 1}
	for _, tc := range []struct {
		name      string
		phase     connectivity.Phase
		local     connectivity.LocalObservation
		stale     bool
		want      clientv0.ServiceState
		wantPhase clientv0.ConnectionPhase
	}{
		{"pending", connectivity.PhaseConnecting, connectivity.LocalObservation{}, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING},
		{"applied_path", connectivity.PhaseConnected, online, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"busy_unknown", connectivity.PhaseConnected, connectivity.LocalObservation{}, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"stale_generation", connectivity.PhaseConnected, online, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"listener_without_path", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true, Permission: online.Permission}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"offline_existing", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{ExistingFlows: true}}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"source_blocked", connectivity.PhaseConnected, connectivity.LocalObservation{Known: true, Applied: true}, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"unsafe_clock", connectivity.PhaseConnected, connectivity.LocalObservation{Permission: connectivity.Permission{ClockUnsafe: true}}, false, clientv0.ServiceState_SERVICE_STATE_ERROR, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"revoked", connectivity.PhaseConnected, connectivity.LocalObservation{Permission: connectivity.Permission{Revoked: true}}, false, clientv0.ServiceState_SERVICE_STATE_POLICY_BLOCKED, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generation := uint64(1)
			if tc.stale {
				generation++
			}
			o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: generation, Intent: state.IntentConnected, Phase: tc.phase, Local: tc.local}}
			c.config.ConnectionObservations = o
			got, err := c.snapshot(context.Background(), caller)
			if err != nil || got.Status.ServiceState != tc.want || got.Status.ConnectionPhase != tc.wantPhase || got.Status.Intent.DesiredState != clientv0.DesiredState_DESIRED_STATE_CONNECTED {
				t.Fatal("intent, application and path conflated", got, err)
			}
			if tc.stale && (got.Status.MapRevision != 0 || got.Status.Agent != nil) {
				t.Fatal("stale backend generation leaked into current snapshot")
			}
			if got.Status.ControlState == clientv0.ControlState_CONTROL_STATE_READY && got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED {
				t.Fatal("unconfirmed path claimed ready control")
			}
		})
	}
}

func TestConnectionStatusSeparatesFailureFromCleanupAndFiltersObserver(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseFailed, Failure: connectivity.FailurePermission, CleanupPending: true, CleanupFailure: connectivity.FailureCleanup}}
	c.config.ConnectionObservations = o
	got, err := c.snapshot(context.Background(), caller)
	if err != nil || got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_ERROR || got.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || len(got.Status.Failures) != 2 || got.Status.Failures[0].Code != clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED || got.Status.Failures[1].ReasonKey != "client.connection.cleanup_unconfirmed" {
		t.Fatal("permission or uncertain cleanup erased", got, err)
	}
	o.value.CleanupPending, o.value.CleanupFailure = false, nil
	got, err = c.snapshot(context.Background(), caller)
	if err != nil || got.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || len(got.Status.Failures) != 1 {
		t.Fatal("confirmed safe cleanup lost primary failure", got, err)
	}
	o.value.Failure = errors.New("synthetic-secret backend response")
	got, err = c.snapshot(context.Background(), caller)
	if err != nil || strings.Contains(protojson.Format(got), "synthetic-secret") || got.Status.Failures[0].ReasonKey != "client.connection.failed" {
		t.Fatal("raw failure leaked", err)
	}
	calls := o.calls
	got, err = c.snapshot(context.Background(), localipc.Principal{SID: "another-user"})
	if err != nil || o.calls != calls || got.Status.ActiveProfileId != "" || got.Status.Agent != nil || len(got.Status.Failures) != 0 {
		t.Fatal("non-owner inspected private connection", err)
	}
}

func TestTypedConnectionRPCFailuresNeverExposeBackendText(t *testing.T) {
	for _, tc := range []struct {
		kind  connectivity.FailureKind
		code  codes.Code
		typed clientv0.ErrorCode
	}{
		{connectivity.FailurePermission, codes.PermissionDenied, clientv0.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED},
		{connectivity.FailureAccess, codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_POLICY_BLOCKED},
		{connectivity.FailureConflict, codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT},
		{connectivity.FailureApply, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED},
		{connectivity.FailurePath, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{connectivity.FailureCleanup, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_APPLY_FAILED},
		{connectivity.FailureControl, codes.Unavailable, clientv0.ErrorCode_ERROR_CODE_UNAVAILABLE},
	} {
		err := rpcError(errors.Join(tc.kind, errors.New("synthetic-secret native path")))
		if status.Code(err) != tc.code || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("classification lost or text leaked", err)
		}
		details := status.Convert(err).Details()
		if len(details) != 1 || details[0].(*clientv0.Failure).Code != tc.typed {
			t.Fatal("typed detail lost", details)
		}
	}
}
