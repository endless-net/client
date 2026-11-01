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
	billingapi "github.com/endless-net/billing/api/public/v1"
	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	"github.com/endless-net/client/internal/state"
	coordinatorapi "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
)

type accountFixture struct {
	billingpublicapiconnect.UnimplementedBillingUserServiceHandler
	t     *testing.T
	calls int
	cycle bool
}

func (f *accountFixture) ListAccounts(ctx context.Context, r *connect.Request[billingapi.ListAccountsRequest]) (*connect.Response[billingapi.ListAccountsResponse], error) {
	f.calls++
	if r.Header().Get("Authorization") != "Bearer "+strings.Repeat("a", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("secret-user-proof"))
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 30*time.Second {
		f.t.Fatal("unbounded target query")
	}
	if r.Msg.Page.PageSize != 1 {
		f.t.Fatal("upstream page request changed")
	}
	if f.cycle {
		return connect.NewResponse(&billingapi.ListAccountsResponse{Page: &billingapi.PageResponse{NextPageToken: "cycle"}}), nil
	}
	if r.Msg.Page.PageToken == "" {
		return connect.NewResponse(&billingapi.ListAccountsResponse{Accounts: []*billingapi.Account{{AccountId: "account-a", Name: "A"}}, Page: &billingapi.PageResponse{NextPageToken: "next-account"}}), nil
	}
	if r.Msg.Page.PageToken != "next-account" {
		f.t.Fatal("account continuation token not opaque")
	}
	return connect.NewResponse(&billingapi.ListAccountsResponse{Accounts: []*billingapi.Account{{AccountId: "account-b", Name: "B"}}}), nil
}

type networkFixture struct {
	publicqueryv1connect.UnimplementedCoordinatorUserServiceHandler
	t           *testing.T
	calls       int
	deny        bool
	denyAccount string
}

func (f *networkFixture) ListNetworks(_ context.Context, r *connect.Request[coordinatorapi.ListNetworksRequest]) (*connect.Response[coordinatorapi.ListNetworksResponse], error) {
	f.calls++
	if r.Header().Get("Authorization") != "Bearer "+strings.Repeat("a", 43) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("secret-user-proof"))
	}
	if f.deny || r.Msg.AccountId == f.denyAccount {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("secret-tenant-message"))
	}
	switch r.Msg.AccountId {
	case "account-a":
		if r.Msg.Page.PageToken == "" {
			return connect.NewResponse(&coordinatorapi.ListNetworksResponse{Networks: []*coordinatorapi.Network{{NetworkId: "network-a", Name: "A"}}, Page: &coordinatorapi.PageResponse{NextPageToken: "next-network"}}), nil
		}
		if r.Msg.Page.PageToken != "next-network" {
			f.t.Fatal("network continuation token not retained")
		}
		return connect.NewResponse(&coordinatorapi.ListNetworksResponse{Networks: []*coordinatorapi.Network{{NetworkId: "network-b", Name: "B"}}}), nil
	case "account-b":
		return connect.NewResponse(&coordinatorapi.ListNetworksResponse{Networks: []*coordinatorapi.Network{{NetworkId: "network-c", Name: "C"}}}), nil
	default:
		f.t.Fatal("foreign account queried")
		return nil, ErrTargetDenied
	}
}

func targetFixture(t *testing.T) (*Targets, state.Profile, *accountFixture, *networkFixture) {
	t.Helper()
	e, _, clock := loginFixture(t)
	ctx := context.Background()
	s, err := e.store.Load(ctx)
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
	p.Secrets.SessionID = "native-session"
	p.Secrets.SessionExpiresAt = clock.Add(time.Hour)
	accounts := &accountFixture{t: t}
	networks := &networkFixture{t: t}
	mux := http.NewServeMux()
	path, h := billingpublicapiconnect.NewBillingUserServiceHandler(accounts)
	mux.Handle(path, h)
	path, h = publicqueryv1connect.NewCoordinatorUserServiceHandler(networks)
	mux.Handle(path, h)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" {
			t.Fatal("browser cookie used for native target query")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		v := w.Result()
		v.Request = r
		return v, nil
	})}
	q, err := NewTargets(client, e.native.announcements, TargetConfig{MaxCalls: 10, MaxAccounts: 4, MaxTargets: 8, MaxResponseBytes: 1 << 20, UpstreamPageSize: 1, Now: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	return q, p, accounts, networks
}

func TestAuthorizedTargetSDKJoinAndOpaquePagination(t *testing.T) {
	q, p, accounts, networks := targetFixture(t)
	result, err := q.Fetch(context.Background(), p)
	if err != nil || len(result) != 3 || result[0].AccountId != "account-a" || result[1].NetworkId != "network-b" || result[2].AccountId != "account-b" || accounts.calls != 2 || networks.calls != 3 {
		t.Fatal("bounded scoped join incomplete", err)
	}
	networks.denyAccount = "account-b"
	if result, err = q.Fetch(context.Background(), p); !errors.Is(err, ErrTargetDenied) || result != nil {
		t.Fatal("revoked authority returned partial targets", err)
	}
	networks.denyAccount = ""
	p.Secrets.Access = strings.Repeat("p", 43)
	if _, err = q.Fetch(context.Background(), p); !errors.Is(err, ErrSessionDenied) {
		t.Fatal("poll proof used as native session", err)
	}
}

func TestTargetQueryLimitsAndRepeatedTokenFailClosed(t *testing.T) {
	q, p, accounts, _ := targetFixture(t)
	q.config.MaxCalls = 1
	if result, err := q.Fetch(context.Background(), p); !errors.Is(err, ErrQueryLimit) || result != nil {
		t.Fatal("call budget silently truncated catalog", err)
	}
	q.config.MaxCalls = 10
	q.config.MaxTargets = 1
	if result, err := q.Fetch(context.Background(), p); !errors.Is(err, ErrQueryLimit) || result != nil {
		t.Fatal("target limit silently truncated catalog", err)
	}
	q.config.MaxTargets = 8
	accounts.cycle = true
	if _, err := q.Fetch(context.Background(), p); !errors.Is(err, ErrTargetProtocol) {
		t.Fatal("pagination cycle not rejected", err)
	}
}
