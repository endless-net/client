//go:build windows && integration

package clientapp

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
	"google.golang.org/protobuf/encoding/protojson"
)

// Consumer HTTPS fixture, not a Coordinator runtime or Management authority.
// It saves one logical request/result before losing the selected first reply.
type approvalRecoveryRemote struct {
	mu                         sync.Mutex
	store                      *state.Store
	profile, base, token, lost string
	expected                   []byte
	view                       coordinator.NodeEnrollmentRequest
	result                     *coordinator.RegisterNodeResponse
	replay                     bool
	calls                      map[string]int
	key                        ed25519.PrivateKey
	keyID                      string
}

func (f *approvalRecoveryRemote) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.store.Load(r.Context())
	p := s.Profiles[f.profile]
	if err != nil || p.Attempt == nil || !p.Attempt.BrowserApproval || !p.RemoteInFlight || p.InFlightAt.IsZero() || string(f.expected) != p.Secrets.RegistrationRequest || r.Header.Get("Cookie") != "" || r.Header.Get("X-EndlessNet-Node-Credential") != "" {
		http.Error(w, "invalid durable browser claim", http.StatusPreconditionFailed)
		return
	}
	q, err := coordinator.DecodeCreateNodeEnrollmentRequest(f.expected, 65536)
	if err != nil || coordinator.VerifyRegisterNodeIdentityProof(q) != nil {
		http.Error(w, "invalid original proof", http.StatusPreconditionFailed)
		return
	}
	operation := ""
	var response any
	createPath := "/nodes/enrollment-requests"
	if r.Method == http.MethodPost && r.URL.Path == createPath {
		operation = "create"
		raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
		if err != nil || !bytes.Equal(raw, f.expected) || r.Header.Get("Authorization") != "" {
			http.Error(w, "changed original Create", http.StatusPreconditionFailed)
			return
		}
		response = coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 1}
	} else {
		if r.Header.Get("Authorization") != "Bearer "+f.token || r.ContentLength != 0 {
			http.Error(w, "invalid original poll capability", http.StatusUnauthorized)
			return
		}
		path := createPath + "/" + f.view.ID
		switch {
		case r.Method == http.MethodGet && r.URL.Path == path:
			operation = "poll"
			if f.view.Status == "pending" {
				now := time.Now().UTC()
				f.view.Status = "approved"
				f.view.AccountID, f.view.NetworkID, f.view.NetworkName = "account", "network", "Resolved network"
				f.view.ApprovedByUserID, f.view.ApprovedAt = "fixture-owner", &now
			}
			response = coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}
		case r.Method == http.MethodPost && r.URL.Path == path+"/complete":
			operation = "complete"
			if f.result == nil {
				now := time.Now().UTC()
				claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: f.keyID, NetworkID: f.view.NetworkID, NodeID: "node", Scopes: []string{"node:register"}, ExpiresAt: now.Add(time.Hour)}
				payload, err := coordinator.NodeCredentialSigningPayload(f.keyID, claims.NetworkID, claims.NodeID, claims.Scopes, claims.ExpiresAt)
				if err != nil {
					http.Error(w, "invalid claim", http.StatusInternalServerError)
					return
				}
				claims.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, payload))
				raw, err := json.Marshal(claims)
				if err != nil {
					http.Error(w, "invalid claim", http.StatusInternalServerError)
					return
				}
				f.view.Status, f.view.NodeID, f.view.CompletedAt = "enrolled", "node", &now
				f.result = &coordinator.RegisterNodeResponse{SchemaVersion: 2, IdempotencyID: q.IdempotencyID, RegistrationBinding: coordinator.RegistrationIdentityProofBinding(q), Network: coordinator.Network{ID: f.view.NetworkID, AccountID: f.view.AccountID}, Node: coordinator.Node{ID: "node", NetworkID: f.view.NetworkID, UserID: "fixture-owner", Hostname: q.Hostname, IdentityPublicKey: q.IdentityPublicKey, PublicKey: q.PublicKey, DeviceFingerprint: q.DeviceFingerprint}, NodeCredential: "enc_" + base64.RawURLEncoding.EncodeToString(raw)}
			}
			response = coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: f.result}
		default:
			http.NotFound(w, r)
			return
		}
	}
	f.calls[operation]++
	if operation == f.lost && !f.replay {
		http.Error(w, "lost original reply fixture", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func prepareBrowserRecoveryState(t *testing.T, c Config, bundle []byte, f *approvalRecoveryRemote) state.Profile {
	t.Helper()
	prepareRecoveryState(t, c, bundle, false)
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(backend, protector, c.InstallationID, c.MaxStatePlainBytes, c.MaxStateCipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(budget(t), s.Revision, func(s *state.State) error {
		p := s.Profiles[c.DefaultProfileID]
		a, err := p.Attempt.LoginSucceeded(time.Now().UTC())
		if err != nil {
			return err
		}
		p.Attempt = &a
		p.Secrets.RegistrationID = "83e11eba-fd12-46ba-a977-9038c0f6a2f0"
		s.Profiles[p.ID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err = store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(budget(t), s.Revision, func(s *state.State) error {
		p, err := control.PrepareApprovalState(s.Profiles[c.DefaultProfileID], control.ApprovalTarget{NetworkName: "Requested network"}, c.ApprovalBase, time.Now().UTC())
		if err != nil {
			return err
		}
		q, err := coordinator.DecodeCreateNodeEnrollmentRequest([]byte(p.Secrets.RegistrationRequest), c.MaxRequestBytes)
		if err != nil {
			return err
		}
		f.expected = []byte(p.Secrets.RegistrationRequest)
		created := p.Attempt.ObservedAt
		f.view = coordinator.NodeEnrollmentRequest{ID: "ner_000000000000000000000003", Status: "pending", NetworkName: q.NetworkName, Hostname: q.Hostname, PublicKey: q.PublicKey, IdentityPublicKey: q.IdentityPublicKey, DeviceFingerprint: q.DeviceFingerprint, CreatedAt: created, ExpiresAt: created.Add(600 * time.Second), ApprovalURL: f.base + "?enrollment_request=ner_000000000000000000000003"}
		if f.lost == "complete" {
			f.view.Status, f.view.AccountID, f.view.NetworkID, f.view.NetworkName = "approved", "account", "network", "Resolved network"
			f.view.ApprovedByUserID, f.view.ApprovedAt = "fixture-owner", &created
			a, err := p.Attempt.BindBrowserContext("account", "network", created)
			if err != nil {
				return err
			}
			a, err = a.Approved(created)
			if err != nil {
				return err
			}
			p.Attempt = &a
			p.Approval.Snapshot = &approvalmodel.Snapshot{Request: f.view, PollToken: f.token, ObservedAt: created}
		}
		s.Profiles[p.ID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err = store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	return s.Profiles[c.DefaultProfileID]
}

func TestWindowsAssemblyRecoversOriginalBrowserCreateAndComplete(t *testing.T) {
	if testing.Short() {
		t.Skip("separate isolated Windows browser App recovery fixture")
	}
	for _, lost := range []string{"create", "complete"} {
		t.Run(lost, func(t *testing.T) {
			c := stateFixtureConfig(t)
			f := &approvalRecoveryRemote{profile: c.DefaultProfileID, base: c.ApprovalBase, token: "nep_" + strings.Repeat("T", 43), lost: lost, calls: map[string]int{}, key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))}
			bundle := func(seed byte) signing.BootstrapTrustBundle {
				public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32)).Public().(ed25519.PublicKey)
				hash := sha256.Sum256(public)
				id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
				return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(public)}}}
			}
			projection := signing.ServerKeyResponse{TrustBundle: bundle(1), NodeCredentialTrustBundle: bundle(7), RelayTrustBundle: bundle(3)}
			f.keyID = projection.NodeCredentialTrustBundle.ActiveKeyID
			raw, err := json.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/server-key", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			})
			mux.HandleFunc("/nodes/enrollment-requests", f.serve)
			mux.HandleFunc("/nodes/enrollment-requests/", f.serve)
			server := httptest.NewUnstartedServer(mux)
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			c.ControlOrigin = server.URL
			original := prepareBrowserRecoveryState(t, c, raw, f)
			var recovery time.Time
			for cycle := range 2 {
				build, digest := runtimeMetadata(t)
				r, err := NewWindows(c, build, digest, io.Discard)
				if err != nil {
					t.Fatal(err)
				}
				transport := r.http.Transport.(*observedHTTP).base.(*http.Transport)
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
				transport.TLSClientConfig.RootCAs.AddCert(server.Certificate())
				f.mu.Lock()
				f.store, f.replay = r.state.Store(), cycle == 1
				f.mu.Unlock()
				if err := r.App.Start(budget(t)); err != nil {
					t.Fatal(err)
				}
				func() {
					defer func() {
						if err := r.App.Stop(budget(t)); err != nil {
							t.Error(err)
						}
					}()
					limit := time.NewTimer(6 * time.Second)
					defer limit.Stop()
					tick := time.NewTicker(10 * time.Millisecond)
					defer tick.Stop()
					for {
						s, err := r.state.Store().Load(budget(t))
						if err != nil {
							t.Fatal(err)
						}
						p := s.Profiles[c.DefaultProfileID]
						if cycle == 0 && p.Attempt.Uncertain || cycle == 1 && p.Attempt.Phase == enrollment.Registered {
							if p.Attempt.Binding != original.Attempt.Binding || !reflect.DeepEqual(p.Keys, original.Keys) || p.Secrets.RegistrationRequest != original.Secrets.RegistrationRequest || !p.Attempt.ApprovalUntil.Equal(original.Attempt.ApprovalUntil) || !p.Attempt.Deadline.Equal(original.Attempt.Deadline) {
								t.Fatal("browser recovery replaced original identity/input/deadline")
							}
							if cycle == 0 {
								recovery = p.Attempt.RecoveryUntil
							} else if !p.Attempt.RecoveryUntil.Equal(recovery) || p.RemoteInFlight || p.Attempt.Uncertain || p.Secrets.NodeCredential == "" || p.Approval.Snapshot == nil || p.Approval.Snapshot.Request.Status != "enrolled" {
								t.Fatal("browser result was not durable or recovery was extended")
							}
							connection, err := localipc.Connect(budget(t), r.PipeConfig())
							if err != nil {
								t.Fatal(err)
							}
							defer func() { _ = connection.Close() }()
							status, err := clientv0.NewClientServiceClient(connection).GetStatus(budget(t), &clientv0.GetStatusRequest{})
							if err != nil {
								t.Fatal(err)
							}
							if status.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || status.Status.StoredState == nil || !status.Status.StoredState.DeviceFingerprintPresent || status.Status.StoredState.NodeCredentialPresent != (cycle == 1) {
								t.Fatal("IPC did not preserve disconnected/credential presence state")
							}
							if cycle == 1 && (status.Status.ControlState != clientv0.ControlState_CONTROL_STATE_REGISTERED || status.Status.NodeId != "node" || status.Status.AccountId != "account" || status.Status.Network == nil || status.Status.Network.Id != "network") {
								t.Fatal("IPC lost confirmed registration context")
							}
							public, err := protojson.Marshal(status)
							if err != nil || bytes.Contains(public, []byte(f.token)) || p.Secrets.NodeCredential != "" && bytes.Contains(public, []byte(p.Secrets.NodeCredential)) {
								t.Fatal("IPC exposed browser secret", err)
							}
							return
						}
						select {
						case <-limit.C:
							t.Fatal("browser App recovery did not finish")
						case <-tick.C:
						}
					}
				}()
				assertPipeClosed(t, r.PipeConfig())
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.result == nil {
				t.Fatal("fixture never committed original result")
			}
			if lost == "create" && (f.calls["create"] != 2 || f.calls["poll"] != 1 || f.calls["complete"] != 1) || lost == "complete" && (f.calls["create"] != 0 || f.calls["poll"] != 0 || f.calls["complete"] != 2) {
				t.Fatal("browser restart dispatched another operation", f.calls)
			}
		})
	}
}
