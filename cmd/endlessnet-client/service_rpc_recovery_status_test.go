package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNativeStatusProjectsDurableRecoveryWithoutProbing(t *testing.T) {
	for _, tc := range []struct {
		phase   client.RecoveryPhase
		state   ipc.ServiceState
		control ipc.ControlState
		code    ipc.ErrorCode
	}{
		{client.RecoveryPhaseRecovering, ipc.ServiceState_SERVICE_STATE_RECOVERING, ipc.ControlState_CONTROL_STATE_RECOVERING, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{client.RecoveryPhaseBlocked, ipc.ServiceState_SERVICE_STATE_RECOVERY_BLOCKED, ipc.ControlState_CONTROL_STATE_RECOVERY_BLOCKED, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{client.RecoveryPhasePolicyBlocked, ipc.ServiceState_SERVICE_STATE_POLICY_BLOCKED, ipc.ControlState_CONTROL_STATE_POLICY_BLOCKED, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED},
		{client.RecoveryPhaseNeedsLogin, ipc.ServiceState_SERVICE_STATE_NEEDS_LOGIN, ipc.ControlState_CONTROL_STATE_NEEDS_LOGIN, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN},
	} {
		t.Run(string(tc.phase), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg.EnrollmentRecovery.Phase = tc.phase
			cfg.EnrollmentRecovery.RequestID = "synthetic-control-correlation"
			cfg.EnrollmentRecovery.ErrorCode = "synthetic-private-error-text"
			cfg.EnrollmentRecovery.Retryable = tc.phase == client.RecoveryPhaseRecovering
			before := *cfg.EnrollmentRecovery
			status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
			if status.ServiceState != tc.state || status.ControlState != tc.control || status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
				t.Fatal("durable recovery state was replaced by enrollment/cache presence")
			}
			progress := status.GetRecovery()
			if progress.GetState() != tc.state || progress.GetOperationId() != before.OperationID || progress.GetFailure().GetCode() != tc.code || progress.GetFailure().GetRetryable() != before.Retryable || progress.GetFailure().GetControlRequestId() != before.RequestID {
				t.Fatal("native recovery lost phase, operation or correlation")
			}
			if calls.Load() != 0 || status.Control != nil || status.Network != nil || !reflect.DeepEqual(before, *cfg.EnrollmentRecovery) {
				t.Fatal("recovery projection probed control, adopted stale map or changed durable state")
			}
			encoded, err := protojson.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			for _, withheld := range []string{cfg.Token, cfg.NodeCredential, cfg.IdentityPrivateKey, cfg.PrivateKey, before.ErrorCode} {
				if withheld != "" && strings.Contains(string(encoded), withheld) {
					t.Fatal("recovery status disclosed private material")
				}
			}
		})
	}
}
