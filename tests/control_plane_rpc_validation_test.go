package tests

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// HC-053: invalid native mutations must fail before changing durable intent or
// performing remote cleanup. Retired HTTP/JSON v2 envelope rules do not apply.
func TestControlPlaneIPCRequestValidation(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal("could not create native IPC transport")
	}
	defer consumer.Close()
	for index, tc := range []struct {
		name string
		edit func(*ipc.DisconnectRequest)
		code ipc.ErrorCode
	}{
		{"missing-mutation", func(r *ipc.DisconnectRequest) { r.Mutation = nil }, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"missing-request-id", func(r *ipc.DisconnectRequest) { r.Mutation.RequestId = "" }, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"invalid-request-id", func(r *ipc.DisconnectRequest) { r.Mutation.RequestId = "not-a-uuid" }, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"missing-instance", func(r *ipc.DisconnectRequest) { r.Mutation.ExpectedInstanceId = "" }, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"missing-revision", func(r *ipc.DisconnectRequest) { r.Mutation.ExpectedRevision = 0 }, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{"wrong-instance", func(r *ipc.DisconnectRequest) { r.Mutation.ExpectedInstanceId = "another-runtime" }, ipc.ErrorCode_ERROR_CODE_STALE_STATE},
		{"future-revision", func(r *ipc.DisconnectRequest) { r.Mutation.ExpectedRevision = ^uint64(0) }, ipc.ErrorCode_ERROR_CODE_STALE_STATE},
		{"unconfirmed-local-forget", func(_ *ipc.DisconnectRequest) {}, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == id && v.GetStoredState().GetCachedMapValid() && !v.UserDisconnected
			})
			requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1)
			request := &ipc.DisconnectRequest{
				Mutation: &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: before.Metadata.InstanceId, ExpectedRevision: before.Metadata.Revision},
				Profile:  &ipc.ProfileRef{ProfileId: before.ActiveProfileId},
			}
			tc.edit(request)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var err error
			if tc.name == "unconfirmed-local-forget" {
				// Protobuf bool absence and explicit false have the same meaning.
				_, err = consumer.ForgetLocalEnrollment(ctx, connect.NewRequest(&ipc.ForgetLocalEnrollmentRequest{Mutation: request.Mutation, Profile: request.Profile, Confirmed: false}))
			} else {
				_, err = consumer.Disconnect(ctx, connect.NewRequest(request))
			}
			if rpc.FailureFromError(err).GetCode() != tc.code {
				t.Fatal("invalid native request did not return the specified typed error")
			}
			after, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
			if err != nil {
				t.Fatal("native status unavailable after rejected mutation")
			}
			status := after.Msg.GetStatus()
			if status.GetNodeId() != id || status.GetActiveProfileId() != before.ActiveProfileId || !status.GetStoredState().GetNodeCredentialPresent() || !status.GetStoredState().GetCachedMapValid() || status.GetUserDisconnected() || status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED {
				t.Fatal("invalid native request changed identity or connection intent")
			}
			_, err = consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: requestID}}))
			if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NOT_FOUND {
				t.Fatal("rejected mutation created a recoverable operation")
			}
			for _, event := range s.Events() {
				if event.Kind == "deleted" || event.Kind == "logout" {
					t.Fatal("invalid native request performed remote cleanup")
				}
			}
		})
	}
	// A valid mutation must still work after all rejected requests.
	runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000101")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
	})
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000102")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && !v.UserDisconnected && v.GetStoredState().GetCachedMapValid() && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED
	})
}
