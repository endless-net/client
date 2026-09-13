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
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	native "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/proto"
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
	idempotencyID, err := clientapi.NewRegistrationIdempotencyID()
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

func writeRecoveryPublicError(t *testing.T, w http.ResponseWriter, code clientapi.ErrorCode, requestID string) {
	t.Helper()
	value, err := clientapi.NewPublicError(code, "safe recovery diagnostic", requestID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := clientapi.MarshalPublicError(value)
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
		code      clientapi.ErrorCode
		terminal  bool
		phase     client.RecoveryPhase
		retryable bool
	}{
		{"unknown", clientapi.ErrorCodeNodeCredentialUnknown, true, "", false},
		{"revoked", clientapi.ErrorCodeNodeCredentialRevoked, true, "", false},
		{"expired", clientapi.ErrorCodeNodeCredentialExpired, true, "", false},
		{"invalid", clientapi.ErrorCodeNodeCredentialInvalid, false, client.RecoveryPhaseBlocked, false},
		{"binding", clientapi.ErrorCodeNodeIdentityBindingMismatch, false, client.RecoveryPhaseBlocked, false},
		{"renewal-required", clientapi.ErrorCodeNodeCredentialRenewalRequired, false, client.RecoveryPhaseBlocked, false},
		{"authentication", clientapi.ErrorCodeAuthenticationRequired, false, client.RecoveryPhaseNeedsLogin, false},
		{"authorization", clientapi.ErrorCodeAuthorizationDenied, false, client.RecoveryPhasePolicyBlocked, false},
		{"transient", clientapi.ErrorCodeTemporarilyUnavailable, false, client.RecoveryPhaseRecovering, true},
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
	var firstRequest clientapi.RegisterNodeRequest
	var durableResponse clientapi.RegisterNodeResponse
	registerCalls := 0
	var fixture recoveryTestFixture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nodes/register":
			req, err := clientapi.DecodeRegisterNodeRequest(r.Body)
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
	if after.NodeCredentialSigningTrust == nil {
		t.Fatal("renewed credential trust was not persisted")
	}
}

func TestNativeRecoveryProviderReusesDurableRenewalIdentity(t *testing.T) {
	var fixture recoveryTestFixture
	var mu sync.Mutex
	var requestIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/nodes/register" {
			t.Error("unexpected native recovery request")
			http.NotFound(w, r)
			return
		}
		req, err := clientapi.DecodeRegisterNodeRequest(r.Body)
		if err != nil {
			t.Error("invalid native renewal request")
			return
		}
		persisted, err := client.LoadConfig(fixture.ConfigPath)
		if err != nil {
			t.Error("cannot load persisted recovery")
			return
		}
		if persisted.MapSigningTrust == nil || persisted.MapSigningTrust.ActiveKeyID != fixture.NewTrust.ActiveKeyID ||
			persisted.EnrollmentRecovery == nil || persisted.EnrollmentRecovery.IdempotencyID != req.IdempotencyID {
			t.Error("renewal did not use durable confirmed trust and request identity")
		}
		mu.Lock()
		requestIDs = append(requestIDs, req.IdempotencyID)
		mu.Unlock()
		writeRecoveryPublicError(t, w, clientapi.ErrorCodeTemporarilyUnavailable, "request-unavailable")
	}))
	defer server.Close()
	fixture = newRecoveryTestFixture(t, server.URL)
	before, err := client.LoadConfig(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		// Reopen persisted input for each attempt, as a replacement worker does.
		store, err := client.OpenConfigStore(fixture.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		result, err := agentRPCTrustRecovery(t.Context(), store.Read())
		if err != nil || result.Configuration != nil || result.RequiresEnrollment ||
			result.Phase != client.RecoveryPhaseRecovering || result.Failure == nil ||
			result.Failure.Code != native.ErrorCode_ERROR_CODE_UNAVAILABLE || !result.Failure.Retryable ||
			result.Failure.ControlRequestId != "request-unavailable" {
			t.Fatal("native retryable recovery result lost failure classification")
		}
		after, err := client.LoadConfig(fixture.ConfigPath)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("failed provider changed durable enrollment or recovery identity")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requestIDs) != 2 || requestIDs[0] != before.EnrollmentRecovery.IdempotencyID || requestIDs[1] != requestIDs[0] {
		t.Fatal("native renewal changed its durable idempotency identity")
	}
}

