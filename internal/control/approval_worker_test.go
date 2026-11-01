package control

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestApprovalWorkerExplicitConstructorResumesAndKeepsConcurrentWake(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	f.approve()
	a, err := f.p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	f.p.Approval.Snapshot = &snapshot
	var worker *LoginWorker
	var calls atomic.Int32
	approval := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		approvalSendClaim(t, &f, req)
		if err := worker.Wake(req.Context(), f.p.ID, loginAttempt); err != nil {
			t.Error("concurrent browser Wake lost", err)
		}
		f.enrolled(t)
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	native, err := NewNative(&http.Client{}, staticAnnouncement{f.announcement}, 65536, "/api/v1/auth/native-login/", func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	login, err := NewLoginExecutor(f.store, native, LoginConfig{MaxPending: 1, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	registration, err := NewRegistrationExecutor(f.store, f.adapter(t, func(*http.Request) (*http.Response, error) {
		t.Error("browser entered native direct registration")
		return nil, errors.New("unexpected")
	}), RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := NewEnrollmentWorker(login, registration, workerTracer(), workerMeter(), workerLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Wake(context.Background(), f.p.ID, loginAttempt); !errors.Is(err, ErrLoginPhaseUnsupported) {
		t.Fatal("existing constructor enabled browser dispatch", err)
	}
	worker, err = NewApprovalEnrollmentWorker(login, registration, approval, workerTracer(), workerMeter(), workerLogger())
	if err != nil || calls.Load() != 0 {
		t.Fatal("constructor performed remote I/O", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Error("browser worker did not stop with its owner", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("browser worker failed bounded stop")
		}
	}()
	limit := time.NewTimer(2 * time.Second)
	defer limit.Stop()
	for {
		p := f.persisted(t)
		if p.Attempt.Phase == enrollment.Registered {
			if calls.Load() != 1 || p.Secrets.NodeCredential == "" || p.RemoteInFlight || p.Approval.Snapshot.Request.Status != "enrolled" {
				t.Fatal("startup browser resume lost atomic result")
			}
			return
		}
		select {
		case <-limit.C:
			t.Fatal("startup did not resume durable browser completion")
		case <-time.After(time.Millisecond):
		}
	}
}
