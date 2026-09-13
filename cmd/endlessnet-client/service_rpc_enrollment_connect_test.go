package main

import (
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNativeEnrollmentAndConnectHaveIndependentOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "degraded control", "configure error", "configure rejected"} {
		t.Run(mode, func(t *testing.T) {
			setInstallationStateDirForTest(t, t.TempDir())
			path := filepath.Join(t.TempDir(), "client.json")
			server, snapshot := testEnrollmentServer(t, testMapSigningKey(t), "synthetic-enrollment-token", 7)
			defer server.Close()
			original := server.Config.Handler
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "degraded control" && r.URL.Path == "/client/readyz" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				original.ServeHTTP(w, r)
			})
			const operationID = "9e15d2c8-13e0-47cd-bb0d-293e41471001"
			action, err := agentRPCEnroll(t.Context(), client.Config{ControlPlaneURLs: []string{server.URL}},
				client.ClientRPCEnrollmentInput{OperationID: operationID, Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE,
					Hostname: "native-enrolled", Token: "synthetic-enrollment-token"},
				func(cfg client.Config) error { return client.SaveConfig(path, cfg) })
			if err != nil || action != nil {
				t.Fatal("native registration did not complete")
			}
			request, calls := snapshot()
			if calls != 1 || request.Hostname != "native-enrolled" || request.IdempotencyID != operationID ||
				request.JoinToken != "synthetic-enrollment-token" || !containsString(request.Tags, "mode:interactive") {
				t.Fatal("native enrollment lost registration input or repeated registration")
			}
			enrolled, err := client.LoadConfig(path)
			if err != nil || enrolled.NodeID != "node-1" || !strings.HasPrefix(enrolled.NodeCredential, "enc_") ||
				enrolled.CachedMap == nil || enrolled.CachedMap.Network.Revision != 7 {
				t.Fatal("native enrollment did not persist verified registration")
			}
			wake := make(chan struct{}, 1)
			engine := &testAgentWireGuard{configure: func(client.Config, api.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
				if mode == "configure error" {
					return client.WireGuardApplyResult{}, errors.New("synthetic private driver failure")
				}
				return client.WireGuardApplyResult{OK: mode != "configure rejected"}, nil
			}}
			err = agentRPCProfileDriver(agentIPCOptions{WireGuard: engine, SyncWake: wake}).Start(t.Context(), enrolled)
			failed := mode == "configure error" || mode == "configure rejected"
			if failed {
				if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || len(wake) != 0 {
					t.Fatal("failed native apply was reported successful or woke the agent")
				}
			} else {
				if err != nil || len(wake) != 1 {
					t.Fatal("successful native apply failed or did not wake the agent")
				}
				status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, enrolled, ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED)
				if mode == "degraded control" && (status.ServiceState != ipc.ServiceState_SERVICE_STATE_DEGRADED ||
					status.ControlState != ipc.ControlState_CONTROL_STATE_DEGRADED || status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED) {
					t.Fatal("readiness failure erased the successful tunnel outcome")
				}
			}
			after, loadErr := client.LoadConfig(path)
			if loadErr != nil || !reflect.DeepEqual(enrolled, after) || engine.configureCalls != 1 {
				t.Fatal("native apply altered registration or repeated configuration")
			}
		})
	}
}
