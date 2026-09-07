package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
)

type userRouteHandler struct {
	clientrpcconnect.UnimplementedUserServiceHandler
	t              *testing.T
	listRouteCalls int

	invoiceCalls    int
	checkoutRequest *clientrpc.CreateBillingCheckoutRequest
}

func (handler *userRouteHandler) ListBillingPlans(_ context.Context, request *connect.Request[clientrpc.ListBillingPlansRequest]) (*connect.Response[clientrpc.ListBillingPlansResponse], error) {
	handler.requireAuthorization(request)
	return connect.NewResponse(&clientrpc.ListBillingPlansResponse{Plans: []*clientrpc.Plan{{PlanId: "team", MonthlyPrice: uint64Pointer(490000), NetworkLimit: 10, NodeLimit: 200, UserLimit: 50}}}), nil
}

func (handler *userRouteHandler) GetBillingSubscription(_ context.Context, request *connect.Request[clientrpc.GetBillingSubscriptionRequest]) (*connect.Response[clientrpc.GetBillingSubscriptionResponse], error) {
	handler.requireAuthorization(request)
	return connect.NewResponse(&clientrpc.GetBillingSubscriptionResponse{Subscription: &clientrpc.Subscription{AccountId: request.Msg.GetAccountId(), PlanId: "team", Status: "active"}}), nil
}

func (handler *userRouteHandler) GetBillingUsage(_ context.Context, request *connect.Request[clientrpc.GetBillingUsageRequest]) (*connect.Response[clientrpc.GetBillingUsageResponse], error) {
	handler.requireAuthorization(request)
	return connect.NewResponse(&clientrpc.GetBillingUsageResponse{Usage: &clientrpc.Usage{AccountId: request.Msg.GetAccountId(), Networks: 2, Nodes: 7, Users: 3, NetworkLimit: 10, NodeLimit: 200, UserLimit: 50}}), nil
}

func (handler *userRouteHandler) CreateBillingCheckout(_ context.Context, request *connect.Request[clientrpc.CreateBillingCheckoutRequest]) (*connect.Response[clientrpc.CreateBillingCheckoutResponse], error) {
	handler.requireAuthorization(request)
	handler.checkoutRequest = request.Msg
	return connect.NewResponse(&clientrpc.CreateBillingCheckoutResponse{Checkout: &clientrpc.Checkout{CheckoutId: "checkout", AccountId: request.Msg.GetAccountId(), Status: "pending"}}), nil
}

func (handler *userRouteHandler) ListBillingInvoices(_ context.Context, request *connect.Request[clientrpc.ListBillingInvoicesRequest]) (*connect.Response[clientrpc.ListBillingInvoicesResponse], error) {
	handler.requireAuthorization(request)
	handler.invoiceCalls++
	if handler.invoiceCalls == 1 {
		return connect.NewResponse(&clientrpc.ListBillingInvoicesResponse{Invoices: []*clientrpc.Invoice{{InvoiceId: "invoice-2"}}, Page: &clientrpc.PageResponse{NextPageToken: "invoice-next"}}), nil
	}
	if request.Msg.GetPage().GetPageToken() != "invoice-next" {
		handler.t.Fatalf("invoice page token = %q", request.Msg.GetPage().GetPageToken())
	}
	return connect.NewResponse(&clientrpc.ListBillingInvoicesResponse{Invoices: []*clientrpc.Invoice{{InvoiceId: "invoice-1"}}, Page: &clientrpc.PageResponse{}}), nil
}

func (handler *userRouteHandler) requireAuthorization(request interface{ Header() http.Header }) {
	handler.t.Helper()
	if got := request.Header().Get("Authorization"); got != "Bearer session-token" {
		handler.t.Fatalf("Authorization = %q", got)
	}
}

func (handler *userRouteHandler) ListAccounts(_ context.Context, request *connect.Request[clientrpc.ListAccountsRequest]) (*connect.Response[clientrpc.ListAccountsResponse], error) {
	handler.requireAuthorization(request)
	return connect.NewResponse(&clientrpc.ListAccountsResponse{Accounts: []*clientrpc.Account{{AccountId: "account"}}, Page: &clientrpc.PageResponse{}}), nil
}

func (handler *userRouteHandler) ListNetworks(_ context.Context, request *connect.Request[clientrpc.ListNetworksRequest]) (*connect.Response[clientrpc.ListNetworksResponse], error) {
	handler.requireAuthorization(request)
	if request.Msg.GetAccountId() != "account" {
		handler.t.Fatalf("account ID = %q", request.Msg.GetAccountId())
	}
	return connect.NewResponse(&clientrpc.ListNetworksResponse{Networks: []*clientrpc.Network{{NetworkId: "network-id", Name: "default"}}, Page: &clientrpc.PageResponse{}}), nil
}

