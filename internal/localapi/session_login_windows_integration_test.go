//go:build windows && integration

package localapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	billing "github.com/endless-net/billing/api/public/v1"
	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
	management "github.com/endless-net/management/api/v1"
	"github.com/endless-net/management/api/v1/managementapiconnect"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type reloginDashboardFixture struct {
	billingpublicapiconnect.UnimplementedBillingUserServiceHandler
	managementapiconnect.UnimplementedManagementDashboardServiceHandler
}

func (reloginDashboardFixture) ListAccounts(_ context.Context, req *connect.Request[billing.ListAccountsRequest]) (*connect.Response[billing.ListAccountsResponse], error) {
	if req.Header().Get("Authorization") != "Bearer "+strings.Repeat("x", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("new session required"))
	}
	return connect.NewResponse(&billing.ListAccountsResponse{Accounts: []*billing.Account{{AccountId: "account", Name: "Personal"}}}), nil
}

func (reloginDashboardFixture) GetPrimaryAccount(_ context.Context, req *connect.Request[billing.GetPrimaryAccountRequest]) (*connect.Response[billing.GetPrimaryAccountResponse], error) {
	if req.Header().Get("Authorization") != "Bearer "+strings.Repeat("x", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("new session required"))
	}
	return connect.NewResponse(&billing.GetPrimaryAccountResponse{Account: &billing.Account{AccountId: "account", Name: "Personal"}}), nil
}

func (reloginDashboardFixture) GetBoard(_ context.Context, req *connect.Request[management.GetBoardRequest]) (*connect.Response[management.GetBoardResponse], error) {
	if req.Header().Get("Authorization") != "Bearer "+strings.Repeat("x", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("new session required"))
	}
	if req.Msg.GetAccountId() != "account" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown account"))
	}
	return connect.NewResponse(&management.GetBoardResponse{Board: &management.DashboardBoard{
		AccountId: req.Msg.GetAccountId(),
		Layout:    &management.DashboardLayout{Image: management.DashboardImage_DASHBOARD_IMAGE_RIBBON},
	}}), nil
}

