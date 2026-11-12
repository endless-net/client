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

func TestRecoveryFailuresRemainDistinctAndSafe(t *testing.T) {
	for kind, reason := range map[connectivity.FailureKind]string{
		connectivity.FailureTrust:      "client.connection.trust_required",
		connectivity.FailureCredential: "client.connection.credential_expired",
		connectivity.FailureAccess:     "client.connection.access_denied",
		connectivity.FailureRevoked:    "client.connection.registration_revoked",
		connectivity.FailurePath:       "client.connection.path_unconfirmed",
		connectivity.FailureCleanup:    "client.connection.cleanup_unconfirmed",
		connectivity.FailureLogin:      "client.connection.login_required",
	} {
		f := observedConnectionFailure(kind)
		if f == nil || f.ReasonKey != reason {
			t.Fatal("unsafe or conflated recovery reason", kind, f)
		}
		if (kind == connectivity.FailureTrust || kind == connectivity.FailureCredential) && f.Retryable {
			t.Fatal("safety action replaced by automatic retry")
		}
	}
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
	online.PeerTransports = []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_DIRECT, Engine: "amneziawg", ProtocolVersion: "3.1", TransportProfileId: "profile", TransportProfileRevision: 7, SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_IDLE, ReasonKey: "client.transport.direct"}}
	sole := connectivity.LocalObservation{Known: true, Applied: true, SoleNodeReady: true, Permission: online.Permission, MapRevision: 8, Address: "100.64.0.7"}
	soleOffline := sole
	soleOffline.Permission = connectivity.Permission{ExistingFlows: true}
	soleWithPeer := sole
	soleWithPeer.PeerCount = 1
	relay := online
	relay.DirectPaths, relay.RelayPaths = 0, 1
	unsupported := relay
	unsupported.PeerTransports = []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_RELAY, Engine: "wireguard", ProtocolVersion: "1", SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_UNSUPPORTED, ReasonKey: "client.transport.capabilities_incompatible"}}
	for _, tc := range []struct {
		name      string
		phase     connectivity.Phase
		local     connectivity.LocalObservation
		stale     bool
		want      clientv0.ServiceState
		wantPhase clientv0.ConnectionPhase
	}{
		{"pending", connectivity.PhaseConnecting, connectivity.LocalObservation{}, false, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING},
		{"verified_sole_node", connectivity.PhaseConnected, sole, false, clientv0.ServiceState_SERVICE_STATE_LOCAL_READY, clientv0.ConnectionPhase_CONNECTION_PHASE_LOCAL_READY},
		{"stale_sole_node", connectivity.PhaseConnected, sole, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
		{"offline_sole_node", connectivity.PhaseConnected, soleOffline, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"peer_without_authenticated_path", connectivity.PhaseConnected, soleWithPeer, false, clientv0.ServiceState_SERVICE_STATE_DEGRADED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"applied_path", connectivity.PhaseConnected, online, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"applied_relay", connectivity.PhaseConnected, relay, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"relay_with_unsupported_direct", connectivity.PhaseConnected, unsupported, false, clientv0.ServiceState_SERVICE_STATE_CONNECTED, clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTED},
		{"unsupported_stale_generation", connectivity.PhaseConnected, unsupported, true, clientv0.ServiceState_SERVICE_STATE_RECOVERING, clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED},
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
			if tc.stale && (got.Status.MapRevision != 0 || got.Status.Agent != nil || len(got.Status.PeerTransports) != 0) {
				t.Fatal("stale backend generation leaked into current snapshot")
			}
			if !tc.stale && tc.local.Known && tc.local.Applied && (got.Status.Agent == nil || got.Status.Agent.RelayPathCount != tc.local.RelayPaths || got.Status.Agent.DirectPathCount != tc.local.DirectPaths) {
				t.Fatal("measured path counts were lost in the producer IPC projection")
			}
			if got.Status.ControlState == clientv0.ControlState_CONTROL_STATE_READY && got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED && got.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_LOCAL_READY {
				t.Fatal("unconfirmed path claimed ready control")
			}
			if !tc.stale && tc.local.Known && tc.local.Applied && len(tc.local.PeerTransports) > 0 {
				expected := tc.local.PeerTransports[0]
				if len(got.Status.PeerTransports) != 1 || got.Status.PeerTransports[0].TransportProfileRevision != expected.TransportProfileRevision || got.Status.PeerTransports[0].SearchState != expected.SearchState || got.Status.PeerTransports[0].SelectedPath != expected.SelectedPath || got.Status.PeerTransports[0].ReasonKey != expected.ReasonKey {
					t.Fatal("public profile observation lost", got)
				}
				engine := expected.Engine
				got.Status.PeerTransports[0].Engine = "reader mutation"
				if tc.local.PeerTransports[0].Engine != engine {
					t.Fatal("IPC response aliased runtime snapshot")
				}
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
