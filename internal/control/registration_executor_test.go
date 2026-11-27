package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
)

func (f *registrationFixture) executor(t *testing.T, trip transport) *RegistrationExecutor {
	t.Helper()
	s, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := f.p
	p.RemoteInFlight = false
	p.InFlightAt = time.Time{}
	if err = f.store.Update(context.Background(), s.Revision, func(s *state.State) error { s.Profiles[p.ID] = p; return nil }); err != nil {
		t.Fatal(err)
	}
	e, err := NewRegistrationExecutor(f.store, f.adapter(t, trip), RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f *registrationFixture) persisted(t *testing.T) state.Profile {
	t.Helper()
	s, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s.Profiles[f.p.ID]
}

func (f *registrationFixture) response(req *http.Request, t *testing.T) *http.Response {
	t.Helper()
	p := f.persisted(t)
	message := &wire.RegisterNodeRequest{}
	readRPC(t, req, message)
	if !p.RemoteInFlight || p.InFlightAt.IsZero() || p.Secrets.RegistrationRequest != string(f.original) || !matchesNativeProof(message, f.request) {
		t.Fatal("send before atomic original claim")
	}
	return rpcResponse(t, req, registrationMessage(f.result))
}

func TestRegistrationExecutorLostHTTPReplyAndRestartReusesOriginal(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		response := f.response(req, t)
		if calls == 1 {
			_ = response.Body.Close()
			return nil, errors.New("lost committed reply")
		}
		return response, nil
	})
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || due.IsZero() || !p.Attempt.Uncertain || p.Attempt.Phase != enrollment.Registering || p.Secrets.NodeCredential != "" || calls != 1 {
		t.Fatal("lost reply became success", err)
	}
	recovery := p.Attempt.RecoveryUntil
	original := p.Secrets.RegistrationRequest
	// A new executor models process restart; neither original bytes nor the first
	// recovery deadline may change. The server fixture returns its original result.
	e, err = NewRegistrationExecutor(f.store, e.remote, e.config)
	if err != nil {
		t.Fatal(err)
	}
	f.now = due
	due, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p = f.persisted(t)
	if err != nil || !due.IsZero() || p.Attempt.Phase != enrollment.Registered || p.Attempt.Uncertain || p.RemoteInFlight || p.Secrets.NodeCredential != f.result.NodeCredential || p.Secrets.RegistrationRequest != original || !p.Attempt.RecoveryUntil.Equal(recovery) || calls != 2 {
		t.Fatal("original recovery failed", err)
	}
	_, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || calls != 2 {
		t.Fatal("registered attempt dispatched again", err)
	}
}

func TestRegistrationExecutorUnknownClaimWritePreventsSend(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) { calls++; return f.response(req, t), nil })
	f.memory.lose = true
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if !errors.Is(err, state.ErrUncertain) || calls != 0 {
		t.Fatal("send after unknown write", err)
	}
	p := f.persisted(t)
	if p.Secrets.RegistrationRequest != "" {
		// If the lost ACK covered the send marker, a resumed executor must retain
		// that exact first InFlightAt. No inferred fresh operation is allowed.
		if !p.RemoteInFlight {
			t.Fatal("partial claim")
		}
	}
	_, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || calls != 1 || f.persisted(t).Attempt.Phase != enrollment.Registered {
		t.Fatal("claim recovery failed", err)
	}
}

func TestRegistrationExecutorLostLocalResultACKDoesNotDispatchAgain(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		response := f.response(req, t)
		f.memory.lose = true
		return response, nil
	})
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if !errors.Is(err, state.ErrUncertain) || calls != 1 {
		t.Fatal("missing unknown local write", err)
	}
	_, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || calls != 1 || f.persisted(t).Attempt.Phase != enrollment.Registered {
		t.Fatal("saved result sent again", err)
	}
}

func TestRegistrationExecutorCancelRaceReturnsVerifiedCommittedNode(t *testing.T) {
	f := newRegistrationFixture(t)
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		response := f.response(req, t)
		s, err := f.store.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = f.store.Update(context.Background(), s.Revision, func(s *state.State) error {
			p := s.Profiles[f.p.ID]
			a, err := p.Attempt.MarkUncertain(f.now)
			if err != nil {
				return err
			}
			a, err = a.Cancel(f.now)
			p.Attempt = &a
			s.Profiles[p.ID] = p
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return response, nil
	})
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || p.Attempt.Phase != enrollment.Registered || p.Attempt.Uncertain || p.RemoteInFlight {
		t.Fatal("local cancel concealed committed node", err)
	}
}

