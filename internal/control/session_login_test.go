package control

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

const sessionLoginRequest = "01235942-1452-427f-bcd7-5cec8a211f47"
const sessionLoginAttempt = "2800c45a-10e6-483c-b0bf-ea31c686a228"

func sessionWorkerFixture(t *testing.T) (*SessionLoginWorker, *nativeFixtureServer, *time.Time) {
	t.Helper()
	e, server, clock := loginFixture(t)
	server.authorized = true
	if _, err := e.Reconcile(t.Context(), loginProfile, loginAttempt); err != nil {
		t.Fatal(err)
	}
	s, err := e.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Update(t.Context(), s.Revision, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID = "account"
		p.Attempt.NetworkID = "network"
		p.Attempt.NodeID = "same-node"
		p.Attempt.CredentialEnds = clock.Add(time.Hour)
		p.Secrets.RegistrationID = "f58fd7b4-38e2-48bb-8734-fcf7ff23cd48"
		p.Secrets.RegistrationRequest = "original-request"
		p.Secrets.NodeCredential = "enc_original"
		p.SessionRefresh = &state.SessionRefresh{RequestID: "13266c15-5293-49d6-b205-e52966fec374", ExpectedSessionID: p.Secrets.SessionID, UserID: p.Secrets.UserID, Renewal: p.Secrets.Renewal, CreatedAt: *clock, Deadline: clock.Add(10 * time.Minute)}
		var err error
		p.SessionLogin, err = state.NewSessionLogin(p, sessionLoginRequest, sessionLoginAttempt, strings.Repeat("q", 43), *clock)
		if err != nil {
			return err
		}
		s.Profiles[loginProfile] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, err = e.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.Update(t.Context(), s.Revision, func(s *state.State) error {
		p := s.Profiles[loginProfile]
		p.SessionRefresh.SentAt = *clock
		s.Profiles[loginProfile] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server.relogin = true
	server.authorized = false
	server.tx = nil
	server.created = 0
	server.starts = 0
	server.gets = 0
	w, err := NewSessionLoginWorker(e.store, e.native, e.config, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	return w, server, clock
}

func TestSessionLoginWorkerLostStartRestartAndUnknownRefresh(t *testing.T) {
	w, server, clock := sessionWorkerFixture(t)
	s, err := w.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := s.Profiles[loginProfile]
	server.lostStart = true
	if _, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil {
		t.Fatal(err)
	}
	s, err = w.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Profiles[loginProfile].SessionLogin.Phase != state.SessionLoginUncertain {
		t.Fatal("lost Start became known denial")
	}
	*clock = clock.Add(time.Second)
	restarted, err := NewSessionLoginWorker(w.store, w.native, w.config, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil {
		t.Fatal(err)
	}
	if server.starts != 2 || server.created != 1 {
		t.Fatal("restart created another native transaction")
	}
	server.authorized = true
	*clock = clock.Add(2 * time.Second)
	if _, err := restarted.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil {
		t.Fatal(err)
	}
	s, err = w.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	current, err := p.CurrentSession()
	if err != nil || current.SessionID != "relogin-session" || current.UserID != "native-user" || p.SessionLogin.Phase != state.SessionLoginSucceeded {
		t.Fatal("fresh matching session was not adopted", err)
	}
	if !reflect.DeepEqual(p.Secrets, original.Secrets) || !reflect.DeepEqual(p.Attempt, original.Attempt) || !bytes.Equal(p.Keys.IdentityPrivate, original.Keys.IdentityPrivate) || !bytes.Equal(p.Keys.WireGuardPrivate, original.Keys.WireGuardPrivate) || !reflect.DeepEqual(p.SessionRefreshHistory[original.SessionRefresh.RequestID], original.SessionRefresh) || p.SessionRefresh != nil {
		t.Fatal("registration/keys/unknown operation changed")
	}
	count := server.starts + server.gets
	if _, err := restarted.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil || server.starts+server.gets != count {
		t.Fatal("completed replay dispatched again", err)
	}
}

func TestSessionLoginWorkerForeignUserAndLostAdoptionACK(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		w, server, clock := sessionWorkerFixture(t)
		if _, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil {
			t.Fatal(err)
		}
		server.authorized = true
		server.foreignUser = foreign
		server.loseResult = !foreign
		*clock = clock.Add(2 * time.Second)
		_, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt)
		if !foreign && !errors.Is(err, state.ErrUncertain) {
			t.Fatal("lost adoption ACK returned success", err)
		}
		if foreign && err != nil {
			t.Fatal(err)
		}
		s, err := w.store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		p := s.Profiles[loginProfile]
		current, err := p.CurrentSession()
		if err != nil {
			t.Fatal(err)
		}
		if foreign {
			if p.SessionLogin.Phase != state.SessionLoginFailed || p.SessionLogin.Reason != state.SessionLoginForeignUser || current.SessionID != "native-session" {
				t.Fatal("foreign user grant adopted")
			}
			if bytes.Contains(p.SessionLogin.Transaction, []byte("foreign-user")) {
				t.Fatal("unaccepted foreign credentials retained")
			}
		} else {
			count := server.starts + server.gets
			if _, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil || server.starts+server.gets != count || current.SessionID != "relogin-session" {
				t.Fatal("lost ACK generated new authority", err)
			}
		}
	}
}

func TestSessionLoginRunRecoversPersistedAttemptWithoutUIWake(t *testing.T) {
	w, server, _ := sessionWorkerFixture(t)
	server.authorized = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, err := w.store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if s.Profiles[loginProfile].SessionLogin.Phase == state.SessionLoginSucceeded {
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("startup did not recover durable attempt")
}

func TestSessionLoginUnknownOutcomeStopsAtOriginalRecoveryBudget(t *testing.T) {
	w, server, clock := sessionWorkerFixture(t)
	original := *clock
	server.lostStart = true
	if _, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(121 * time.Second)
	if due, err := w.Reconcile(t.Context(), loginProfile, sessionLoginAttempt); err != nil || !due.IsZero() {
		t.Fatal("exhausted recovery rescheduled", err)
	}
	s, err := w.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	l := s.Profiles[loginProfile].SessionLogin
	if server.starts != 1 || server.created != 1 || l.Phase != state.SessionLoginUncertain || l.Reason != state.SessionLoginOutcomeUnknown || !l.Deadline.Equal(original.Add(10*time.Minute)) || !l.RecoveryUntil.Equal(original.Add(2*time.Minute)) {
		t.Fatal("unknown outcome retried, promoted or extended")
	}
	if err := w.recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(w.jobs) != 0 {
		t.Fatal("paused unknown operation queued again")
	}
	*clock = original.Add(11 * time.Minute)
	if err := w.recover(t.Context()); err != nil || len(w.jobs) != 0 {
		t.Fatal("expired unknown outcome entered a busy loop", err)
	}
}
