package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientapiv2 "github.com/endless-net/client-api/clientapi/v2"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"
)

type recoveryTestFixture struct {
	ConfigPath    string
	OldSigningKey ed25519.PrivateKey
	NewSigningKey ed25519.PrivateKey
	OldCredential string
	NewTrust      clientapi.SigningTrustBundle
}

func newRecoveryTestFixture(t *testing.T, controlURL string) recoveryTestFixture {
	t.Helper()
	setInstallationStateDirForTest(t, filepath.Join(t.TempDir(), "installation-state"))
	oldKey := testMapSigningKey(t)
	newKey := testMapSigningKey(t)
	identityPrivateKey, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	wireGuardPrivateKey, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	wireGuardPublicKey, err := wgkeys.PublicKey(wireGuardPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	identityPublicKey, err := client.IdentityPublicKey(identityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := client.DeviceFingerprint(controlURL, wireGuardPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	oldCredential, err := clientapi.SignNodeCredential(oldKey, "network-1", "node-1", []string{"node:register", "node:map"}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	registrationBindingSum := sha256.Sum256([]byte("prior-registration-binding"))
	oldMap := clientapi.RegisterNodeResponse{
		Revision: clientapi.MapRevision{Network: 7},
		Network:  clientapi.Network{ID: "network-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 7},
		Node: clientapi.Node{
			ID: "node-1", NetworkID: "network-1", Hostname: "recovery-node",
			IdentityPublicKey: identityPublicKey, PublicKey: wireGuardPublicKey, DeviceFingerprint: fingerprint,
			AssignedIP: "100.64.0.2", ApprovalState: clientapi.NodeApprovalApproved,
			Endpoint: "198.51.100.5:51820", EndpointGeneration: 4,
			EndpointCandidates: []string{"198.51.100.5:51820"}, AdvertisedIPs: []string{"10.10.0.0/16"}, RequestedTags: []string{"role:test"},
		},
		RegistrationBinding: base64.RawURLEncoding.EncodeToString(registrationBindingSum[:]),
	}
	oldMap.MapSignature, err = clientapi.SignNetworkMap(oldKey, oldMap)
	if err != nil {
		t.Fatal(err)
	}
	newPublic := base64.RawURLEncoding.EncodeToString(newKey.Public().(ed25519.PublicKey))
	newTrust, err := clientapi.NewSigningTrustBundle(newPublic)
	if err != nil {
		t.Fatal(err)
	}
	idempotencyID, err := clientapiv2.NewRegistrationIdempotencyID()
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := client.NewEnrollmentRecovery("operation-1", idempotencyID, controlURL, newTrust.ActiveKeyID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "client.json")
	cfg := client.Config{
		LocalOwnerID: "uid:1000", ControlPlaneURLs: []string{controlURL}, ManagementURL: "https://management.example.test",
		Token: "session-token", ActiveAccountID: "account-1", IdentityPrivateKey: identityPrivateKey, PrivateKey: wireGuardPrivateKey,
		NodeID: "node-1", NetworkID: "network-1", NodeCredential: oldCredential, NodeApprovalState: clientapi.NodeApprovalApproved,
		DeviceFingerprint: fingerprint, MapSigningTrust: &newTrust, MapRevision: 7, CachedMap: &oldMap,
		ConnectionIntent:   &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredConnected, Reason: "user_connect", UpdatedAt: time.Now().UTC().Format(time.RFC3339)},
		EnrollmentRecovery: &recovery,
	}
	if err := client.SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	return recoveryTestFixture{ConfigPath: configPath, OldSigningKey: oldKey, NewSigningKey: newKey, OldCredential: oldCredential, NewTrust: newTrust}
}

func writeRecoveryPublicError(t *testing.T, w http.ResponseWriter, code clientapiv2.ErrorCode, requestID string) {
	t.Helper()
	value, err := clientapiv2.NewPublicError(code, "safe recovery diagnostic", requestID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := clientapiv2.MarshalPublicError(value)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := code.HTTPStatus()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(controlRequestIDHeader, requestID)
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func TestEnrollmentRecoveryTerminalAndPreservationMatrix(t *testing.T) {
	tests := []struct {
		name      string
		code      clientapiv2.ErrorCode
		terminal  bool
		phase     client.RecoveryPhase
		retryable bool
	}{
		{"unknown", clientapiv2.ErrorCodeNodeCredentialUnknown, true, "", false},
		{"revoked", clientapiv2.ErrorCodeNodeCredentialRevoked, true, "", false},
		{"expired", clientapiv2.ErrorCodeNodeCredentialExpired, true, "", false},
		{"invalid", clientapiv2.ErrorCodeNodeCredentialInvalid, false, client.RecoveryPhaseBlocked, false},
		{"binding", clientapiv2.ErrorCodeNodeIdentityBindingMismatch, false, client.RecoveryPhaseBlocked, false},
		{"renewal-required", clientapiv2.ErrorCodeNodeCredentialRenewalRequired, false, client.RecoveryPhaseBlocked, false},
		{"authentication", clientapiv2.ErrorCodeAuthenticationRequired, false, client.RecoveryPhaseNeedsLogin, false},
		{"authorization", clientapiv2.ErrorCodeAuthorizationDenied, false, client.RecoveryPhasePolicyBlocked, false},
		{"transient", clientapiv2.ErrorCodeTemporarilyUnavailable, false, client.RecoveryPhaseRecovering, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/nodes/register" {
					http.NotFound(w, r)
					return
				}
				writeRecoveryPublicError(t, w, tc.code, "request-"+tc.name)
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			before, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			result, err := continueEnrollmentRecovery(context.Background(), fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			after, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.terminal {
				if !result.Terminal || !result.Completed || after.EnrollmentRecovery != nil || after.NodeID != "" || after.NetworkID != "" || after.NodeCredential != "" || after.CachedMap != nil {
					t.Fatalf("terminal result/config = %#v / %#v", result, after.EnrollmentRecovery)
				}
			} else {
				if result.Terminal || result.Completed || after.EnrollmentRecovery == nil || after.EnrollmentRecovery.Phase != tc.phase || after.EnrollmentRecovery.ErrorCode != string(tc.code) || after.EnrollmentRecovery.Retryable != tc.retryable {
					t.Fatalf("preserved result/config = %#v / %#v", result, after.EnrollmentRecovery)
				}
				if after.NodeID != before.NodeID || after.NetworkID != before.NetworkID || after.NodeCredential != before.NodeCredential || after.CachedMap == nil {
					t.Fatal("non-terminal error changed node-bound state")
				}
			}
			if after.LocalOwnerID != before.LocalOwnerID || after.Token != before.Token || after.ActiveAccountID != before.ActiveAccountID ||
				after.IdentityPrivateKey != before.IdentityPrivateKey || after.PrivateKey != before.PrivateKey || after.DeviceFingerprint != before.DeviceFingerprint ||
				after.MapSigningTrust == nil || after.MapSigningTrust.ActiveKeyID != before.MapSigningTrust.ActiveKeyID || !reflect.DeepEqual(after.ConnectionIntent, before.ConnectionIntent) {
				t.Fatal("recovery failure violated key/owner/trust/session/intent retention")
			}
		})
	}
}

func TestEnrollmentRecoveryRejectsMalformedAndTextOnlyErrorsWithoutCleanup(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{"plain-text-terminal-words", "text/plain", "node_credential_unknown revoked expired"},
		{"unknown-code", "application/json", `{"schema_version":2,"error_code":"future_terminal_code","diagnostic_message":"node_credential_unknown","request_id":"request-1"}`},
		{"generic-json", "application/json", `{"error":"invalid node credential"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			result, _ := continueEnrollmentRecovery(context.Background(), fixture.ConfigPath)
			cfg, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if result.Terminal || cfg.EnrollmentRecovery == nil || cfg.EnrollmentRecovery.Phase != client.RecoveryPhaseBlocked || cfg.EnrollmentRecovery.ErrorCode != recoveryErrorProtocol || cfg.NodeCredential != fixture.OldCredential {
				t.Fatalf("malformed failure caused cleanup: result=%#v recovery=%#v", result, cfg.EnrollmentRecovery)
			}
		})
	}
}

func TestEnrollmentRecoveryRetriesSameRequestAfterLostResponse(t *testing.T) {
	var mu sync.Mutex
	var firstRequest clientapiv2.RegisterNodeRequest
	var durableResponse clientapiv2.RegisterNodeResponse
	registerCalls := 0
	var fixture recoveryTestFixture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nodes/register":
			req, err := clientapiv2.DecodeRegisterNodeRequest(r.Body)
			if err != nil {
				t.Errorf("decode renewal: %v", err)
				return
			}
			mu.Lock()
			registerCalls++
			if registerCalls == 1 {
				firstRequest = req
				durableResponse = recoverySuccessResponse(t, fixture.NewSigningKey, req)
			} else if !reflect.DeepEqual(req, firstRequest) {
				t.Errorf("retry request changed\nfirst: %#v\nretry: %#v", firstRequest, req)
			}
			response := durableResponse
			call := registerCalls
			mu.Unlock()
			if call == 1 {
				hijacker := w.(http.Hijacker)
				conn, _, err := hijacker.Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponseFromBundle(fixture.NewTrust))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fixture = newRecoveryTestFixture(t, server.URL)
	before, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := continueEnrollmentRecovery(context.Background(), fixture.ConfigPath)
	if err == nil || first.Retryable != true || first.Completed {
		t.Fatalf("lost response result = %#v, err=%v", first, err)
	}
	afterLost, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if afterLost.EnrollmentRecovery == nil || afterLost.EnrollmentRecovery.IdempotencyID != before.EnrollmentRecovery.IdempotencyID || afterLost.NodeCredential != fixture.OldCredential {
		t.Fatal("lost response did not preserve retry identity and old credential")
	}
	second, err := continueEnrollmentRecovery(context.Background(), fixture.ConfigPath)
	if err != nil || !second.Completed || second.Terminal {
		t.Fatalf("retry result = %#v, err=%v", second, err)
	}
	after, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.EnrollmentRecovery != nil || after.NodeCredential == fixture.OldCredential || after.CachedMap == nil || after.MapRevision != 8 {
		t.Fatalf("validated durable result was not atomically committed: %#v", after.EnrollmentRecovery)
	}
}

func TestTrustServerPersistsTrustAndIntentBeforeRenewalAndIsIdempotent(t *testing.T) {
	var fixture recoveryTestFixture
	var mu sync.Mutex
	requestIDs := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponseFromBundle(fixture.NewTrust))
		case "/nodes/register":
			req, err := clientapiv2.DecodeRegisterNodeRequest(r.Body)
			if err != nil {
				t.Errorf("decode renewal: %v", err)
				return
			}
			persisted, err := client.LoadConfig(fixture.ConfigPath)
			if err != nil {
				t.Errorf("load persisted recovery: %v", err)
				return
			}
			if persisted.MapSigningTrust == nil || persisted.MapSigningTrust.ActiveKeyID != fixture.NewTrust.ActiveKeyID || persisted.EnrollmentRecovery == nil || persisted.EnrollmentRecovery.IdempotencyID != req.IdempotencyID {
				t.Errorf("renewal escaped before atomic trust+intent save: %#v", persisted.EnrollmentRecovery)
			}
			mu.Lock()
			requestIDs = append(requestIDs, req.IdempotencyID)
			mu.Unlock()
			writeRecoveryPublicError(t, w, clientapiv2.ErrorCodeTemporarilyUnavailable, "request-unavailable")
		case "/client/readyz":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fixture = newRecoveryTestFixture(t, server.URL)
	oldPublic := base64.RawURLEncoding.EncodeToString(fixture.OldSigningKey.Public().(ed25519.PublicKey))
	oldTrust, err := clientapi.NewSigningTrustBundle(oldPublic)
	if err != nil {
		t.Fatal(err)
	}
	store, err := client.OpenConfigStore(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *client.Config) error {
		cfg.MapSigningTrust = &oldTrust
		cfg.EnrollmentRecovery = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	handler := agentIPCHandlers(agentIPCOptions{ConfigPath: fixture.ConfigPath})
	request := ipc.TrustServerRequest{ConfirmedControlOrigin: server.URL, ConfirmedKeyID: fixture.NewTrust.ActiveKeyID}
	first, err := handler.TrustServer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := handler.TrustServer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != ipc.RecoveryOutcomeAccepted || second.Outcome != ipc.RecoveryOutcomeAlreadyApplied || first.OperationID != second.OperationID || first.State != ipc.StateRecovering || second.State != ipc.StateRecovering {
		t.Fatalf("trust operation was not idempotent: first=%#v second=%#v", first, second)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requestIDs) != 2 || requestIDs[0] != requestIDs[1] {
		t.Fatalf("renewal idempotency IDs = %#v", requestIDs)
	}
}

func recoverySuccessResponse(t *testing.T, signingKey ed25519.PrivateKey, req clientapiv2.RegisterNodeRequest) clientapiv2.RegisterNodeResponse {
	t.Helper()
	credential, err := clientapi.SignNodeCredential(signingKey, req.NetworkID, "node-1", []string{"node:register", "node:map"}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	response := clientapiv2.RegisterNodeResponse{
		SchemaVersion: clientapiv2.SchemaVersion, IdempotencyID: req.IdempotencyID,
		Revision: clientapi.MapRevision{Network: 8},
		Network:  clientapi.Network{ID: req.NetworkID, Name: "default", CIDR: "100.64.0.0/24", Revision: 8},
		Node: clientapi.Node{
			ID: "node-1", NetworkID: req.NetworkID, Hostname: req.Hostname,
			IdentityPublicKey: req.IdentityPublicKey, PublicKey: req.PublicKey, DeviceFingerprint: req.DeviceFingerprint,
			AssignedIP: "100.64.0.2", ApprovalState: clientapi.NodeApprovalApproved,
			Endpoint: req.Endpoint, EndpointGeneration: req.EndpointGeneration,
			EndpointCandidates: append([]string(nil), req.EndpointCandidates...), AdvertisedIPs: append([]string(nil), req.AdvertisedIPs...), RequestedTags: append([]string(nil), req.Tags...),
		},
		RegistrationBinding: clientapiv2.RegistrationIdentityProofBinding(req),
		NodeCredential:      credential,
	}
	v1Response := response.NetworkMap()
	response.MapSignature, err = clientapi.SignNetworkMap(signingKey, v1Response)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.ValidateForRequest(req); err != nil {
		t.Fatalf("invalid success fixture: %v", err)
	}
	return response
}

func TestRecoveryStatusMapsAllDocumentedStates(t *testing.T) {
	tests := []struct {
		phase   client.RecoveryPhase
		state   ipc.ServiceState
		control ipc.ControlState
	}{
		{client.RecoveryPhaseRecovering, ipc.StateRecovering, ipc.ControlStateRecovering},
		{client.RecoveryPhaseBlocked, ipc.StateRecoveryBlocked, ipc.ControlStateRecoveryBlocked},
		{client.RecoveryPhasePolicyBlocked, ipc.StatePolicyBlocked, ipc.ControlStatePolicyBlocked},
		{client.RecoveryPhaseNeedsLogin, ipc.StateNeedsLogin, ipc.ControlStateNeedsLogin},
	}
	for _, tc := range tests {
		state, control := ipcRecoveryState(tc.phase)
		if state != tc.state || control != tc.control {
			t.Fatalf("phase %q = %q/%q, want %q/%q", tc.phase, state, control, tc.state, tc.control)
		}
	}
}

func TestDurableRecoveryStateOverridesStaleAgentSigningError(t *testing.T) {
	recovery, err := client.NewEnrollmentRecovery("operation-1", "registration-id-1", "https://control.example.test", "key-2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{NodeID: "node-1", NetworkID: "network-1", NodeCredential: "credential", EnrollmentRecovery: &recovery}
	snapshot := client.AgentSnapshot{NodeID: "node-1", NetworkID: "network-1", LastError: serverMapSigningTrustChangedError}
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := client.OpenConfigStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	status := agentIPCStatusForConfig(context.Background(), agentIPCOptions{ConfigPath: configPath, ConfigStore: store}, cfg, &snapshot)
	if status.State != ipc.StateRecovering || status.ControlState != ipc.ControlStateRecovering || status.Recovery == nil || status.Recovery.OperationID != recovery.OperationID {
		t.Fatalf("stale agent error replaced durable recovery state: %#v", status)
	}
}

func TestConnectReturnsRecoveryStateInsteadOfTextMatchedCleanup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    clientapiv2.ErrorCode
		state   ipc.ServiceState
		control ipc.ControlState
	}{
		{"terminal", clientapiv2.ErrorCodeNodeCredentialRevoked, ipc.StateNeedsEnrollment, ipc.ControlStateNotRegistered},
		{"binding", clientapiv2.ErrorCodeNodeIdentityBindingMismatch, ipc.StateRecoveryBlocked, ipc.ControlStateRecoveryBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/nodes/register" {
					writeRecoveryPublicError(t, w, tc.code, "connect-request")
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			fixture := newRecoveryTestFixture(t, server.URL)
			response, err := agentIPCHandlers(agentIPCOptions{ConfigPath: fixture.ConfigPath}).Connect(context.Background(), ipc.ConnectRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if response.State != tc.state || response.ControlState != tc.control {
				t.Fatalf("connect recovery response = %#v", response)
			}
		})
	}
}

func TestRecoveryControlEndpointRejectsCredentialExfiltrationURLs(t *testing.T) {
	for _, raw := range []string{
		"http://control.example.test", "https://user:password@control.example.test", "https://control.example.test?redirect=https://evil.test", "file:///tmp/control",
	} {
		if endpoint, err := recoveryControlEndpoint(raw); err == nil {
			t.Fatalf("unsafe control URL %q produced %q", raw, endpoint)
		}
	}
	if endpoint, err := recoveryControlEndpoint("http://localhost:8080/base/"); err != nil || !strings.HasSuffix(endpoint, "/base/nodes/register") {
		t.Fatalf("loopback development URL = %q, %v", endpoint, err)
	}
}

func TestLogoutReturnsTypedRemoteCleanupCorrelationAndLocalForgetStillCompletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/nodes/node-1" {
			http.NotFound(w, r)
			return
		}
		writeRecoveryPublicError(t, w, clientapiv2.ErrorCodeTemporarilyUnavailable, "remote-request-123")
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		LocalOwnerID: "uid:1000", ControlPlaneURLs: []string{server.URL}, Token: "session-token", ActiveAccountID: "account-1",
		IdentityPrivateKey: "retained-identity-key", PrivateKey: "retained-wireguard-key",
		NodeID: "node-1", NetworkID: "network-1", NodeCredential: "old-node-credential",
	}); err != nil {
		t.Fatal(err)
	}
	handler := agentIPCHandlers(agentIPCOptions{ConfigPath: configPath, WireGuard: &testAgentWireGuard{}})
	_, err := handler.Logout(context.Background(), ipc.LogoutRequest{})
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != ipc.ErrorRemoteCleanupRequired || ipcErr.RequestID != "remote-request-123" {
		t.Fatalf("logout error = %#v, want typed remote cleanup correlation", err)
	}
	preserved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.NodeCredential != "old-node-credential" || preserved.Token != "session-token" || preserved.LocalOwnerID != "uid:1000" {
		t.Fatal("failed remote logout changed local state before explicit local forget")
	}
	local, err := handler.LocalForget(context.Background(), ipc.LocalForgetRequest{Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if local.Outcome != ipc.LogoutOutcomeRemoteCleanupUnconfirmed {
		t.Fatalf("local forget outcome = %q", local.Outcome)
	}
	forgotten, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if forgotten.NodeCredential != "" || forgotten.Token != "" || forgotten.ActiveAccountID != "" {
		t.Fatal("local forget did not clear node/session state")
	}
	if forgotten.LocalOwnerID != "uid:1000" || forgotten.IdentityPrivateKey != "retained-identity-key" || forgotten.PrivateKey != "retained-wireguard-key" {
		t.Fatal("local forget violated owner/key retention matrix")
	}
}
