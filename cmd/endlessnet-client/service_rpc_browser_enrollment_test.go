package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNativeBrowserEnrollmentApprovalPersistenceAndCompletion(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "endlessnet.conf")
	mapKey := testMapSigningKey(t)
	mapSigningPublicKey := testMapSigningPublicKey(t, testNetworkMapWithRevision(t, mapKey, "net-pending", "node-pending", 1).MapSignature)
	approvalURL := "https://admin.example.test/admin/?enrollment_request=ner_test"
	pollToken := "nep_secret_poll"
	var gotReq clientapi.RegisterNodeRequest
	requestCalls := 0
	approved := false
	completeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			w.WriteHeader(http.StatusOK)
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, mapSigningPublicKey))
		case "/nodes/enrollment-requests":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			requestCalls++
			if auth := r.Header.Get("Authorization"); auth != "" {
				http.Error(w, "authorization header must be empty", http.StatusBadRequest)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(gotReq.JoinToken) != "" {
				http.Error(w, "join token must not be set", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(gotReq.IdempotencyID) == "" {
				http.Error(w, "idempotency key is required", http.StatusBadRequest)
				return
			}
			if err := clientapi.VerifyRegisterNodeIdentityProof(gotReq); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(clientapi.CreateNodeEnrollmentRequestResponse{
				Request: clientapi.NodeEnrollmentRequest{
					ID:          "ner_test",
					Status:      clientapi.NodeEnrollmentRequestPending,
					ApprovalURL: approvalURL,
				},
				PollToken:        pollToken,
				PollAfterSeconds: 1,
			})
		case "/nodes/enrollment-requests/ner_test":
			status := clientapi.NodeEnrollmentRequestPending
			if approved {
				status = clientapi.NodeEnrollmentRequestApproved
			}
			_ = json.NewEncoder(w).Encode(clientapi.NodeEnrollmentRequestStatusResponse{
				Request: clientapi.NodeEnrollmentRequest{ID: "ner_test", Status: status, ApprovalURL: approvalURL},
			})
		case "/nodes/enrollment-requests/ner_test/complete":
			if !approved {
				http.Error(w, "enrollment is not approved", http.StatusConflict)
				return
			}
			completeCalls++
			registration := clientapi.RegisterNodeResponse{
				Network: clientapi.Network{ID: "net-browser", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
				Node: clientapi.Node{
					ID:                "node-browser",
					NetworkID:         "net-browser",
					Hostname:          gotReq.Hostname,
					IdentityPublicKey: gotReq.IdentityPublicKey,
					PublicKey:         gotReq.PublicKey,
					DeviceFingerprint: gotReq.DeviceFingerprint,
					AssignedIP:        "100.64.0.2",
					ApprovalState:     clientapi.NodeApprovalApproved,
				},
				SchemaVersion:       clientapi.SchemaVersion,
				IdempotencyID:       gotReq.IdempotencyID,
				RegistrationBinding: clientapi.RegistrationIdentityProofBinding(gotReq),
			}
			credential, err := clientapi.SignNodeCredential(mapKey, registration.Network.ID, registration.Node.ID, []string{"node:register", "node:map", "node:endpoint", "node:delete"}, time.Now().UTC().Add(time.Hour))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			registration.NodeCredential = credential
			signature, err := clientapi.SignNetworkMap(mapKey, registration)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			registration.MapSignature = signature
			_ = json.NewEncoder(w).Encode(clientapi.CompleteNodeEnrollmentRequestResponse{
				Request:      clientapi.NodeEnrollmentRequest{ID: "ner_test", Status: clientapi.NodeEnrollmentRequestEnrolled, NodeID: registration.Node.ID},
				Registration: &registration,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	const operationID = "2f2296cc-40f2-42dc-8f37-b046c43e8011"
	input := client.ClientRPCEnrollmentInput{OperationID: operationID, Browser: true, Hostname: "interactive-native", Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_SERVER}
	save := func(cfg client.Config) error { return client.SaveConfig(configPath, cfg) }
	action, err := agentRPCEnroll(t.Context(), client.Config{ControlPlaneURLs: []string{server.URL}}, input, save)
	if err != nil || action.GetKind() != ipc.UserAction_KIND_OPEN_BROWSER || action.GetBrowserUrl() != approvalURL {
		t.Fatal("native browser enrollment lost its approval action")
	}
	wire, err := protojson.Marshal(action)
	if err != nil || strings.Contains(string(wire), pollToken) {
		t.Fatal("native approval action disclosed poll token")
	}
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnrollmentRequestID != "ner_test" || cfg.EnrollmentPollToken != pollToken || cfg.ApprovalURL != approvalURL ||
		cfg.EnrollmentRequest == nil || clientapi.RegistrationIdentityProofBinding(*cfg.EnrollmentRequest) != clientapi.RegistrationIdentityProofBinding(gotReq) {
		t.Fatal("native browser enrollment did not persist the original polling authority")
	}
	if cfg.NodeID != "" || cfg.NodeCredential != "" || cfg.CachedMap != nil || completeCalls != 0 ||
		requestCalls != 1 || gotReq.Hostname != input.Hostname || gotReq.IdempotencyID != operationID || !containsString(gotReq.Tags, "mode:server") {
		t.Fatal("pending browser enrollment adopted authority or changed the request")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatal("pending browser enrollment rendered a tunnel configuration")
	}
	approved = true
	// Resuming must use the saved request rather than a newly supplied hostname.
	input.Hostname = "must-not-replace-saved-hostname"
	action, err = agentRPCEnroll(t.Context(), cfg, input, save)
	if err != nil || action != nil {
		t.Fatal("approved native browser enrollment did not finish")
	}
	cfg, err = client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnrollmentRequest != nil || cfg.EnrollmentRequestID != "" || cfg.EnrollmentPollToken != "" ||
		cfg.NodeID != "node-browser" || cfg.CachedMap == nil || cfg.CachedMap.Node.Hostname != "interactive-native" || cfg.NodeCredential == "" ||
		requestCalls != 1 || completeCalls != 1 {
		t.Fatal("browser completion lost identity, repeated request creation or retained polling authority")
	}
	if cfg.NodeCredentialSigningTrust == nil {
		t.Fatal("verified credential trust was not persisted by enrollment")
	}
	engine := &testAgentWireGuard{}
	wake := make(chan struct{}, 1)
	if err := agentRPCProfileDriver(agentIPCOptions{WireGuard: engine, SyncWake: wake}).Start(t.Context(), cfg); err != nil {
		t.Fatal("separate native Connect failed after approval", err)
	}
	if engine.configureCalls != 1 || len(wake) != 1 {
		t.Fatal("native Connect did not apply exactly once")
	}
}
