package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"github.com/endless-net/identity/api/session/v1/sessionv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sessionRotationFixture struct {
	sessionv1connect.UnimplementedSessionServiceHandler
	t       *testing.T
	f       *registrationFixture
	mode    string
	calls   int
	request *sessionv1.RenewSessionRequest
}

func (s *sessionRotationFixture) RenewSession(ctx context.Context, r *connect.Request[sessionv1.RenewSessionRequest]) (*connect.Response[sessionv1.RenewSessionResponse], error) {
	s.t.Helper()
	s.calls++
	current, err := s.f.store.Load(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	p := current.Profiles[loginProfile]
	if p.SessionRefresh == nil || p.SessionRefresh.SentAt.IsZero() || p.SessionRefresh.RequestID != r.Msg.RequestId || r.Header().Get("Authorization") != "Bearer "+s.f.p.Secrets.Renewal {
		s.t.Fatal("session RPC before frozen send marker")
	}
	if s.request == nil {
		s.request = proto.Clone(r.Msg).(*sessionv1.RenewSessionRequest)
	} else if !proto.Equal(s.request, r.Msg) {
		s.t.Fatal("session retry changed input")
	}
	if s.mode == "lost-session" && s.calls == 1 {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("secret lost session result"))
	}
	user := "native-user"
	if s.mode == "foreign-session" {
		user = "foreign-user"
	}
	op := &sessionv1.SessionRenewal{OperationId: "renewal-operation", RequestId: r.Msg.RequestId, PreviousSessionId: r.Msg.ExpectedSessionId, State: sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, ReplayExpiresAt: timestamppb.New(s.f.now.Add(12 * time.Minute)), Outcome: &sessionv1.SessionRenewal_Result{Result: &sessionv1.SessionRenewalResult{Session: &sessionv1.UserSession{SessionId: "rotated-session", UserId: user, State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(s.f.now.Add(time.Hour)), RenewalSupported: true}, AccessBearer: strings.Repeat("n", 43), RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: strings.Repeat("g", 43), ExpiresAt: timestamppb.New(s.f.now.Add(24 * time.Hour))}}}}
	if s.mode == "lost-session-save" {
		s.f.memory.lose = true
	}
	return connect.NewResponse(&sessionv1.RenewSessionResponse{Operation: op, PollAuthorization: strings.Repeat("q", 43)}), nil
}

func TestCredentialsRefreshUsesOriginalDeviceAndRecoversUnknownResults(t *testing.T) {
	for _, mode := range []string{"valid", "lost-session", "lost-node", "lost-session-save", "lost-node-save", "foreign-session", "foreign-node", "signature"} {
		t.Run(mode, func(t *testing.T) {
			f := newRegistrationFixture(t)
			f.sign(t, f.announcementNodeKey(t), "node:register", f.now.Add(-time.Minute))
			f.p.Attempt.Phase = enrollment.Registered
			f.p.Attempt.NodeID = "node"
			f.p.Attempt.CredentialEnds = f.now.Add(-time.Minute)
			f.p.RemoteInFlight = false
			f.p.InFlightAt = time.Time{}
			f.p.Secrets.RegistrationRequest = string(f.original)
			f.p.Secrets.NodeCredential = f.result.NodeCredential
			f.p.Secrets.UserID = "native-user"
			f.p.Secrets.SessionID = "previous-session"
			f.p.Secrets.Renewal = strings.Repeat("z", 43)
			f.p.Secrets.SessionExpiresAt = f.now.Add(-time.Minute)
			f.p.Secrets.RenewalExpiresAt = f.now.Add(24 * time.Hour)
			f.p.Connection = &state.ConnectionState{}
			if _, err := f.p.Connection.AcceptIntent(state.IntentConnected, f.now); err != nil {
				t.Fatal(err)
			}
			initial, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.store.Update(context.Background(), initial.Revision, func(s *state.State) error { s.Profiles[loginProfile] = f.p; return nil }); err != nil {
				t.Fatal("registered fixture", err)
			}
			baseline, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			actor := &sessionRotationFixture{t: t, f: &f, mode: mode}
			_, sessionHandler := sessionv1connect.NewSessionServiceHandler(actor)
			nodeCalls := 0
			var originalNode []byte
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/nodes/register" {
					w := httptest.NewRecorder()
					sessionHandler.ServeHTTP(w, r)
					response := w.Result()
					response.Request = r
					return response, nil
				}
				nodeCalls++
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := f.store.Load(r.Context())
				if err != nil {
					t.Fatal(err)
				}
				pending := stored.Profiles[loginProfile].NodeRenewal
				if pending == nil || pending.SentAt.IsZero() || pending.Request != string(raw) || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("n", 43) || r.Header.Get("Cookie") != "" {
					t.Fatal("node RPC before frozen marker/current session")
				}
				if originalNode == nil {
					originalNode = bytes.Clone(raw)
				} else if !bytes.Equal(raw, originalNode) {
					t.Fatal("node retry changed original bytes")
				}
				if mode == "lost-node" && nodeCalls == 1 {
					return nil, errors.New("secret lost node result")
				}
				request, err := coordinator.DecodeRegisterNodeRequest(raw, 65536)
				if err != nil {
					t.Fatal(err)
				}
				if request.IdempotencyID == f.p.Secrets.RegistrationID || request.NodeCredential != f.p.Secrets.NodeCredential || request.RegistrationBinding != f.result.RegistrationBinding {
					t.Fatal("renewal changed registration/predecessor")
				}
				f.sign(t, f.announcementNodeKey(t), "node:register", f.now.Add(time.Hour))
				response := coordinator.NodeCredentialRenewalResponse{SchemaVersion: 1, IdempotencyID: request.IdempotencyID, RenewalBinding: coordinator.RegistrationIdentityProofBinding(request), RegistrationBinding: request.RegistrationBinding, PreviousCredentialSHA256: coordinator.NodeCredentialSHA256(request.NodeCredential), NetworkID: "network", NodeID: "node", NodeCredential: f.result.NodeCredential, IssuedAt: f.now, ExpiresAt: f.now.Add(time.Hour)}
				if mode == "foreign-node" {
					response.NodeID = "foreign"
				}
				if mode == "signature" {
					response.NodeCredential = strings.Replace(response.NodeCredential, "enc_", "enc_A", 1)
				}
				if mode == "lost-node-save" {
					f.memory.lose = true
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(encode(t, response))), Request: r}, nil
			})}
			makeExecutor := func() *Credentials {
				e, err := NewCredentials(f.store, client, staticAnnouncement{f.announcement}, RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: func() time.Time { return f.now }}, RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
				if err != nil {
					t.Fatal(err)
				}
				return e
			}
			err = makeExecutor().Ensure(context.Background(), loginProfile, 1)
			if strings.HasPrefix(mode, "lost-") {
				if err == nil {
					t.Fatal("unknown result reported success")
				}
				err = makeExecutor().Ensure(context.Background(), loginProfile, 1)
			}
			if mode == "foreign-session" || mode == "foreign-node" || mode == "signature" {
				if !errors.Is(err, ErrCredentialsProtocol) {
					t.Fatal("foreign/unverified authority accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal("renewal failed", err)
			}
			current, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			before, after := baseline.Profiles[loginProfile], current.Profiles[loginProfile]
			if !reflect.DeepEqual(before.Attempt, after.Attempt) || !reflect.DeepEqual(before.Keys, after.Keys) || before.Secrets != after.Secrets || after.ActiveSession == nil || after.ActiveNodeCredential == nil {
				t.Fatal("renewal changed immutable enrollment state")
			}
			wantSessions, wantNodes := 1, 1
			if mode == "lost-session" {
				wantSessions = 2
			}
			if mode == "lost-node" {
				wantNodes = 2
			}
			if actor.calls != wantSessions || nodeCalls != wantNodes {
				t.Fatal("hidden remote retry/duplicate", actor.calls, nodeCalls)
			}
			if err = makeExecutor().Ensure(context.Background(), loginProfile, 1); err != nil || actor.calls != wantSessions || nodeCalls != wantNodes {
				t.Fatal("healthy credential caused another renewal", err)
			}
			if mode == "valid" {
				assertRenewedEndpointReceipt(t, f.store, after, f.now)
			}
		})
	}
}