func TestRegistrationOriginalRequestAndSessionCannotChangeAfterSend(t *testing.T) {
	f := newRegistrationFixture(t)
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		response := f.response(req, t)
		_ = response.Body.Close()
		return nil, ErrRegistrationUnavailable
	})
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"request", "session", "registration_id"} {
		t.Run(name, func(t *testing.T) {
			s, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			err = f.store.Update(context.Background(), s.Revision, func(s *state.State) error {
				p := s.Profiles[f.p.ID]
				switch name {
				case "request":
					p.Secrets.RegistrationRequest += " "
				case "session":
					p.Secrets.Access = "other"
				case "registration_id":
					p.Secrets.RegistrationID = "83e11eba-fd12-46ba-a977-9038c0f6a2f1"
				}
				s.Profiles[p.ID] = p
				return nil
			})
			if !errors.Is(err, state.ErrCorrupt) {
				t.Fatal("original registration inputs changed", err)
			}
		})
	}
}

func TestRegistrationExecutorRecoveryExpiresWithoutAnotherSend(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		response := f.response(req, t)
		_ = response.Body.Close()
		return nil, ErrRegistrationUnavailable
	})
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	p := f.persisted(t)
	original := p.Secrets.RegistrationRequest
	recovery := p.Attempt.RecoveryUntil
	f.now = recovery
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p = f.persisted(t)
	if err != nil || !due.IsZero() || calls != 1 || p.Attempt.Phase != enrollment.Expired || !p.Attempt.Uncertain || !p.Attempt.RecoveryUntil.Equal(recovery) || p.Secrets.RegistrationRequest != original {
		t.Fatal("elapsed recovery dispatched or extended", err)
	}
}

func TestRegistrationExecutorTrustFailureCanResumeWithSameRequest(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) { calls++; return f.response(req, t), nil })
	e.remote.announcements = staticAnnouncement{}
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || due.IsZero() || calls != 0 || p.RemoteInFlight || p.Attempt.Uncertain || p.Registration.BlockedReason != "client.registration.trust_required" {
		t.Fatal("trust failure changed remote certainty", err)
	}
	e.remote.announcements = staticAnnouncement{f.announcement}
	f.now = due
	_, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || calls != 1 || f.persisted(t).Attempt.Phase != enrollment.Registered {
		t.Fatal("same request could not resume", err)
	}
}

func TestRegistrationExecutorTrustChangeDuringCallPreventsCredentialCommit(t *testing.T) {
	f := newRegistrationFixture(t)
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		response := f.response(req, t)
		var bundles signing.ServerKeyResponse
		if err := json.Unmarshal(f.announcement.Bundle, &bundles); err != nil {
			t.Fatal(err)
		}
		bundles.NodeCredentialTrustBundle.Keys[0].ProviderVersion++
		announcement, err := DecodeAnnouncement(f.p.ControlOrigin, encode(t, bundles), f.now)
		if err != nil {
			t.Fatal(err)
		}
		s, err := f.store.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = f.store.Update(context.Background(), s.Revision, func(s *state.State) error {
			p := s.Profiles[f.p.ID]
			p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
			s.Profiles[p.ID] = p
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return response, nil
	})
	_, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if !errors.Is(err, state.ErrStale) || p.Secrets.NodeCredential != "" || p.Attempt.Phase == enrollment.Registered || !p.RemoteInFlight {
		t.Fatal("changed trust accepted previous result", err)
	}
}

func TestRegistrationExecutorInvalidResultPausesWithoutNewEffect(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	f.result.Network.AccountID = "foreign"
	e := f.executor(t, func(req *http.Request) (*http.Response, error) { calls++; return f.response(req, t), nil })
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || !due.IsZero() || p.Registration.BlockedReason == "" || !p.Attempt.Uncertain || p.Secrets.NodeCredential != "" {
		t.Fatal("invalid result promoted", err)
	}
	_, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || calls != 1 {
		t.Fatal("invalid response retried", err)
	}
}

type failedEnrollmentAnnouncement struct{ err error }

func (a failedEnrollmentAnnouncement) Fetch(context.Context, string) (Announcement, error) {
	return Announcement{}, a.err
}

func TestRegistrationBootstrapOutageKeepsKnownRequest(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		return f.response(req, t), nil
	})
	e.remote.announcements = failedEnrollmentAnnouncement{ErrBootstrapUnavailable}
	due, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || due.IsZero() || calls != 0 || p.RemoteInFlight || p.Attempt.Uncertain || p.Registration.BlockedReason != "client.registration.control_unavailable" {
		t.Fatalf("bootstrap outage changed enrollment outcome: %v", err)
	}
	request := p.Secrets.RegistrationRequest
	f.now = due
	e.remote.announcements = staticAnnouncement{f.announcement}
	if _, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt); err != nil {
		t.Fatal(err)
	}
	p = f.persisted(t)
	if calls != 1 || p.Attempt.Phase != enrollment.Registered || p.Registration.BlockedReason != "" || p.Secrets.RegistrationRequest != request {
		t.Fatal("registration failed to resume the original request")
	}
}

