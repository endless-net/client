package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestAgentRPCRecoveryFailureMapping(t *testing.T) {
	for _, tc := range []struct {
		remote              clientapi.ErrorCode
		code                ipc.ErrorCode
		terminal, retryable bool
	}{
		{clientapi.ErrorCodeNodeCredentialRevoked, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, true, false},
		{clientapi.ErrorCodeTemporarilyUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, false, true},
		{clientapi.ErrorCodeAuthenticationRequired, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN, false, false},
		{clientapi.ErrorCodeAuthorizationDenied, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED, false, false},
		{clientapi.ErrorCodeNodeIdentityBindingMismatch, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, false, false},
	} {
		t.Run(string(tc.remote), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeRecoveryPublicError(t, w, tc.remote, "request-id") }))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			result, err := agentRPCTrustRecovery(t.Context(), cfg)
			if err != nil || result.Configuration != nil || result.Failure == nil || result.Failure.Code != tc.code || result.Failure.Retryable != tc.retryable || result.RequiresEnrollment != tc.terminal || result.Failure.ControlRequestId != "request-id" {
				t.Fatal("incorrect native recovery mapping", err)
			}
		})
	}
}
