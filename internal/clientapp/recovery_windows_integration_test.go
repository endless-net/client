//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
	signing "github.com/endless-net/signing/api/contract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This remote fixture records one logical effect, then loses its first reply.
// Real HTTPS/SDK, the factory's observed transport, DPAPI and pipe are retained.
type recoveryRemote struct {
	identityapiconnect.UnimplementedNativeLoginServiceHandler
	mu       sync.Mutex
	store    *state.Store
	profile  string
	original []byte
	calls    int
	replay   bool
	key      ed25519.PrivateKey
	keyID    string
}

func (f *recoveryRemote) sent(ctx context.Context, raw []byte) (state.Profile, error) {
	s, err := f.store.Load(ctx)
	p := s.Profiles[f.profile]
	if err != nil || !p.RemoteInFlight || p.InFlightAt.IsZero() {
		return p, errors.New("missing durable send marker")
	}
	if f.original == nil {
		f.original = append([]byte(nil), raw...)
	} else if !bytes.Equal(f.original, raw) {
		return p, errors.New("replay changed original request")
	}
	f.calls++
	return p, nil
}

func (f *recoveryRemote) StartNativeLogin(ctx context.Context, q *connect.Request[identityapi.StartNativeLoginRequest]) (*connect.Response[identityapi.StartNativeLoginResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(q.Msg)
	if err != nil {
		return nil, err
	}
	p, err := f.sent(ctx, raw)
	if err != nil || q.Header().Get("Authorization") != "Bearer "+p.Secrets.RequestProof {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("binding rejected"))
	}
	if !f.replay {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("lost reply fixture"))
	}
	created := p.Attempt.CreatedAt
	tx := &identityapi.NativeLoginTransaction{TransactionId: "ab8851d7-45e8-43bc-9b9a-a67ef3dd40fe", RequestId: q.Msg.RequestId, AttemptBindingSha256: q.Msg.AttemptBindingSha256, State: identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED, CreatedAt: timestamppb.New(created), ExpiresAt: timestamppb.New(created.Add(600 * time.Second)), ReplayExpiresAt: timestamppb.New(created.Add(1320 * time.Second))}
	tx.Outcome = &identityapi.NativeLoginTransaction_Result{Result: &identityapi.NativeLoginResult{Session: &sessionv1.UserSession{SessionId: "native-session", UserId: "native-user", State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true, ExpiresAt: timestamppb.New(created.Add(time.Hour))}, AccessBearer: strings.Repeat("a", 43), RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: strings.Repeat("n", 43), ExpiresAt: timestamppb.New(created.Add(24 * time.Hour))}}}
	return connect.NewResponse(&identityapi.StartNativeLoginResponse{Transaction: tx, PollAuthorization: strings.Repeat("p", 43)}), nil
}

