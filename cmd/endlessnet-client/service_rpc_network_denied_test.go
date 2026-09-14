package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func TestNetworkRegistrationDenialPreservesUncertainAttempt(t *testing.T) {
	for _, mode := range []string{"validated", "malformed", "mismatched_request_id"} {
		t.Run(mode, func(t *testing.T) {
			setInstallationStateDirForTest(t, t.TempDir())
			server, _ := testPendingEnrollmentServer(t, testMapSigningKey(t), "", nil)
			defer server.Close()
			original := server.Config.Handler
			var requests []api.RegisterNodeRequest
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/nodes/register" {
					if r.Method == http.MethodDelete || r.URL.Path == "/auth/logout" {
						t.Error("unresolved registration attempted unbound cleanup")
					}
					original.ServeHTTP(w, r)
					return
				}
				var request api.RegisterNodeRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Validate() != nil {
					t.Error("invalid registration request")
				}
				requests = append(requests, request)
				if mode == "malformed" {
					http.Error(w, "not a producer rejection", http.StatusForbidden)
					return
				}
				if mode == "mismatched_request_id" {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set(controlRequestIDHeader, "different-correlation")
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: api.ErrorCodeAuthorizationDenied, RequestID: "synthetic-correlation", DiagnosticMessage: "private upstream diagnostic"})
					return
				}
				writeRecoveryPublicError(t, w, api.ErrorCodeAuthorizationDenied, "synthetic-correlation")
			})
			identity, err := client.GenerateIdentityPrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			private, err := wgkeys.GeneratePrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			cfg := client.Config{ControlPlaneURLs: []string{server.URL}, NetworkID: "net-pending", ActiveAccountID: "account", Token: "synthetic-shared-session", IdentityPrivateKey: identity, PrivateKey: private}
			input := client.ClientRPCNetworkRegistrationInput{OperationID: "82366b0c-52c9-43e9-bebb-b984445157fc", NetworkID: "net-pending", Hostname: "target-host"}
			save := func(next client.Config) error { cfg = next; return nil }
			_, err = agentRPCRegisterNetworkTarget(t.Context(), cfg, input, save)
			assertDenied := func(err error) {
				t.Helper()
				if err == nil {
					t.Fatal("denied registration was accepted")
				}
				failure := rpc.FailureFromError(err)
				if mode == "validated" {
					if failure == nil || failure.Code != ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED {
						t.Fatal("validated denial lost safe RPC classification", err)
					}
				} else if failure != nil && failure.Code == ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED {
					t.Fatal("unvalidated response was trusted as authorization denial")
				}
			}
			assertDenied(err)
			pending := cfg.PendingDirectRegistration
			if pending == nil || cfg.NodeID != "" || cfg.NodeCredential != "" || cfg.CachedMap != nil || len(requests) != 1 {
				t.Fatal("denial discarded unresolved request or installed authority")
			}
			err = agentRPCCleanupNetworkTarget(t.Context(), cfg, input, save)
			assertDenied(err)
			if !reflect.DeepEqual(cfg.PendingDirectRegistration, pending) || len(requests) != 2 || !reflect.DeepEqual(requests[0], requests[1]) || cfg.Token != "synthetic-shared-session" {
				t.Fatal("compensation changed uncertain request or shared session")
			}
		})
	}
}
