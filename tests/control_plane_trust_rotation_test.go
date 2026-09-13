package tests

import (
	"net/http"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
)

// HC-021: map trust recovery preserves enrollment and connection intent.
// Node credential trust remains independent of map-signing trust.
func TestControlPlaneMapSigningRotation(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		disconnected, interrupted bool
	}{
		{"connected", false, false}, {"disconnected", true, false},
		{"connected-interrupted", false, true}, {"disconnected-interrupted", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) { testMapSigningRotation(t, tc.disconnected, tc.interrupted) })
	}
}

func testMapSigningRotation(t *testing.T, disconnected, interrupted bool) {
	t.Helper()
	s, n, id := nativeControlScenario(t)
	oldKey := s.Trust().ActiveKeyID
	if disconnected {
		runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000001")
	} else {
		runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000001")
	}
	baseline := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.UserDisconnected == disconnected })
	identity := func() *ipc.ServerIdentity {
		t.Helper()
		response := &ipc.GetServerIdentityResponse{}
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			err = n.NativeService("server-identity", response, "--profile-id", baseline.ActiveProfileId)
			if !testclient.IsNativeStaleState(err) {
				break
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if response.Identity == nil {
			t.Fatal("missing native server identity")
		}
		return response.Identity
	}
	if err := s.RotateMapSigningKey(); err != nil {
		t.Fatal(err)
	}
	newKey := s.Trust().ActiveKeyID
	announced := identity()
	if !announced.Changed || announced.TrustedKeyId != oldKey || announced.AnnouncedKeyId != newKey || announced.ControlOrigin != s.URL() || announced.AnnouncementId == "" {
		t.Fatal("client did not distinguish pinned and announced map identities")
	}
	attempt := func(requestID, key string) (*ipc.Operation, []string) {
		t.Helper()
		status := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.ActiveProfileId == baseline.ActiveProfileId })
		response := &ipc.TrustServerIdentityResponse{}
		var args []string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			args = append(testclient.NativeMutationArguments(requestID, status), "--confirmed-control-origin", announced.ControlOrigin,
				"--confirmed-key-id", key, "--confirmed-announcement-id", announced.AnnouncementId)
			err = n.NativeService("trust-server", response, args...)
			if !testclient.IsNativeStaleState(err) || attempt == 2 {
				break
			}
			// An admission rejection is distinct from the accepted stale-key
			// failure asserted below. Never retry that terminal operation or
			// change the request ID, key, origin or announcement being tested.
			observed := identity()
			refreshed := &ipc.GetStatusResponse{}
			if n.NativeService("status", refreshed) != nil || !nativeTrustRetryContextMatches(status, refreshed.Status, announced, observed) {
				t.Fatal("native signing rotation context changed after CAS rejection")
			}
			status = refreshed.Status
		}
		if err != nil {
			t.Fatal(err)
		}
		if response.GetOperation().GetId() == "" || response.Operation.Kind != ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY || response.Operation.ProfileId != baseline.ActiveProfileId {
			t.Fatal("missing profile-bound trust operation")
		}
		return response.Operation, args
	}
	stale, _ := attempt("00000000-0000-4000-8000-000000000002", oldKey)
	rejected := n.AwaitNativeOperation(stale.Id)
	if rejected.State != ipc.OperationState_OPERATION_STATE_FAILED || rejected.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE || identity().TrustedKeyId != oldKey {
		t.Fatal("stale confirmation changed pinned trust")
	}
	if interrupted {
		if err := s.SetPublicError(http.MethodPost, "/nodes/register", api.ErrorCodeTemporarilyUnavailable); err != nil {
			t.Fatal(err)
		}
	}
	accepted, args := attempt("00000000-0000-4000-8000-000000000003", newKey)
	if interrupted {
		awaitRecovery := func() {
			t.Helper()
			n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() &&
					v.GetRecovery().GetOperationId() == accepted.Id && v.GetRecovery().GetState() == ipc.ServiceState_SERVICE_STATE_RECOVERING &&
					v.GetRecovery().GetFailure().GetRetryable()
			})
		}
		awaitRecovery()
		n.Crash()
		n.Start()
		awaitRecovery()
		pending := &ipc.GetOperationResponse{}
		if err := n.NativeService("operation", pending, "--request-id", accepted.RequestId); err != nil {
			t.Fatal(err)
		}
		if pending.GetOperation().GetId() != accepted.Id || pending.GetOperation().GetState() != ipc.OperationState_OPERATION_STATE_RUNNING {
			t.Fatal("interrupted recovery lost its running durable operation")
		}
		// The native trust worker retries even when user connection intent is off.
		// Recovery must not require another mutation or silently connect the tunnel.
		s.ClearResponseFault(http.MethodPost, "/nodes/register")
	}
	completed := n.AwaitNativeOperation(accepted.Id)
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.GetChange() == nil || !completed.GetChange().Changed {
		t.Fatal("confirmed trust recovery did not complete")
	}
	awaitIntent := func() {
		t.Helper()
		n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.ActiveProfileId == baseline.ActiveProfileId && v.GetStoredState().GetNodeCredentialPresent() &&
				v.GetStoredState().GetCachedMapValid() && v.UserDisconnected == disconnected &&
				v.GetIntent().GetDesiredState() == baseline.GetIntent().GetDesiredState() && v.Recovery == nil &&
				(disconnected || v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED)
		})
	}
	awaitIntent()
	n.Stop()
	n.Start()
	awaitIntent()
	replay := &ipc.TrustServerIdentityResponse{}
	if err := n.NativeService("trust-server", replay, args...); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replay.Operation, completed) {
		t.Fatal("completed recovery replay changed outcome")
	}
	confirmed := identity()
	if confirmed.Changed || confirmed.TrustedKeyId != newKey || confirmed.AnnouncedKeyId != newKey {
		t.Fatal("confirmed trust did not survive restart")
	}
	if disconnected {
		runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000004")
	}
	if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "after-map-rotation" }); err != nil {
		t.Fatal(err)
	}
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetCachedMapValid() && !v.UserDisconnected && v.GetNetwork().GetName() == "after-map-rotation" &&
			v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && v.Agent.LastFailure == nil
	})
	created, refreshed := 0, 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			created++
		}
		if event.Kind == "registration-refreshed" {
			refreshed++
		}
		if (event.Kind == "registered" || event.Kind == "registration-refreshed") && event.NodeID != id {
			t.Fatal("trust recovery changed node identity")
		}
	}
	if created != 1 || refreshed == 0 {
		t.Fatal("trust recovery did not renew the existing enrollment")
	}
}