func (f *recoveryRemote) register(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	p, claimErr := f.sent(r.Context(), raw)
	if err != nil || claimErr != nil || r.Header.Get("Authorization") != "Bearer "+p.Secrets.Access || string(raw) != p.Secrets.RegistrationRequest {
		http.Error(w, "binding rejected", http.StatusPreconditionFailed)
		return
	}
	if !f.replay {
		http.Error(w, "lost reply fixture", http.StatusServiceUnavailable)
		return
	}
	q, err := coordinator.DecodeRegisterNodeRequest(raw, 65536)
	if err != nil {
		http.Error(w, "request rejected", http.StatusBadRequest)
		return
	}
	expires := p.Attempt.CreatedAt.Add(time.Hour)
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: f.keyID, NetworkID: q.NetworkID, NodeID: "node", Scopes: []string{"node:register"}, ExpiresAt: expires}
	payload, err := coordinator.NodeCredentialSigningPayload(f.keyID, claims.NetworkID, claims.NodeID, claims.Scopes, expires)
	if err != nil {
		http.Error(w, "claim rejected", http.StatusInternalServerError)
		return
	}
	claims.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, payload))
	encoded, err := json.Marshal(claims)
	if err != nil {
		http.Error(w, "claim rejected", http.StatusInternalServerError)
		return
	}
	result := coordinator.RegisterNodeResponse{SchemaVersion: 2, IdempotencyID: q.IdempotencyID, RegistrationBinding: coordinator.RegistrationIdentityProofBinding(q), Network: coordinator.Network{ID: q.NetworkID, AccountID: q.AccountID}, Node: coordinator.Node{ID: "node", NetworkID: q.NetworkID, IdentityPublicKey: q.IdentityPublicKey, PublicKey: q.PublicKey, DeviceFingerprint: q.DeviceFingerprint}, NodeCredential: "enc_" + base64.RawURLEncoding.EncodeToString(encoded)}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func prepareRecoveryState(t *testing.T, c Config, bundle []byte, registration bool) state.Profile {
	t.Helper()
	backend, err := state.OpenWindows(c.StateDirectory, c.ServiceSID, c.StateLockRetry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	}()
	protector, err := state.NewDPAPI(c.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.New(backend, protector, c.InstallationID, c.MaxStatePlainBytes, c.MaxStateCipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(budget(t)); err != nil {
		t.Fatal(err)
	}
	s, err := store.Load(budget(t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := state.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := keys.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p := state.Profile{ID: c.DefaultProfileID, DisplayName: "Fixture", ControlOrigin: c.ControlOrigin, Keys: keys, Hostname: "device", Secrets: state.RemoteSecrets{RequestProof: strings.Repeat("r", 43)}}
	now := time.Now().UTC().Add(-time.Second)
	a, err := enrollment.New(enrollment.Binding{AttemptID: "487fb261-c50e-4fad-8e3a-47622509959b", RequestID: "b8c8f430-ce98-444d-84c5-61dcd346b4b5", ProfileID: p.ID, Owner: c.ServiceSID, ControlOrigin: c.ControlOrigin, DeviceKeyHash: fingerprint}, now)
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := control.DecodeAnnouncement(c.ControlOrigin, bundle, now)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
	p.Hostname = "device"
	if registration {
		a, err = a.LoginSucceeded(now)
		if err != nil {
			t.Fatal(err)
		}
		a, err = a.Select("account", "network", now)
		if err != nil {
			t.Fatal(err)
		}
		p.Attempt = &a
		p.Secrets.Access = strings.Repeat("a", 43)
		p.Secrets.SessionExpiresAt = now.Add(time.Hour)
		p.Secrets.RegistrationID = "83e11eba-fd12-46ba-a977-9038c0f6a2f0"
	}
	if err := store.Update(budget(t), s.Revision, func(s *state.State) error {
		s.OwnerSID = c.ServiceSID
		s.ActiveProfileID = p.ID
		s.Profiles[p.ID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWindowsAssemblyRecoversOriginalLoginAndRegistrationAfterLostReply(t *testing.T) {
	if testing.Short() {
		t.Skip("separate Windows full application recovery fixture")
	}
	for _, registration := range []bool{false, true} {
		name := "login"
		if registration {
			name = "registration"
		}
		t.Run(name, func(t *testing.T) {
			c := stateFixtureConfig(t)
			remote := &recoveryRemote{profile: c.DefaultProfileID, key: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))}
			bundle := func(seed byte) signing.BootstrapTrustBundle {
				key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32)).Public().(ed25519.PublicKey)
				hash := sha256.Sum256(key)
				id := "ed25519:" + base64.RawURLEncoding.EncodeToString(hash[:])
				return signing.BootstrapTrustBundle{Version: 1, ActiveKeyID: id, Keys: []signing.BootstrapTrustKey{{KeyID: id, Algorithm: "ed25519", PublicKey: base64.RawURLEncoding.EncodeToString(key)}}}
			}
			projection := signing.ServerKeyResponse{TrustBundle: bundle(1), NodeCredentialTrustBundle: bundle(7), RelayTrustBundle: bundle(3)}
			remote.keyID = projection.NodeCredentialTrustBundle.ActiveKeyID
			raw, err := json.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/server-key", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			})
			path, handler := identityapiconnect.NewNativeLoginServiceHandler(remote)
			mux.Handle(path, handler)
			mux.HandleFunc("/nodes/register", remote.register)
			server := httptest.NewUnstartedServer(mux)
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			c.ControlOrigin = server.URL
			original := prepareRecoveryState(t, c, raw, registration)
			var recovery time.Time
			var request string
			for cycle := range 2 {
				build, digest := runtimeMetadata(t)
				r, err := NewWindows(c, build, digest, io.Discard)
				if err != nil {
					t.Fatal(err)
				}
				// Only the fixture CA is injected. TLS verification, observed transport,
				// SDK, deadlines and the actual application composition remain enabled.
				transport := r.http.Transport.(*observedHTTP).base.(*http.Transport)
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
				transport.TLSClientConfig.RootCAs.AddCert(server.Certificate())
				remote.mu.Lock()
				remote.store = r.state.Store()
				remote.replay = cycle == 1
				remote.mu.Unlock()
				if err := r.App.Start(budget(t)); err != nil {
					t.Fatal(err)
				}
				func() {
					defer func() {
						if err := r.App.Stop(budget(t)); err != nil {
							t.Error(err)
						}
					}()
					deadline := time.NewTimer(5 * time.Second)
					defer deadline.Stop()
					tick := time.NewTicker(10 * time.Millisecond)
					defer tick.Stop()
					for {
						s, err := r.state.Store().Load(budget(t))
						if err != nil {
							t.Fatal(err)
						}
						p := s.Profiles[c.DefaultProfileID]
						finished := p.Attempt.Phase == enrollment.ChoosingContext
						if registration {
							finished = p.Attempt.Phase == enrollment.Registered
						}
						if cycle == 0 && p.Attempt.Uncertain || cycle == 1 && finished {
							if p.Attempt.Binding != original.Attempt.Binding || !reflect.DeepEqual(p.Keys, original.Keys) || !p.Attempt.Deadline.Equal(original.Attempt.Deadline) {
								t.Fatal("recovery changed original identity or budget")
							}
							if cycle == 0 {
								recovery = p.Attempt.RecoveryUntil
								request = p.Secrets.RegistrationRequest
							} else {
								if !p.Attempt.RecoveryUntil.Equal(recovery) || p.RemoteInFlight || p.Attempt.Uncertain || p.Secrets.RegistrationRequest != request {
									t.Fatal("recovery extended budget or changed request")
								}
								if registration && p.Secrets.NodeCredential == "" || !registration && p.Secrets.Access == "" {
									t.Fatal("verified result not durable")
								}
							}
							connection, err := localipc.Connect(budget(t), r.PipeConfig())
							if err != nil {
								t.Fatal(err)
							}
							defer func() { _ = connection.Close() }()
							if _, err := clientv0.NewClientServiceClient(connection).GetStatus(budget(t), &clientv0.GetStatusRequest{}); err != nil {
								t.Fatal(err)
							}
							return
						}
						select {
						case <-deadline.C:
							t.Fatal("recovery did not converge within fixture bound")
						case <-tick.C:
						}
					}
				}()
				assertPipeClosed(t, r.PipeConfig())
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.calls != 2 {
				t.Fatal("unexpected sends across restart", remote.calls)
			}
		})
	}
}