func (f *registrationFixture) announcementNodeKey(t *testing.T) string {
	t.Helper()
	var trust struct {
		NodeCredentialTrustBundle struct {
			ActiveKeyID string `json:"active_key_id"`
		} `json:"node_credential_trust_bundle"`
	}
	if json.Unmarshal(f.announcement.Bundle, &trust) != nil || trust.NodeCredentialTrustBundle.ActiveKeyID == "" {
		t.Fatal("fixture trust missing")
	}
	return trust.NodeCredentialTrustBundle.ActiveKeyID
}

func assertRenewedEndpointReceipt(t *testing.T, store *state.Store, original state.Profile, now time.Time) {
	t.Helper()
	change := func(fn func(*state.Profile)) error {
		current, e := store.Load(t.Context())
		if e != nil {
			return e
		}
		return store.Update(t.Context(), current.Revision, func(s *state.State) error {
			p := s.Profiles[loginProfile]
			fn(&p)
			s.Profiles[loginProfile] = p
			return nil
		})
	}
	request := coordinator.UpdateNodeEndpointRequest{Endpoint: "192.0.2.1:51820", Generation: 1, TTL: "30s", Status: "online"}
	raw, e := json.Marshal(request)
	if e != nil {
		t.Fatal(e)
	}
	if e = change(func(p *state.Profile) {
		_, _ = p.Connection.NextEndpointGeneration()
		p.Connection.EndpointRequest = &state.EndpointRequest{IntentGeneration: p.Connection.Generation, Request: raw}
	}); e != nil {
		t.Fatal(e)
	}
	if e = change(func(p *state.Profile) { p.Connection.EndpointRequest.Sent = true }); e != nil {
		t.Fatal(e)
	}
	ack := coordinator.UpdateNodeEndpointResponse{SchemaVersion: 2, NodeID: original.Attempt.NodeID, NetworkID: original.Attempt.NetworkID, Endpoint: request.Endpoint, Generation: 1, Online: true, UpdatedAt: now, ExpiresAt: now.Add(30 * time.Second), Revision: coordinator.MapRevision{Network: 2}}
	if !ack.ExpiresAt.After(original.Attempt.CredentialEnds) {
		t.Fatal("fixture does not exercise renewed lifetime")
	}
	if e = change(func(p *state.Profile) { p.Connection.EndpointRequest.Receipt = &ack }); e != nil {
		t.Fatal("valid current credential endpoint ACK rejected", e)
	}
	current, e := store.Load(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	p := current.Profiles[loginProfile]
	if !reflect.DeepEqual(p.Attempt, original.Attempt) || p.Secrets != original.Secrets || !reflect.DeepEqual(p.ActiveNodeCredential, original.ActiveNodeCredential) || !reflect.DeepEqual(p.Connection.EndpointRequest.Receipt, &ack) {
		t.Fatal("endpoint adoption rewrote original history or current authority")
	}
}
