package tests

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/proto"
)

func nativeCleanupState(v *ipc.Status) bool {
	return v != nil && v.StoredState != nil && v.NodeId == "" && v.GetNetwork().GetId() == "" &&
		!v.StoredState.NodeCredentialPresent && !v.StoredState.TokenPresent && !v.StoredState.CachedMapPresent &&
		!v.StoredState.CachedMapValid && v.MapRevision == 0 && v.PeerCount == 0 &&
		v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
}

func nativeLogoutAttempt(t *testing.T, n *testclient.Node, requestID string) (*ipc.Operation, []string) {
	t.Helper()
	status := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.ActiveProfileId != "" })
	args := testclient.NativeMutationArguments(requestID, status)
	response := &ipc.LogoutResponse{}
	if err := n.NativeService("logout", response, args...); err != nil {
		t.Fatal(err)
	}
	if response.Operation == nil || response.Operation.Id == "" || response.Operation.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT || response.Operation.ProfileId != status.ActiveProfileId {
		t.Fatal("logout did not return its profile-bound operation")
	}
	return n.AwaitNativeOperation(response.Operation.Id), args
}

func assertNativeUnconfirmedLogout(t *testing.T, op *ipc.Operation) {
	t.Helper()
	if op.GetState() != ipc.OperationState_OPERATION_STATE_FAILED || op.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED {
		t.Fatal("unavailable control did not produce a typed unconfirmed logout outcome")
	}
}

// HC-022: a failed remote cleanup remains retryable after process restart.
// Exact replay retains its failure; a fresh request can retry remote cleanup.
func TestControlPlaneLogoutRetryAfterControlRecovery(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	s.SetUnavailable(true)
	failed, args := nativeLogoutAttempt(t, n, "00000000-0000-4000-8000-000000000001")
	assertNativeUnconfirmedLogout(t, failed)
	retained := func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
	}
	n.AwaitNativeStatus(retained)
	n.Stop()
	n.Start()
	n.AwaitNativeStatus(retained)
	for _, event := range s.Events() {
		if event.Kind == "deleted" || event.Kind == "logout" {
			t.Fatal("unavailable control unexpectedly confirmed cleanup")
		}
	}
	s.SetUnavailable(false)
	replay := &ipc.LogoutResponse{}
	if err := n.NativeService("logout", replay, args...); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replay.Operation, failed) {
		t.Fatal("exact logout replay retried or changed its failed outcome")
	}
	// Restoring the fixture's HTTP availability is not yet a published client
	// recovery observation. Submit the fresh user attempt only after that
	// transition, while retaining the original enrollment. Do not retry logout
	// admission automatically or reuse the terminal failed request as new work.
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return retained(v) && v.ControlState == ipc.ControlState_CONTROL_STATE_READY
	})
	completed, _ := nativeLogoutAttempt(t, n, "00000000-0000-4000-8000-000000000002")
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.GetCleanup().GetOutcome() != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_CONFIRMED || !completed.GetCleanup().GetLocalRegistrationRemoved() {
		t.Fatal("new logout attempt did not confirm remote and local cleanup")
	}
	n.AwaitNativeStatus(nativeCleanupState)
	n.Stop()
	n.Start()
	n.AwaitNativeStatus(nativeCleanupState)
	deleted, created := 0, 0
	for _, event := range s.Events() {
		if event.Kind == "deleted" {
			deleted++
			if event.NodeID != id {
				t.Fatal("retried logout deleted a different node")
			}
		}
		if event.Kind == "registered" {
			created++
		}
	}
	if deleted != 1 || created != 1 {
		t.Fatal("logout retry did not preserve one registration and one confirmed deletion")
	}
}

// HC-022: unconfirmed remote revocation is distinct from explicit local cleanup.
// Only native IPC, the shipping CLI and testserver observations are used.
func TestControlPlaneLocalForgetAfterUnconfirmedLogout(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	output, err := n.ServiceCommand("local-forget")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "local-forget requires --confirm-local-forget") {
		t.Fatal("CLI did not require explicit local cleanup confirmation (output withheld)")
	}
	retained := func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
	}
	n.AwaitNativeStatus(retained)
	s.SetUnavailable(true)
	failed, _ := nativeLogoutAttempt(t, n, "00000000-0000-4000-8000-000000000001")
	assertNativeUnconfirmedLogout(t, failed)
	status := n.AwaitNativeStatus(retained)
	args := append(testclient.NativeMutationArguments("00000000-0000-4000-8000-000000000002", status), "--confirm-local-forget")
	response := &ipc.ForgetLocalEnrollmentResponse{}
	if err := n.NativeService("local-forget", response, args...); err != nil {
		t.Fatal(err)
	}
	if response.Operation == nil || response.Operation.Id == "" || response.Operation.Kind != ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT || response.Operation.ProfileId != status.ActiveProfileId {
		t.Fatal("local cleanup did not return its profile-bound operation")
	}
	completed := n.AwaitNativeOperation(response.Operation.Id)
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.GetCleanup().GetOutcome() != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_UNCONFIRMED || !completed.GetCleanup().GetLocalRegistrationRemoved() {
		t.Fatal("local cleanup incorrectly reported confirmed remote revocation")
	}
	clean := func(v *ipc.Status) bool {
		return nativeCleanupState(v) && v.GetStoredState().GetMapSigningTrustPresent()
	}
	n.AwaitNativeStatus(clean)
	s.SetUnavailable(false)
	n.Stop()
	before := len(s.Events())
	n.Start()
	n.AwaitNativeStatus(clean)
	replay := &ipc.ForgetLocalEnrollmentResponse{}
	if err := n.NativeService("local-forget", replay, args...); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replay.Operation, completed) {
		t.Fatal("local cleanup replay changed its durable outcome")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = testclient.Await(ctx, func() bool {
		for _, event := range s.Events()[before:] {
			if event.Kind == "registration-request" || event.Kind == "registered" || event.Kind == "registration-refreshed" {
				return true
			}
		}
		return false
	})
	if err == nil {
		t.Fatal("locally forgotten client attempted automatic reenrollment after restart")
	}
	for _, event := range s.Events() {
		if event.Kind == "deleted" || event.Kind == "logout" {
			t.Fatal("testserver unexpectedly confirmed remote cleanup")
		}
	}
}
