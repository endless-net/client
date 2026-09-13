package tests

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
)

// HC-021: confirmation is bound to the exact origin/key/announcement.
// This does not qualify successful signing-key rotation.
func TestControlPlaneTrustConfirmation(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.ActiveProfileId != "" })
	profile := status.ActiveProfileId
	identity := func() *ipc.ServerIdentity {
		t.Helper()
		response := &ipc.GetServerIdentityResponse{}
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			err = n.NativeService("server-identity", response, "--profile-id", profile)
			if !testclient.IsNativeStaleState(err) {
				break
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		v := response.Identity
		if v == nil || v.ProfileId != profile || v.ControlOrigin != s.URL() || v.TrustedKeyId == "" || v.TrustedKeyId != v.AnnouncedKeyId || v.Changed || len(v.AnnouncementId) != 64 {
			t.Fatal("native identity does not match the enrolled server")
		}
		return v
	}
	initial := identity()
	// Complete tuple is consent in v0; the retired --yes flag is not restored.
	if _, err := n.ServiceCommand("trust-server", "--confirmed-control-origin", initial.ControlOrigin, "--confirmed-key-id", initial.AnnouncedKeyId); err == nil {
		t.Fatal("CLI accepted confirmation without its announcement ID")
	}
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	for i, mode := range []string{"wrong-origin", "missing-key", "malformed-announcement", "wrong-key", "wrong-announcement"} {
		t.Run(mode, func(t *testing.T) {
			before := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.GetStoredState().GetCachedMapValid() })
			requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+100)
			request := &ipc.TrustServerIdentityRequest{Mutation: &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: before.GetMetadata().GetInstanceId(), ExpectedRevision: before.GetMetadata().GetRevision()},
				Profile: &ipc.ProfileRef{ProfileId: profile}, ConfirmedControlOrigin: initial.ControlOrigin, ConfirmedKeyId: initial.AnnouncedKeyId, ConfirmedAnnouncementId: initial.AnnouncementId}
			switch mode {
			case "wrong-origin":
				request.ConfirmedControlOrigin = "https://wrong-origin.invalid"
			case "missing-key":
				request.ConfirmedKeyId = ""
			case "malformed-announcement":
				request.ConfirmedAnnouncementId = "invalid"
			case "wrong-key":
				request.ConfirmedKeyId = "wrong-key"
			case "wrong-announcement":
				request.ConfirmedAnnouncementId = strings.Repeat("0", 64)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			response, err := consumer.TrustServerIdentity(ctx, connect.NewRequest(request))
			cancel()
			if mode == "wrong-key" || mode == "wrong-announcement" {
				if err != nil || response.Msg.GetOperation().GetId() == "" {
					t.Fatal("well-formed confirmation did not reach verification worker")
				}
				op := n.AwaitNativeOperation(response.Msg.Operation.Id)
				if op.State != ipc.OperationState_OPERATION_STATE_FAILED || op.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE {
					t.Fatal("mismatched confirmation did not fail as stale")
				}
			} else {
				if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
					t.Fatal("malformed confirmation was not rejected at admission")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				_, err := consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: requestID}}))
				cancel()
				if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NOT_FOUND {
					t.Fatal("invalid confirmation admitted an operation")
				}
			}
			if !proto.Equal(identity(), initial) {
				t.Fatal("rejected confirmation changed pinned trust or announcement")
			}
			if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) { m.Network.Name = mode }); err != nil {
				t.Fatal(err)
			}
			n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() && v.GetNetwork().GetName() == mode && v.ActiveProfileId == profile
			})
		})
	}
	runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000001")
	status = n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.UserDisconnected && v.ActiveProfileId == profile })
	var args []string
	response := &ipc.TrustServerIdentityResponse{}
	for attempt := 0; attempt < 3; attempt++ {
		args = append(testclient.NativeMutationArguments("00000000-0000-4000-8000-000000000002", status),
			"--confirmed-control-origin", initial.ControlOrigin, "--confirmed-key-id", initial.AnnouncedKeyId, "--confirmed-announcement-id", initial.AnnouncementId)
		err = n.NativeService("trust-server", response, args...)
		if !testclient.IsNativeStaleState(err) || attempt == 2 {
			break
		}
		// Only a rejected admission can refresh CAS. The no-op assertion and
		// restart replay below must retain the original confirmed trust tuple.
		observed := identity()
		refreshed := &ipc.GetStatusResponse{}
		if n.NativeService("status", refreshed) != nil || !nativeTrustRetryContextMatches(status, refreshed.Status, initial, observed) {
			t.Fatal("native no-op trust context changed after CAS rejection")
		}
		status = refreshed.Status
	}
	if err != nil {
		t.Fatal(err)
	}
	if response.GetOperation().GetId() == "" {
		t.Fatal("missing trust operation")
	}
	completed := n.AwaitNativeOperation(response.Operation.Id)
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.Kind != ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY || completed.GetChange() == nil || completed.GetChange().Changed {
		t.Fatal("unchanged trust did not complete as no-op")
	}
	retained := func(v *ipc.Status) bool {
		return v.NodeId == id && v.ActiveProfileId == profile && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() &&
			v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
	}
	n.AwaitNativeStatus(retained)
	n.Stop()
	n.Start()
	n.AwaitNativeStatus(retained)
	replay := &ipc.TrustServerIdentityResponse{}
	if err := n.NativeService("trust-server", replay, args...); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replay.Operation, completed) || !proto.Equal(identity(), initial) {
		t.Fatal("trust replay after restart changed outcome or trust")
	}
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000003")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetCachedMapValid() && !v.UserDisconnected
	})
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("trust confirmation changed registration identity")
	}
}