func (handler *userRouteHandler) ListAdvertisedRoutes(_ context.Context, request *connect.Request[clientrpc.ListAdvertisedRoutesRequest]) (*connect.Response[clientrpc.ListAdvertisedRoutesResponse], error) {
	handler.requireAuthorization(request)
	if request.Msg.GetNetworkId() != "network-id" {
		handler.t.Fatalf("network ID = %q", request.Msg.GetNetworkId())
	}
	handler.listRouteCalls++
	if handler.listRouteCalls == 1 {
		return connect.NewResponse(&clientrpc.ListAdvertisedRoutesResponse{
			Routes: []*clientrpc.AdvertisedRoute{{NetworkId: "network-id", NodeId: "other", Cidr: "198.51.100.0/24"}},
			Page:   &clientrpc.PageResponse{NextPageToken: "next"},
		}), nil
	}
	if request.Msg.GetPage().GetPageToken() != "next" {
		handler.t.Fatalf("route page token = %q", request.Msg.GetPage().GetPageToken())
	}
	return connect.NewResponse(&clientrpc.ListAdvertisedRoutesResponse{
		Routes: []*clientrpc.AdvertisedRoute{{NetworkId: "network-id", NodeId: "router", Hostname: "router-a", Cidr: "192.0.2.0/24"}},
		Page:   &clientrpc.PageResponse{},
	}), nil
}

func TestUserRouteAPIUsesClientAPIConnectPagination(t *testing.T) {
	handler := &userRouteHandler{t: t}
	path, connectHandler := clientrpcconnect.NewUserServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	api := &userRouteAPI{
		client: clientrpcconnect.NewUserServiceClient(server.Client(), server.URL),
		token:  "session-token",
	}
	routes, err := api.ListAdvertisedRoutes(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if handler.listRouteCalls != 2 || len(routes) != 2 || routes[1].GetNodeId() != "router" {
		t.Fatalf("routes = %+v, calls = %d", routes, handler.listRouteCalls)
	}

}

func TestUserBillingAPIUsesTypedConnectSurface(t *testing.T) {
	handler := &userRouteHandler{t: t}
	path, connectHandler := clientrpcconnect.NewUserServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	api := &userRouteAPI{client: clientrpcconnect.NewUserServiceClient(server.Client(), server.URL), token: "session-token", accountID: "previous-active-account"}

	accounts, err := api.ListBillingAccounts(t.Context())
	if err != nil || len(accounts) != 1 || accounts[0].GetAccountId() != "account" {
		t.Fatalf("accounts = %+v, err = %v", accounts, err)
	}
	plans, err := api.ListBillingPlans(t.Context())
	if err != nil || len(plans) != 1 || plans[0].GetUserLimit() != 50 {
		t.Fatalf("plans = %+v, err = %v", plans, err)
	}
	usage, err := api.GetBillingUsage(t.Context(), "account")
	if err != nil || usage.GetUsers() != 3 || usage.GetNodeLimit() != 200 {
		t.Fatalf("usage = %+v, err = %v", usage, err)
	}
	checkout, err := api.CreateBillingCheckout(t.Context(), "account", "team", "monthly", "checkout-retry-key")
	if err != nil || checkout.GetCheckoutId() != "checkout" {
		t.Fatalf("checkout = %+v, err = %v", checkout, err)
	}
	if request := handler.checkoutRequest; request == nil || request.GetPlanId() != "team" || request.GetOperation().GetIdempotencyKey() != "checkout-retry-key" {
		t.Fatalf("checkout request = %+v", request)
	}
	invoices, err := api.ListBillingInvoices(t.Context(), "account")
	if err != nil || len(invoices) != 2 || handler.invoiceCalls != 2 {
		t.Fatalf("invoices = %+v, calls = %d, err = %v", invoices, handler.invoiceCalls, err)
	}
}

func uint64Pointer(value uint64) *uint64 { return &value }

func TestClientRejectsAdministrativeRouteCommands(t *testing.T) {
	for _, command := range []string{"approve-route", "revoke-route"} {
		if err := cmdNetwork([]string{command}); err == nil || !strings.Contains(err.Error(), "unknown network command") {
			t.Fatalf("%s: %v", command, err)
		}
	}
}

func TestUserAPIRejectsRepeatedPageTokens(t *testing.T) {
	calls := 0
	err := (&userRouteAPI{}).readPages(t.Context(), func(string) (string, error) { calls++; return "same-page", nil })
	if err == nil || calls != 2 {
		t.Fatalf("pagination calls=%d err=%v", calls, err)
	}
}
