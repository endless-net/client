package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type defaultFixture struct {
	e             *DefaultTargetExecutor
	login         *LoginExecutor
	server        *nativeFixtureServer
	clock         *time.Time
	calls         int
	original      []byte
	result        coordinator.DefaultEnrollmentTarget
	beforeSend    func(*http.Request)
	afterResponse func()
}

type failedDefaultBody struct{}

func (failedDefaultBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func defaultTargetFixture(t *testing.T) *defaultFixture {
	t.Helper()
	login, server, clock := loginFixture(t)
	s, err := login.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	a, err := p.Attempt.LoginSucceeded(*clock)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	p.Secrets.Access = strings.Repeat("a", 43)
	p.Secrets.SessionExpiresAt = clock.Add(time.Hour)
	if err = login.store.Update(t.Context(), s.Revision, func(s *state.State) error { s.Profiles[p.ID] = p; return nil }); err != nil {
		t.Fatal(err)
	}
	f := &defaultFixture{login: login, server: server, clock: clock}
	remote, err := NewDefaultTarget(&http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		f.calls++
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		current, err := login.store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		p := current.Profiles[loginProfile]
		if req.URL.Path != coordinator.DefaultTargetPath || req.Method != "POST" || req.Header.Get("Authorization") != "Bearer "+p.Secrets.Access || !p.RemoteInFlight || p.DefaultTarget == nil || p.DefaultTarget.Request != string(raw) {
			t.Fatal("resolver send before durable original/marker")
		}
		if f.original == nil {
			f.original = append([]byte(nil), raw...)
		} else if !bytes.Equal(f.original, raw) {
			t.Fatal("resolver replay changed bytes")
		}
		r, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest(raw, coordinator.DefaultTargetMaxBytes)
		if err != nil || coordinator.VerifyDefaultTargetIdentityProof(r) != nil {
			t.Fatal("invalid proof", err)
		}
		digest, err := coordinator.DefaultTargetRequestSHA256(r)
		if err != nil {
			t.Fatal(err)
		}
		if f.result.RequestID == "" {
			f.result = coordinator.DefaultEnrollmentTarget{SchemaVersion: 1, RequestID: r.RequestID, RequestSHA256: digest, AccountID: "account", NetworkID: "network", Revision: 1, ResolvedAt: *clock, ExpiresAt: r.ExpiresAt}
		}
		if f.beforeSend != nil {
			f.beforeSend(req)
		}
		out, _ := json.Marshal(f.result)
		if f.afterResponse != nil {
			f.afterResponse()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(out)), Request: req}, nil
	})}, login.native.announcements, RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	f.e, err = NewDefaultTargetExecutor(login.store, remote, RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *defaultFixture) profile(t *testing.T) state.Profile {
	t.Helper()
	s, err := f.login.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return s.Profiles[loginProfile]
}

func TestDefaultResolverBindsRegistrationAndPreservesReceipt(t *testing.T) {
	f := defaultTargetFixture(t)
	before := f.profile(t)
	due, err := f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	p := f.profile(t)
	if err != nil || due.IsZero() || p.Attempt.Phase != enrollment.Registering || p.Attempt.AccountID != "account" || p.Secrets.RegistrationID != before.Attempt.RequestID || p.DefaultTarget.Receipt == "" || p.RemoteInFlight || !bytes.Equal(before.Keys.IdentityPrivate, p.Keys.IdentityPrivate) {
		t.Fatal("default resolution failed", err)
	}
	raw, err := PrepareRegistration(p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := coordinator.DecodeRegisterNodeRequest(raw, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest(f.original, len(f.original))
	if f.result.MatchRegistration(original, r, *f.clock) != nil {
		t.Fatal("registration lost resolver binding")
	}
	s, err := f.login.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.login.store.Update(t.Context(), s.Revision, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		p.DefaultTarget = nil
		s.Profiles[loginProfile] = p
		return nil
	}); !errors.Is(err, state.ErrCorrupt) {
		t.Fatal("durable binding erased", err)
	}
	_, err = f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	if err != nil || f.calls != 1 {
		t.Fatal("resolved attempt dispatched twice", err)
	}
}

func TestDefaultResolverLostReplyAndRestartReuseExactRequest(t *testing.T) {
	f := defaultTargetFixture(t)
	base := f.e.remote.registration.http.Transport
	f.e.remote.registration.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(r)
		if f.calls == 1 && err == nil {
			_ = response.Body.Close()
			return nil, errors.New("lost committed resolver reply")
		}
		return response, err
	})
	due, err := f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	p := f.profile(t)
	if err != nil || due.IsZero() || !p.Attempt.Uncertain || p.DefaultTarget.Receipt != "" {
		t.Fatal("lost reply became outcome", err)
	}
	recovery := p.Attempt.RecoveryUntil
	identity := p.Keys.IdentityPrivate
	f.e, err = NewDefaultTargetExecutor(f.login.store, f.e.remote, f.e.config)
	if err != nil {
		t.Fatal(err)
	}
	*f.clock = due
	_, err = f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	p = f.profile(t)
	if err != nil || p.Attempt.Phase != enrollment.Registering || p.Attempt.Uncertain || !p.Attempt.RecoveryUntil.Equal(recovery) || !bytes.Equal(identity, p.Keys.IdentityPrivate) || f.calls != 2 {
		t.Fatal("original resolver recovery failed", err)
	}
}