func recoverySuccessResponse(t *testing.T, signingKey ed25519.PrivateKey, req clientapi.RegisterNodeRequest) clientapi.RegisterNodeResponse {
	t.Helper()
	credential, err := clientapi.SignNodeCredential(signingKey, req.NetworkID, "node-1", []string{"node:register", "node:map"}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	response := clientapi.RegisterNodeResponse{
		SchemaVersion: clientapi.SchemaVersion, IdempotencyID: req.IdempotencyID,
		Revision: clientapi.MapRevision{Network: 8},
		Network:  clientapi.Network{ID: req.NetworkID, Name: "default", CIDR: "100.64.0.0/24", Revision: 8},
		Node: clientapi.Node{
			ID: "node-1", NetworkID: req.NetworkID, Hostname: req.Hostname,
			IdentityPublicKey: req.IdentityPublicKey, PublicKey: req.PublicKey, DeviceFingerprint: req.DeviceFingerprint,
			AssignedIP: "100.64.0.2", ApprovalState: clientapi.NodeApprovalApproved,
			Endpoint: req.Endpoint, EndpointGeneration: req.EndpointGeneration,
			EndpointCandidates: append([]string(nil), req.EndpointCandidates...), AdvertisedIPs: append([]string(nil), req.AdvertisedIPs...), RequestedTags: append([]string(nil), req.Tags...),
		},
		RegistrationBinding: clientapi.RegistrationIdentityProofBinding(req),
		NodeCredential:      credential,
	}
	v1Response := response
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
		state   native.ServiceState
		control native.ControlState
		code    native.ErrorCode
	}{
		{client.RecoveryPhaseRecovering, native.ServiceState_SERVICE_STATE_RECOVERING, native.ControlState_CONTROL_STATE_RECOVERING, native.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{client.RecoveryPhaseBlocked, native.ServiceState_SERVICE_STATE_RECOVERY_BLOCKED, native.ControlState_CONTROL_STATE_RECOVERY_BLOCKED, native.ErrorCode_ERROR_CODE_UNAVAILABLE},
		{client.RecoveryPhasePolicyBlocked, native.ServiceState_SERVICE_STATE_POLICY_BLOCKED, native.ControlState_CONTROL_STATE_POLICY_BLOCKED, native.ErrorCode_ERROR_CODE_POLICY_BLOCKED},
		{client.RecoveryPhaseNeedsLogin, native.ServiceState_SERVICE_STATE_NEEDS_LOGIN, native.ControlState_CONTROL_STATE_NEEDS_LOGIN, native.ErrorCode_ERROR_CODE_NEEDS_LOGIN},
	}
	for _, tc := range tests {
		t.Run(string(tc.phase), func(t *testing.T) {
			for _, retryable := range []bool{false, true} {
				recovery, err := client.NewEnrollmentRecovery("operation-1", "registration-id-1", "https://control.example.test", "key-2", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				recovery = recovery.WithFailure(tc.phase, "synthetic-internal-code", "control-request-1", retryable, time.Now())
				cfg := client.Config{NodeID: "node-1", EnrollmentRecovery: &recovery,
					ConnectionIntent: &client.ConnectionIntent{DesiredState: client.ConnectionIntentDesiredDisconnected}}
				status := buildAgentRPCStatus(t.Context(), agentIPCOptions{}, cfg, native.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
				// Exercise the public protobuf representation, not a retired DTO.
				raw, err := proto.Marshal(status)
				if err != nil {
					t.Fatal(err)
				}
				decoded := new(native.Status)
				if err := proto.Unmarshal(raw, decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.ServiceState != tc.state || decoded.ControlState != tc.control ||
					decoded.GetRecovery().GetState() != tc.state || decoded.GetRecovery().GetOperationId() != recovery.OperationID {
					t.Fatal("native status lost durable recovery state or operation correlation")
				}
				failure := decoded.GetRecovery().GetFailure()
				if failure == nil || failure.Code != tc.code || failure.ControlRequestId != recovery.RequestID || failure.Retryable != retryable {
					t.Fatal("native recovery failure lost typed classification or request correlation")
				}
				if decoded.ConnectionPhase != native.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED ||
					!decoded.UserDisconnected || decoded.GetIntent().GetDesiredState() != native.DesiredState_DESIRED_STATE_DISCONNECTED {
					t.Fatal("recovery state changed actual phase or disconnected intent")
				}
			}
		})
	}
}

func TestDurableRecoveryStateOverridesStaleAgentSigningError(t *testing.T) {
	recovery, err := client.NewEnrollmentRecovery("operation-1", "registration-id-1", "https://control.example.test", "key-2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{NodeID: "node-1", NetworkID: "network-1", NodeCredential: "credential", EnrollmentRecovery: &recovery,
		ControlPlaneURLs: []string{"https://control.example.test"}}
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := client.OpenConfigStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := client.NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := mutations.AdoptInitialProfile(); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeAgentFailureSnapshot(statePath, configPath, errors.New(serverMapSigningTrustChangedError)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.LoadAgentSnapshot(statePath)
	if err != nil || snapshot.LastError != serverMapSigningTrustChangedError || snapshot.ProfileID == "" {
		t.Fatal("stale signing failure fixture is missing its profile binding")
	}
	status := buildAgentRPCStatus(t.Context(), agentIPCOptions{ConfigPath: configPath, ConfigStore: store, StateOutput: statePath},
		store.Read(), native.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED)
	if status.ServiceState != native.ServiceState_SERVICE_STATE_RECOVERING ||
		status.ControlState != native.ControlState_CONTROL_STATE_RECOVERING ||
		status.GetRecovery().GetOperationId() != recovery.OperationID || status.GetAgent().GetLastFailure() != nil {
		t.Fatal("stale agent failure replaced durable native recovery state")
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
