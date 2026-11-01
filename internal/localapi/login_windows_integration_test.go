//go:build windows && integration

package localapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"connectrpc.com/connect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type nativeHTTPFixture struct {
	store      *state.Store
	mu         sync.Mutex
	tx         *identityapi.NativeLoginTransaction
	authorized bool
}

func (f *nativeHTTPFixture) StartNativeLogin(ctx context.Context, r *connect.Request[identityapi.StartNativeLoginRequest]) (*connect.Response[identityapi.StartNativeLoginResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	p := s.Profiles[s.ActiveProfileID]
	if !p.RemoteInFlight || r.Header().Get("Authorization") != "Bearer "+p.Secrets.RequestProof {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("fixture proof/send marker missing"))
	}
	if f.tx == nil {
		now := time.Now().UTC()
		f.tx = &identityapi.NativeLoginTransaction{TransactionId: "3d8bdf1c-b741-438e-b908-471e4e2a60da", RequestId: r.Msg.RequestId, AttemptBindingSha256: r.Msg.AttemptBindingSha256, State: identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(600 * time.Second)), ReplayExpiresAt: timestamppb.New(now.Add(1320 * time.Second)), PollAfterSeconds: 1}
		f.tx.Outcome = &identityapi.NativeLoginTransaction_BrowserAction{BrowserAction: &identityapi.NativeLoginBrowserAction{VerificationUrl: p.ControlOrigin + "/api/v1/auth/native-login/" + f.tx.TransactionId, ExpiresAt: f.tx.ExpiresAt}}
	}
	return connect.NewResponse(&identityapi.StartNativeLoginResponse{Transaction: proto.Clone(f.tx).(*identityapi.NativeLoginTransaction), PollAuthorization: strings.Repeat("p", 43)}), nil
}
func (f *nativeHTTPFixture) GetNativeLogin(_ context.Context, r *connect.Request[identityapi.GetNativeLoginRequest]) (*connect.Response[identityapi.GetNativeLoginResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tx == nil || r.Msg.TransactionId != f.tx.TransactionId || r.Header().Get("Authorization") != "Bearer "+strings.Repeat("p", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("fixture poll proof missing"))
	}
	t := proto.Clone(f.tx).(*identityapi.NativeLoginTransaction)
	if !f.authorized {
		return connect.NewResponse(&identityapi.GetNativeLoginResponse{Transaction: t}), nil
	}
	t.State = identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED
	t.PollAfterSeconds = 0
	t.Outcome = &identityapi.NativeLoginTransaction_Result{Result: &identityapi.NativeLoginResult{Session: &sessionv1.UserSession{SessionId: "native-session", UserId: "native-user", State: sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)), RenewalSupported: true}, AccessBearer: strings.Repeat("a", 43), RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{Bearer: strings.Repeat("n", 43), ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour))}}}
	return connect.NewResponse(&identityapi.GetNativeLoginResponse{Transaction: t}), nil
}
func (*nativeHTTPFixture) CancelNativeLogin(context.Context, *connect.Request[identityapi.CancelNativeLoginRequest]) (*connect.Response[identityapi.CancelNativeLoginResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not needed by waiting fixture"))
}

type nativeFixtureTransport struct{ mux *http.ServeMux }

func (f nativeFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	response := w.Result()
	response.Request = r
	return response, nil
}

func TestWindowsControllerWithNativeLoginWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit Windows IPC/DPAPI + generated upstream SDK fixture")
	}
	s, _, sid := nativeStore(t)
	now := time.Now()
	cfg := config(&now)
	cfg.Now = time.Now
	cfg.Targets = &targetCatalog{items: []*clientv0.EnrollmentTarget{{AccountId: "account-a", NetworkId: "network-a", Restriction: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}}}}
	cfg.TargetPageKey = [32]byte{5}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeID = id
	mux := http.NewServeMux()
	upstream := &nativeHTTPFixture{store: s}
	path, h := identityapiconnect.NewNativeLoginServiceHandler(upstream)
	mux.Handle(path, h)
	n, err := control.NewNative(&http.Client{Transport: nativeFixtureTransport{mux}}, cfg.Announcements, 1<<20, "/api/v1/auth/native-login/", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	e, err := control.NewLoginExecutor(s, n, control.LoginConfig{MaxPending: 2, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := control.NewLoginWorker(e, tracenoop.NewTracerProvider().Tracer("fixture"), metricnoop.NewMeterProvider().Meter("fixture"), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(s, worker, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop := nativeClient(t, c, sid)
	defer stop()
	runCtx, cancelWorker := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx) }()
	defer func() {
		cancelWorker()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("native worker did not stop")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	view, err := client.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	r := request(view.Status.Metadata.Revision)
	r.Mutation.ExpectedInstanceId = cfg.RuntimeID
	started, err := client.Enroll(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if started.Attempt.Restriction == nil {
		t.Fatal("unconfirmed login was dispatched")
	}
	// This fixture seeds synthetic public trust independently of OS elevation.
	// Trust RPC authority is covered separately by the real-token native suite.
	current, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	announcement, err := cfg.Announcements.Fetch(ctx, cfg.DefaultControlOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(ctx, current.Revision, func(s *state.State) error {
		p := s.Profiles[defaultID]
		p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
		s.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sawBrowser := false
	for {
		got, err := client.GetEnrollmentAttempt(ctx, getRequest())
		if err != nil {
			t.Fatal(err)
		}
		if got.Attempt.BrowserAction != nil {
			sawBrowser = true
			// Model explicit user consent after the browser action was observed,
			// rather than auto-authorizing before the UI could ever see it.
			upstream.mu.Lock()
			upstream.authorized = true
			upstream.mu.Unlock()
		}
		if got.Attempt.Phase == clientv0.EnrollmentPhase_ENROLLMENT_PHASE_CHOOSING_CONTEXT {
			data, err := protojson.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if !sawBrowser || got.Attempt.NodeId != nil || strings.Contains(string(data), strings.Repeat("a", 43)) || strings.Contains(string(data), strings.Repeat("n", 43)) {
				t.Fatal("native UI projection lost browser state or leaked grants")
			}
			break
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("native login worker did not complete", ctx.Err())
		case <-timer.C:
		}
	}
	current, err = s.Load(ctx)
	if err != nil || current.OwnerSID != sid || current.Profiles[defaultID].Secrets.SessionID != "native-session" {
		t.Fatal("native session not protected/caller-bound", err)
	}
	choices, err := client.ListEnrollmentTargets(ctx, &clientv0.ListEnrollmentTargetsRequest{AttemptId: started.Attempt.AttemptId})
	if err != nil || len(choices.Targets) != 1 {
		t.Fatal("native target projection failed", err)
	}
	selected, err := client.SelectEnrollmentTarget(ctx, &clientv0.SelectEnrollmentTargetRequest{Mutation: &clientv0.MutationContext{ExpectedInstanceId: cfg.RuntimeID, ExpectedRevision: choices.Metadata.Revision}, AttemptId: started.Attempt.AttemptId, AccountId: "account-a", NetworkId: "network-a"})
	if err != nil || selected.Attempt.NodeId != nil || selected.Attempt.Restriction.GetReasonKey() != "client.enrollment.registration_unavailable" {
		t.Fatal("selection bypassed missing admission executor", err)
	}
}