func TestDefaultResolverPartialReplyRemainsRecoverable(t *testing.T) {
	f := defaultTargetFixture(t)
	base := f.e.remote.registration.http.Transport
	f.e.remote.registration.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(r)
		if f.calls == 1 && err == nil {
			_ = response.Body.Close()
			response.Body = io.NopCloser(io.MultiReader(strings.NewReader("{"), failedDefaultBody{}))
		}
		return response, err
	})
	due, err := f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	p := f.profile(t)
	if err != nil || due.IsZero() || !p.Attempt.Uncertain || p.DefaultTarget.BlockedReason != "" {
		t.Fatal("transport truncation treated as forged result", err)
	}
	*f.clock = due
	_, err = f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	if err != nil || f.calls != 2 || f.profile(t).Attempt.Phase != enrollment.Registering {
		t.Fatal("truncated receipt could not recover original outcome", err)
	}
}

func TestDefaultResolverLostClaimOrResultACK(t *testing.T) {
	for _, claim := range []bool{true, false} {
		t.Run(map[bool]string{true: "claim", false: "result"}[claim], func(t *testing.T) {
			f := defaultTargetFixture(t)
			if claim {
				f.server.memory.lose = true
			} else {
				f.afterResponse = func() { f.server.memory.lose = true }
			}
			_, err := f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
			if !errors.Is(err, state.ErrUncertain) || claim && f.calls != 0 || !claim && f.calls != 1 {
				t.Fatal("unknown local commit mishandled", err)
			}
			f.afterResponse = nil
			_, err = f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
			if err != nil || f.profile(t).Attempt.Phase != enrollment.Registering || f.calls != 1 {
				t.Fatal("lost ACK repeated result/new request", err)
			}
		})
	}
}

func TestDefaultResolverRefusesChangedReceiptAndCancelledLateResult(t *testing.T) {
	for _, kind := range []string{"binding", "expired", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := defaultTargetFixture(t)
			f.beforeSend = func(*http.Request) {
				switch kind {
				case "binding":
					f.result.RequestSHA256 = strings.Repeat("f", 64)
				case "expired":
					f.result.ExpiresAt = *f.clock
				case "cancel":
					s, err := f.login.store.Load(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					err = f.login.store.Update(t.Context(), s.Revision, func(s *state.State) error {
						p := s.Profiles[loginProfile]
						a, err := p.Attempt.Cancel(*f.clock)
						p.Attempt = &a
						s.Profiles[loginProfile] = p
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err := f.e.Reconcile(context.Background(), loginProfile, loginAttempt)
			p := f.profile(t)
			if err != nil || p.Attempt.Phase == enrollment.Registering || p.DefaultTarget.Receipt != "" || p.Secrets.RegistrationID != "" {
				t.Fatal("invalid/late receipt authorized registration", err)
			}
			if kind != "cancel" && p.DefaultTarget.BlockedReason != "client.default_target.invalid_response" {
				t.Fatal("invalid receipt did not stop retry")
			}
		})
	}
}

func TestDefaultWorkerSchedulesResolverAndNextRegistration(t *testing.T) {
	f := defaultTargetFixture(t)
	register, err := NewRegistrationExecutor(f.login.store, f.e.remote.registration, f.e.config)
	if err != nil {
		t.Fatal(err)
	}
	// The browser executor is deliberately unused by this native worker phase.
	approve := &ApprovalExecutor{store: f.login.store}
	w, err := NewDefaultEnrollmentWorker(f.login, register, approve, f.e, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Wake(t.Context(), loginProfile, loginAttempt); err != nil {
		t.Fatal(err)
	}
	due, err := w.reconcile(t.Context(), loginJob{profile: loginProfile, attempt: loginAttempt})
	if err != nil || due.IsZero() || f.profile(t).Attempt.Phase != enrollment.Registering || f.calls != 1 {
		t.Fatal("default worker did not advance", err)
	}
	// Existing login-only constructor must not silently gain resolver authority.
	loginOnly, err := NewLoginWorker(f.login, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if loginOnly.defaultTarget != nil {
		t.Fatal("login-only scope expanded")
	}
}

func TestDefaultWorkerKeepsWakeAfterNativeLoginChangesPhase(t *testing.T) {
	login, server, clock := loginFixture(t)
	server.authorized = true
	remote, err := NewDefaultTarget(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("resolver called during login pass")
		return nil, ErrDefaultTargetProtocol
	})}, login.native.announcements, RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	config := RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return *clock }}
	target, err := NewDefaultTargetExecutor(login.store, remote, config)
	if err != nil {
		t.Fatal(err)
	}
	register, err := NewRegistrationExecutor(login.store, remote.registration, config)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewDefaultEnrollmentWorker(login, register, &ApprovalExecutor{store: login.store}, target, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	due, err := w.reconcile(t.Context(), loginJob{profile: loginProfile, attempt: loginAttempt})
	s, readErr := login.store.Load(t.Context())
	if err != nil || readErr != nil || due.IsZero() || s.Profiles[loginProfile].Attempt.Phase != enrollment.ChoosingContext || server.starts != 1 {
		t.Fatal("login phase dropped resolver wake", err, readErr)
	}
}

func TestDefaultResolverFailsClosedBeforeCredentialSend(t *testing.T) {
	f := defaultTargetFixture(t)
	f.e.remote.registration.announcements = staticAnnouncement{}
	due, err := f.e.Reconcile(t.Context(), loginProfile, loginAttempt)
	p := f.profile(t)
	if err != nil || due.IsZero() || f.calls != 0 || p.Attempt.Uncertain || p.DefaultTarget.BlockedReason != "client.default_target.trust_required" || p.RemoteInFlight {
		t.Fatal("bootstrap failure sent credentials or asserted uncertainty", err)
	}
}
