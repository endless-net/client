package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeEnrollmentRetryClassification(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, io.ErrUnexpectedEOF, &net.OpError{Op: "dial", Err: errors.New("connection refused")}} {
		if !retryableRPCEnrollmentError(fmt.Errorf("registration: %w", err)) {
			t.Fatal("transport error not retryable")
		}
	}
	for _, err := range []error{nil, context.Canceled, x509.UnknownAuthorityError{}, errors.New("registration identity mismatch")} {
		if retryableRPCEnrollmentError(err) {
			t.Fatal("terminal error classified as retryable")
		}
	}
	for _, status := range []int{400, 401, 403, 409, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "synthetic failure", status) }))
			defer server.Close()
			api := clientapi.NewAPI(server.URL, "")
			var retryableTransport bool
			api.HTTPClient.Transport = enrollmentContextTransport{lifetime: t.Context(), outcome: func(status int, err error) {
				retryableTransport = retryableEnrollmentHTTPStatus(status) || retryableRPCEnrollmentError(err)
			}}
			_, err := api.NodeEnrollmentRequestStatus("request", "synthetic-poll-token")
			want := status == 429 || status >= 500
			if err == nil || (retryableTransport || retryableRPCEnrollmentError(err)) != want {
				t.Fatalf("status %d retry classification mismatch", status)
			}
		})
	}
}

func TestAgentRPCEnrollUsesTypedWorkflowAndPendingAction(t *testing.T) {
	setInstallationStateDirForTest(t, filepath.Join(t.TempDir(), "installation-state"))
	server, snapshot := testPendingEnrollmentServer(t, testMapSigningKey(t), "synthetic-join-token")
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "client.json")
	action, err := agentRPCEnroll(t.Context(), client.Config{ControlPlaneURLs: []string{server.URL}}, client.ClientRPCEnrollmentInput{
		OperationID: "2e8e2f42-1fcf-4110-b279-4a1b999f2034", Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_SERVER,
		Hostname: "native-node", Token: "synthetic-join-token",
	}, func(cfg client.Config) error { return client.SaveConfig(configPath, cfg) })
	if err != nil {
		t.Fatal(err)
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if action.GetKind() != ipc.UserAction_KIND_WAIT_FOR_APPROVAL || saved.NodeID != "node-pending" || saved.CachedMap != nil || saved.NodeApprovalState != clientapi.NodeApprovalPending {
		t.Fatal("native provider did not save verified restricted enrollment")
	}
	request, calls := snapshot()
	if calls != 1 || request.Hostname != "native-node" || len(request.Tags) != 1 || request.Tags[0] != "mode:server" {
		t.Fatal("native registration input lost")
	}
	status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, saved, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
	if status.ServiceState != ipc.ServiceState_SERVICE_STATE_NEEDS_APPROVAL || status.ControlState != ipc.ControlState_CONTROL_STATE_PENDING_APPROVAL ||
		status.GetPendingAction().GetKind() != ipc.UserAction_KIND_WAIT_FOR_APPROVAL || status.GetStoredState().GetCachedMapValid() {
		t.Fatal("pending native status claimed usable enrollment")
	}
	engine := &testAgentWireGuard{}
	err = agentRPCProfileDriver(agentIPCOptions{WireGuard: engine}).Start(t.Context(), saved)
	if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT || engine.configureCalls != 0 {
		t.Fatal("pending enrollment reached tunnel configuration")
	}
	after, err := client.LoadConfig(configPath)
	if err != nil || !reflect.DeepEqual(saved, after) {
		t.Fatal("rejected pending connection changed persisted approval state")
	}
}
