package control

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

func TestEnrollmentWorkerResumesDurableRegistrationAndKeepsConcurrentWake(t *testing.T) {
	f := newRegistrationFixture(t)
	var worker *LoginWorker
	called := make(chan struct{}, 1)
	registration := f.executor(t, func(req *http.Request) (*http.Response, error) {
		response := f.response(req, t)
		// A fresh controller Wake concurrent with the active job must survive its
		// completion, even if that completion changes phase and returns zero due.
		worker.mu.Lock()
		before := worker.jobs[f.p.ID].generation
		worker.mu.Unlock()
		if err := worker.Wake(req.Context(), f.p.ID, loginAttempt); err != nil {
			t.Error(err)
		}
		worker.mu.Lock()
		after := worker.jobs[f.p.ID].generation
		worker.mu.Unlock()
		if after != before+1 {
			t.Error("concurrent wake lost")
		}
		called <- struct{}{}
		return response, nil
	})
	native, err := NewNative(&http.Client{}, staticAnnouncement{f.announcement}, 65536, "/api/v1/auth/native-login/", func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	login, err := NewLoginExecutor(f.store, native, LoginConfig{MaxPending: 1, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	worker, err = NewEnrollmentWorker(login, registration, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("worker did not stop")
		}
	}()
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("persisted registration not resumed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p := f.persisted(t)
		worker.mu.Lock()
		remaining := len(worker.jobs)
		worker.mu.Unlock()
		if p.Attempt.Phase == enrollment.Registered && remaining == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("registration result or queued wake not consumed")
}

func TestEnrollmentWorkerRetainsLoginOnlyBoundary(t *testing.T) {
	f := newRegistrationFixture(t)
	registration := f.executor(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected call")
		return nil, ErrRegistrationProtocol
	})
	native, err := NewNative(&http.Client{}, staticAnnouncement{f.announcement}, 65536, "/api/v1/auth/native-login/", func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	login, err := NewLoginExecutor(f.store, native, LoginConfig{MaxPending: 1, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewLoginWorker(login, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Wake(context.Background(), f.p.ID, loginAttempt); !errors.Is(err, ErrLoginPhaseUnsupported) {
		t.Fatal("login-only worker expanded its authority", err)
	}
	other, err := state.New(&memory{}, testCodec{}, loginProfile, 65536, 131072)
	if err != nil {
		t.Fatal(err)
	}
	registration.store = other
	if _, err = NewEnrollmentWorker(login, registration, workerTracer(), workerMeter(), workerLogger()); err == nil {
		t.Fatal("different state owners accepted")
	}
}
