package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	billing "github.com/endless-net/billing/api/public/v1"
	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	ipc "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/state"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	remote "github.com/endless-net/management/api/v1"
	"github.com/endless-net/management/api/v1/managementapiconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type dashboardOwners struct {
	*accountFixture
	managementapiconnect.UnimplementedManagementDashboardServiceHandler
	mutations int
}

func (f *dashboardOwners) GetPrimaryAccount(_ context.Context, r *connect.Request[billing.GetPrimaryAccountRequest]) (*connect.Response[billing.GetPrimaryAccountResponse], error) {
	if r.Header().Get("Authorization") != "Bearer "+f.access {
		f.t.Fatal("primary query lost native proof")
	}
	return connect.NewResponse(&billing.GetPrimaryAccountResponse{Account: &billing.Account{AccountId: "account-b", Name: "B"}}), nil
}

func (f *dashboardOwners) ReplaceBoard(_ context.Context, r *connect.Request[remote.ReplaceBoardRequest]) (*connect.Response[remote.ReplaceBoardResponse], error) {
	f.mutations++
	if r.Header().Get("Authorization") != "Bearer "+f.access ||
		r.Msg.AccountId != "account-b" || r.Msg.ExpectedRevision == nil ||
		r.Msg.GetExpectedRevision() != 0 || r.Msg.Layout.Image != remote.DashboardImage_DASHBOARD_IMAGE_RIBBON {
		f.t.Fatal("mutation lost owner scope, revision presence or token")
	}
	return nil, connect.NewError(connect.CodeAborted, errors.New("synthetic private detail"))
}

func (f *dashboardOwners) GetBoard(_ context.Context, r *connect.Request[remote.GetBoardRequest]) (*connect.Response[remote.GetBoardResponse], error) {
	if r.Header().Get("Authorization") != "Bearer "+f.access || r.Msg.AccountId != "account-b" {
		f.t.Fatal("read lost account scope or current session")
	}
	return connect.NewResponse(&remote.GetBoardResponse{Board: &remote.DashboardBoard{
		AccountId: r.Msg.AccountId,
		CanEdit:   true,
		Layout:    &remote.DashboardLayout{Image: remote.DashboardImage_DASHBOARD_IMAGE_RIBBON},
	}}), nil
}

func TestDashboardSelectedPersonalAccountAndOneConditionalWrite(t *testing.T) {
	targets, profile, accounts, _ := targetFixture(t)
	profile.Secrets.UserID = "signed-user"
	// An active session can browse without entering registration target selection.
	profile.Attempt = nil
	now := targets.config.Now()
	profile.Secrets.SessionExpiresAt = now.Add(-time.Second)
	accounts.access = strings.Repeat("n", 43)
	const requestID = "00000000-0000-4000-8000-000000000001"
	active, err := proto.Marshal(&sessionv1.SessionRenewal{
		OperationId:       "renewal-operation",
		RequestId:         requestID,
		PreviousSessionId: profile.Secrets.SessionID,
		State:             sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED,
		ReplayExpiresAt:   timestamppb.New(now.Add(12 * time.Minute)),
		Outcome: &sessionv1.SessionRenewal_Result{Result: &sessionv1.SessionRenewalResult{
			Session: &sessionv1.UserSession{
				SessionId:        "rotated-session",
				UserId:           profile.Secrets.UserID,
				State:            sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE,
				ExpiresAt:        timestamppb.New(now.Add(time.Hour)),
				RenewalSupported: true,
			},
			AccessBearer: accounts.access,
			RenewalAuthorization: &sessionv1.SessionRenewalAuthorization{
				Bearer:    strings.Repeat("g", 43),
				ExpiresAt: timestamppb.New(now.Add(24 * time.Hour)),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile.ActiveSession = &state.ActiveSession{
		RequestID:             requestID,
		PreviousSessionID:     profile.Secrets.SessionID,
		PreviousRenewalSHA256: state.CredentialDigest(profile.Secrets.Renewal),
		Operation:             active,
	}
	owners := &dashboardOwners{accountFixture: accounts}
	mux := http.NewServeMux()
	mux.Handle(billingpublicapiconnect.NewBillingUserServiceHandler(owners))
	mux.Handle(managementapiconnect.NewManagementDashboardServiceHandler(owners))
	httpClient := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.URL.Host != strings.TrimPrefix(profile.ControlOrigin, "https://") {
			t.Fatal("dashboard used an unexpected origin or credential class")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	dashboard, err := NewDashboard(httpClient, targets.announcements, targets.config)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := dashboard.List(t.Context(), profile)
	if err != nil || len(catalog.GetSpaces()) != 2 || catalog.GetPrimaryAccountId() != "account-b" || accounts.calls != 2 {
		t.Fatal("personal Account did not come from Billing", err)
	}
	if board, err := dashboard.Get(t.Context(), profile, "account-b"); err != nil || !board.GetCanEdit() {
		t.Fatal("renewed session could not read an editable board", err)
	}
	request := &ipc.ReplaceDashboardBoardRequest{
		AccountId: "account-b", ExpectedRevision: proto.Uint64(0),
		Layout: &ipc.DashboardLayout{Image: ipc.DashboardImage_DASHBOARD_IMAGE_RIBBON},
	}
	if _, err = dashboard.Replace(t.Context(), profile, request); !errors.Is(err, ErrDashboardConflict) || owners.mutations != 1 {
		t.Fatal("unknown or duplicated conflict", err, owners.mutations)
	}
	dashboard.config.Now = func() time.Time { return now.Add(time.Hour) }
	if _, err = dashboard.Replace(t.Context(), profile, request); !errors.Is(err, ErrSessionDenied) || owners.mutations != 1 {
		t.Fatal("expired session dispatched a mutation", err, owners.mutations)
	}
}
