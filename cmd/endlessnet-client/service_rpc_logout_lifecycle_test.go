//go:build windows || linux || darwin

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/proto"
)

func TestNativeLogoutLifecycle(t *testing.T) {
	for _, mode := range []string{"confirmed", "remote failure"} {
		t.Run(mode, func(t *testing.T) { testNativeLogoutLifecycle(t, mode) })
	}
}

func testNativeLogoutLifecycle(t *testing.T, mode string) {
	t.Helper()
	var remoteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/nodes/node-1/endpoint" {
			// Teardown may notify a node whose revocation was already confirmed.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		remoteCalls.Add(1)
		if mode == "confirmed" && r.Method == http.MethodPost && r.URL.Path == "/auth/logout" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.Method != http.MethodDelete || r.URL.Path != "/nodes/node-1" {
			t.Error("unexpected remote cleanup request")
			http.NotFound(w, r)
			return
		}
		if mode == "confirmed" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeRecoveryPublicError(t, w, clientapi.ErrorCodeTemporarilyUnavailable, "remote-request-123")
	}))
	defer server.Close()
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-logout-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-logout-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	opts := agentIPCOptions{ConfigStore: store, StateOutput: statePath, OperationMu: &sync.Mutex{}, WireGuard: &testAgentWireGuard{}}
	if runtime.GOOS == "windows" {
		opts.Pipe = endpoint
	} else {
		opts.UnixSocket = endpoint
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	stop, _, err := startAgentRPC(ctx, cancel, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, timeout := context.WithTimeout(ctx, 10*time.Second)
	defer timeout()
	info, err := consumer.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mutation := func(id string) *ipc.MutationContext {
		t.Helper()
		response, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		meta := response.Msg.Status.Metadata
		return &ipc.MutationContext{RequestId: id, ExpectedInstanceId: meta.InstanceId, ExpectedRevision: meta.Revision}
	}
	created, err := consumer.CreateProfile(ctx, connect.NewRequest(&ipc.CreateProfileRequest{
		Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889001"), DisplayName: "logout fixture", ControlOrigin: "https://control.example.test",
	}))
	if err != nil {
		t.Fatal(err)
	}
	profile := &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}
	if err := store.Update(func(cfg *client.Config) error {
		cfg.RPCState.ActiveProfileID = profile.ProfileId
		cfg.ControlPlaneURLs = []string{server.URL}
		cfg.Token, cfg.ActiveAccountID = "synthetic-session", "account-1"
		cfg.IdentityPrivateKey, cfg.PrivateKey = "retained-identity-key", "retained-wireguard-key"
		cfg.NodeID, cfg.NetworkID, cfg.NodeCredential = "node-1", "network-1", "synthetic-credential"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := store.Read()
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	await := func(id string) *ipc.Operation {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			response, err := consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: id}}))
			if err != nil {
				t.Fatal(err)
			}
			op := response.Msg.Operation
			switch op.State {
			case ipc.OperationState_OPERATION_STATE_SUCCEEDED, ipc.OperationState_OPERATION_STATE_FAILED, ipc.OperationState_OPERATION_STATE_CANCELLED:
				return op
			}
			select {
			case <-ctx.Done():
				t.Fatal("native operation did not terminate")
			case <-ticker.C:
			}
		}
	}
	request := &ipc.LogoutRequest{Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889002"), Profile: profile}
	accepted, err := consumer.Logout(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	failed := await(accepted.Msg.Operation.Id)
	if mode == "confirmed" {
		if failed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || failed.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT ||
			failed.ProfileId != profile.ProfileId || failed.GetCleanup().GetOutcome() != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_CONFIRMED ||
			!failed.GetCleanup().GetLocalRegistrationRemoved() {
			t.Fatal("native logout did not commit confirmed cleanup")
		}
		after := store.Read()
		if after.NodeID != "" || after.NodeCredential != "" || after.Token != "" || after.ActiveAccountID != "" ||
			after.LocalOwnerID != before.LocalOwnerID || after.PrivateKey != before.PrivateKey || after.IdentityPrivateKey != before.IdentityPrivateKey ||
			after.ConnectionIntent == nil || after.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected {
			t.Fatal("confirmed native logout violated cleanup or identity retention")
		}
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			t.Fatal("confirmed logout retained stale agent snapshot")
		}
		replayed, err := consumer.Logout(ctx, connect.NewRequest(request))
		if err != nil || !proto.Equal(replayed.Msg.GetOperation(), failed) || remoteCalls.Load() != 2 {
			t.Fatal("confirmed logout replay repeated remote effects or changed result")
		}
		return
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatal("remote refusal removed agent snapshot before teardown")
	}
	if failed.State != ipc.OperationState_OPERATION_STATE_FAILED || failed.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT ||
		failed.ProfileId != profile.ProfileId || failed.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED ||
		failed.GetFailure().GetControlRequestId() != "remote-request-123" {
		t.Fatal("native logout lost terminal failure classification or correlation")
	}
	preserved := store.Read()
	if preserved.Token != before.Token || preserved.NodeCredential != before.NodeCredential || preserved.LocalOwnerID != before.LocalOwnerID {
		t.Fatal("remote failure deleted local enrollment or ownership")
	}
	replayed, err := consumer.Logout(ctx, connect.NewRequest(request))
	if err != nil || !proto.Equal(replayed.Msg.GetOperation(), failed) || remoteCalls.Load() != 1 {
		t.Fatal("exact replay retried failed remote cleanup or changed its result")
	}
	forgotten, err := consumer.ForgetLocalEnrollment(ctx, connect.NewRequest(&ipc.ForgetLocalEnrollmentRequest{
		Mutation: mutation("cdf16435-772e-47dc-86d4-475ae7889003"), Profile: profile, Confirmed: true,
	}))
	if info.CallerAccess != ipc.Access_ACCESS_ADMINISTRATOR {
		if connect.CodeOf(err) != connect.CodePermissionDenied || store.Read().NodeCredential != before.NodeCredential {
			t.Fatal("nonadministrator local forget bypassed authorization or changed registration")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	completed := await(forgotten.Msg.Operation.Id)
	if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED ||
		completed.Kind != ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT || completed.ProfileId != profile.ProfileId ||
		completed.GetCleanup().GetOutcome() != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_UNCONFIRMED ||
		!completed.GetCleanup().GetLocalRegistrationRemoved() || completed.GetCleanup().GetControlRequestId() != "remote-request-123" {
		t.Fatal("local forget did not report unconfirmed remote cleanup")
	}
	after := store.Read()
	if after.NodeCredential != "" || after.Token != "" || after.ActiveAccountID != "" || after.NodeID != "" || after.NetworkID != "" ||
		after.ConnectionIntent == nil || after.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected {
		t.Fatal("local forget retained registration or session")
	}
	if after.LocalOwnerID != before.LocalOwnerID || after.IdentityPrivateKey != before.IdentityPrivateKey || after.PrivateKey != before.PrivateKey || remoteCalls.Load() != 1 {
		t.Fatal("local forget changed installation identity or repeated remote cleanup")
	}
}