func TestWindowsSessionLoginOwnerPipeDPAPIAndUIReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit Windows peer-auth/DPAPI/session worker fixture; no real Node/network changes")
	}
	s, fault, sid := nativeStore(t)
	now := time.Now()
	cfg := config(&now)
	cfg.Now = time.Now
	mux := http.NewServeMux()
	upstream := &nativeHTTPFixture{store: s, relogin: true}
	path, handler := identityapiconnect.NewNativeLoginServiceHandler(upstream)
	mux.Handle(path, handler)
	mux.Handle(billingpublicapiconnect.NewBillingUserServiceHandler(reloginDashboardFixture{}))
	mux.Handle(managementapiconnect.NewManagementDashboardServiceHandler(reloginDashboardFixture{}))
	upstreamHTTP := &http.Client{Transport: nativeFixtureTransport{mux}}
	dashboard, err := control.NewDashboard(upstreamHTTP, cfg.Announcements, control.TargetConfig{
		MaxCalls: 4, MaxAccounts: 4, MaxResponseBytes: 65536, UpstreamPageSize: 4, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Dashboard = dashboard
	native, err := control.NewNative(upstreamHTTP, cfg.Announcements, 65536, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	w, err := control.NewSessionLoginWorker(s, native, control.LoginConfig{MaxPending: 2, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: time.Now}, tracenoop.NewTracerProvider().Tracer("fixture"), metricnoop.NewMeterProvider().Meter("fixture"), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	cfg.SessionLogins = w
	cfg.MaxSessionLogins = 2
	c, err := New(s, &testRunner{store: s}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop := nativeClient(t, c, sid)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.StartSessionLogin(ctx, sessionRequest()); status.Code(err) != codes.PermissionDenied {
		stop()
		t.Fatal("fresh installation claimed by session login", err)
	}
	view, err := client.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil {
		stop()
		t.Fatal(err)
	}
	initial := request(view.Status.Metadata.Revision)
	if _, err := client.Enroll(ctx, initial); err != nil {
		stop()
		t.Fatal(err)
	}
	value, err := s.Load(ctx)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	announcement, err := cfg.Announcements.Fetch(ctx, cfg.DefaultControlOrigin)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	if err := s.Update(ctx, value.Revision, func(value *state.State) error {
		p := value.Profiles[defaultID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID = "account"
		p.Attempt.NetworkID = "network"
		p.Attempt.NodeID = "same-node"
		p.Attempt.CredentialEnds = time.Now().Add(-time.Minute)
		p.Secrets.RegistrationID = requestID
		p.Secrets.RegistrationRequest = "fixture-initial-request"
		p.Secrets.NodeCredential = "enc_fixture"
		p.Secrets.UserID = "native-user"
		p.Secrets.SessionID = "old-native-session"
		p.Secrets.Access = strings.Repeat("a", 43)
		p.Secrets.Renewal = strings.Repeat("n", 43)
		p.Secrets.SessionExpiresAt = time.Now().Add(-time.Minute)
		p.Secrets.RenewalExpiresAt = time.Now().Add(-time.Minute)
		p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
		value.Profiles[defaultID] = p
		return nil
	}); err != nil {
		stop()
		t.Fatal(err)
	}
	before, err := s.Load(ctx)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	r := sessionRequest()
	r.Mutation.ExpectedRevision = before.Revision
	fault.lose.Store(true)
	if _, err := client.StartSessionLogin(ctx, r); status.Code(err) != codes.Unavailable {
		stop()
		t.Fatal("lost saved ACK reported accepted", err)
	}
	query := &clientv0.GetSessionLoginRequest{Selector: &clientv0.GetSessionLoginRequest_RequestId{RequestId: sessionRequestID}}
	read, err := client.GetSessionLogin(ctx, query)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	firstID := read.Attempt.AttemptId
	deadline := read.Attempt.Deadline.AsTime()
	stop() // UI connection goes away; no Disconnect or login cancellation.
	run, cancelWorker := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(run) }()
	defer func() {
		cancelWorker()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("session worker did not stop")
		}
	}()
	cfg.RuntimeID, err = randomID()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(s, c.runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop = nativeClient(t, reopened, sid)
	defer stop()
	replay, err := client.StartSessionLogin(ctx, r)
	if err != nil || replay.Attempt.AttemptId != firstID || !replay.Attempt.Deadline.AsTime().Equal(deadline) {
		t.Fatal("reopen changed request/budget or required old CAS", err)
	}
	sawBrowser := false
	for ctx.Err() == nil {
		read, err := client.GetSessionLogin(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if read.Attempt.BrowserAction != nil {
			sawBrowser = true
			if !strings.HasPrefix(read.Attempt.BrowserAction.VerificationUrl, cfg.DefaultControlOrigin+"/native-login/") {
				t.Fatal("wrong browser origin")
			}
			upstream.mu.Lock()
			upstream.authorized = true
			upstream.mu.Unlock()
		}
		if read.Attempt.Phase == clientv0.SessionLoginPhase_SESSION_LOGIN_PHASE_SUCCEEDED {
			if !sawBrowser {
				t.Fatal("confirmation state not observed")
			}
			session, err := client.GetSession(ctx, &clientv0.GetSessionRequest{})
			if err != nil || session.Session.State != clientv0.SessionState_SESSION_STATE_ACTIVE {
				t.Fatal("new session not projected", err)
			}
			spaces, err := client.ListDashboardSpaces(ctx, &clientv0.ListDashboardSpacesRequest{})
			if err != nil || len(spaces.GetSpaces()) != 1 || spaces.GetPrimaryAccountId() != "account" {
				t.Fatal("could not list dashboard spaces after re-login", err)
			}
			board, err := client.GetDashboardBoard(ctx, &clientv0.GetDashboardBoardRequest{AccountId: spaces.GetPrimaryAccountId()})
			if err != nil || board.GetBoard().GetLayout().GetImage() != clientv0.DashboardImage_DASHBOARD_IMAGE_RIBBON {
				t.Fatal("could not load dashboard board after re-login", err)
			}
			after, err := s.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			p, old := after.Profiles[defaultID], before.Profiles[defaultID]
			if !reflect.DeepEqual(old.Secrets, p.Secrets) || !reflect.DeepEqual(old.Attempt, p.Attempt) || !bytes.Equal(old.Keys.IdentityPrivate, p.Keys.IdentityPrivate) || !bytes.Equal(old.Keys.WireGuardPrivate, p.Keys.WireGuardPrivate) {
				t.Fatal("re-login changed original registration/keys")
			}
			public, err := protojson.Marshal(session)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{p.SessionLogin.RequestProof, p.SessionLogin.PollProof, strings.Repeat("x", 43), strings.Repeat("y", 43)} {
				if bytes.Contains(public, []byte(secret)) {
					t.Fatal("IPC leaked private native material")
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native session login did not finish", connect.CodeOf(ctx.Err()))
}
