package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestAgentCredentialTransportValidatesTerminalResponse(t *testing.T) {
	for _, tc := range []struct {
		name                string
		code                api.ErrorCode
		header, contentType string
		status              int
		want                bool
	}{
		{"revoked", api.ErrorCodeNodeCredentialRevoked, "request-1", "application/json", 401, true},
		{"expired", api.ErrorCodeNodeCredentialExpired, "request-1", "application/json", 401, true},
		{"unknown", api.ErrorCodeNodeCredentialUnknown, "request-1", "application/json", 401, true},
		{"invalid", api.ErrorCodeNodeCredentialInvalid, "request-1", "application/json", 401, false},
		{"temporary", api.ErrorCodeTemporarilyUnavailable, "request-1", "application/json", 503, false},
		{"request-mismatch", api.ErrorCodeNodeCredentialRevoked, "request-2", "application/json", 401, false},
		{"missing-request-id", api.ErrorCodeNodeCredentialRevoked, "", "application/json", 401, false},
		{"wrong-type", api.ErrorCodeNodeCredentialRevoked, "request-1", "text/plain", 401, false},
		{"wrong-status", api.ErrorCodeNodeCredentialRevoked, "request-1", "application/json", 403, false},
		{"unknown-code", api.ErrorCode("unknown"), "request-1", "application/json", 401, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set(controlRequestIDHeader, tc.header)
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: tc.code, RequestID: "request-1", DiagnosticMessage: "test failure"})
			}))
			defer s.Close()
			a := api.NewAPIWithNodeCredentialURLs([]string{s.URL}, "", "test-credential")
			transport := &agentCredentialTransport{nodeID: "node-1", credential: "test-credential"}
			a.HTTPClient.Transport = transport
			_, err := a.ReadMapStreamEvent("node-1", api.MapCursor{}, time.Second)
			if err == nil {
				t.Fatal("error response unexpectedly succeeded")
			}
			if (transport.terminal != nil) != tc.want {
				t.Fatal("incorrect terminal classification")
			}
			transport.terminal = nil
			_, _ = a.ServerKey()
			if transport.terminal != nil {
				t.Fatal("server-key failure authorized credential deletion")
			}
		})
	}
}

func TestAgentTerminalCredentialCleanupPreservesDeviceState(t *testing.T) {
	for _, mode := range []string{"success", "teardown-failure", "credential-replaced", "transient"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newRecoveryTestFixture(t, "http://127.0.0.1:1")
			store, err := client.OpenConfigStore(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Read()
			wg := &testAgentWireGuard{}
			var failure error = &terminalAgentCredential{code: api.ErrorCodeNodeCredentialRevoked, nodeID: before.NodeID, credential: before.NodeCredential}
			if mode == "teardown-failure" {
				wg.down = func() (client.WireGuardApplyResult, error) {
					return client.WireGuardApplyResult{}, errors.New("teardown failed")
				}
			}
			if mode == "credential-replaced" {
				failure = &terminalAgentCredential{code: api.ErrorCodeNodeCredentialRevoked, nodeID: before.NodeID, credential: "obsolete"}
			}
			if mode == "transient" {
				failure = errors.New("temporary outage")
			}
			handled, err := handleAgentTerminalCredential(context.Background(), agentIPCOptions{ConfigPath: fixture.ConfigPath, ConfigStore: store, WireGuard: wg}, failure)
			after := store.Read()
			if mode == "success" {
				if !handled || err != nil || wg.downCalls != 1 || after.NodeID != "" || after.NodeCredential != "" || after.CachedMap != nil {
					t.Fatal("terminal cleanup failed")
				}
				if after.IdentityPrivateKey != before.IdentityPrivateKey || after.PrivateKey != before.PrivateKey || after.LocalOwnerID != before.LocalOwnerID || after.Token != before.Token || !reflect.DeepEqual(after.MapSigningTrust, before.MapSigningTrust) || !reflect.DeepEqual(after.ConnectionIntent, before.ConnectionIntent) {
					t.Fatal("terminal cleanup removed retained state")
				}
			} else if after.NodeID != before.NodeID || after.NodeCredential != before.NodeCredential || !reflect.DeepEqual(after.CachedMap, before.CachedMap) {
				t.Fatal("non-terminal or failed cleanup changed enrollment")
			}
		})
	}
}