func TestRegistrationWireRejectionPreservesEarlierUnknownOutcome(t *testing.T) {
	for _, tc := range []struct {
		wire            string
		httpStatus      int
		reason          string
		previousUnknown bool
	}{
		{"permission_denied", http.StatusForbidden, "client.registration.access_denied", false},
		{"permission_denied", http.StatusForbidden, "client.registration.access_denied", true},
		{"already_exists", http.StatusConflict, "client.registration.request_rejected", false},
		{"already_exists", http.StatusConflict, "client.registration.request_rejected", true},
		{"invalid_argument", http.StatusBadRequest, "client.registration.request_rejected", false},
	} {
		name := tc.wire
		if tc.previousUnknown {
			name += "_after_unknown"
		}
		t.Run(name, func(t *testing.T) {
			f := newRegistrationFixture(t)
			if tc.wire == "already_exists" {
				f.useJoinKey(t)
			}
			calls := 0
			e := f.executor(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if tc.previousUnknown && calls == 1 {
					return nil, io.ErrUnexpectedEOF
				}
				return &http.Response{
					StatusCode: tc.httpStatus, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"code":"` + tc.wire + `","message":"private fixture"}`)), Request: req,
				}, nil
			})
			var earlier time.Time
			if tc.previousUnknown {
				due, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt)
				if err != nil || due.IsZero() {
					t.Fatal("lost response did not schedule exact recovery", err)
				}
				earlier = f.persisted(t).Attempt.RecoveryUntil
				f.now = due
			}
			due, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt)
			p := f.persisted(t)
			if err != nil || !due.IsZero() || p.Registration.BlockedReason != tc.reason ||
				p.Attempt.Uncertain != tc.previousUnknown || p.RemoteInFlight != tc.previousUnknown || !p.Attempt.RecoveryUntil.Equal(earlier) {
				t.Fatalf("wire rejection lost its reason or changed prior certainty: %v", err)
			}
			before := calls
			f.now = f.now.Add(time.Second)
			if _, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt); err != nil || calls != before {
				t.Fatal("definite rejection caused another dispatch", err)
			}
		})
	}
}

func (f *registrationFixture) useJoinKey(t *testing.T) {
	t.Helper()
	const key = "fixture-join-key"
	f.p.JoinTokenDigest = state.CredentialDigest(key)
	f.p.Secrets.Access = ""
	f.p.Secrets.SessionExpiresAt = time.Time{}
	body, err := PrepareJoinRegistration(f.p, key)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Secrets.RegistrationRequest = string(body)
	f.store, f.memory = bootstrapStore(t)
	f.p.RemoteInFlight = false
	f.p.InFlightAt = time.Time{}
	if err := f.store.Update(t.Context(), 1, func(s *state.State) error {
		s.OwnerSID, s.ActiveProfileID = f.p.Attempt.Owner, f.p.ID
		s.Profiles[f.p.ID] = f.p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type quotaDeniedEnrollmentService struct {
	publicqueryv1connect.UnimplementedEnrollmentServiceHandler
}

func (quotaDeniedEnrollmentService) RegisterNode(context.Context, *connect.Request[wire.RegisterNodeRequest]) (*connect.Response[wire.RegisterNodeResponse], error) {
	return nil, contract.DeviceQuotaExhausted()
}

func TestConfirmedQuotaDenialSettlesLostRegistration(t *testing.T) {
	f := newRegistrationFixture(t)
	f.useJoinKey(t)
	_, handler := publicqueryv1connect.NewEnrollmentServiceHandler(quotaDeniedEnrollmentService{})
	calls := 0
	e := f.executor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return nil, io.ErrUnexpectedEOF
		case 2:
		default:
			t.Fatal("request after terminal refusal")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		response := w.Result()
		response.Request = req
		return response, nil
	})
	due, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt)
	if err != nil || due.IsZero() || !f.persisted(t).Attempt.Uncertain {
		t.Fatal("lost response", err)
	}
	f.now = due
	due, err = e.Reconcile(t.Context(), f.p.ID, loginAttempt)
	p := f.persisted(t)
	if err != nil || !due.IsZero() || p.Attempt.Phase != enrollment.Denied || p.RemoteInFlight || p.Attempt.Uncertain || p.Registration.BlockedReason != state.QuotaDeniedReason || p.Secrets.RegistrationRequest != "" {
		t.Fatal("terminal refusal", err, p.Attempt.Phase, p.Registration.BlockedReason)
	}
	if _, err := e.Reconcile(t.Context(), f.p.ID, loginAttempt); err != nil || calls != 2 {
		t.Fatal("resumed terminal refusal", err)
	}
}
