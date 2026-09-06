package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	managementapi "github.com/endless-net/management/managementapi/v1"
	"github.com/endless-net/management/managementapi/v1/managementapiconnect"
)

type managementRouteHandler struct {
	managementapiconnect.UnimplementedManagementAdminServiceHandler
	t               *testing.T
	listRouteCalls  int
	approvalRequest *managementapi.SetAdvertisedRouteApprovalRequest
}

func (handler *managementRouteHandler) requireAuthorization(request interface{ Header() http.Header }) {
	handler.t.Helper()
	if got := request.Header().Get("Authorization"); got != "Bearer session-token" {
		handler.t.Fatalf("Authorization = %q", got)
	}
}

func (handler *managementRouteHandler) ListAccounts(_ context.Context, request *connect.Request[managementapi.ListAccountsRequest]) (*connect.Response[managementapi.ListAccountsResponse], error) {
	handler.requireAuthorization(request)
	return connect.NewResponse(&managementapi.ListAccountsResponse{Accounts: []*managementapi.Account{{AccountId: "account"}}, Page: &managementapi.PageResponse{}}), nil
}

func (handler *managementRouteHandler) ListNetworks(_ context.Context, request *connect.Request[managementapi.ListNetworksRequest]) (*connect.Response[managementapi.ListNetworksResponse], error) {
	handler.requireAuthorization(request)
	if request.Msg.GetAccountId() != "account" {
		handler.t.Fatalf("account ID = %q", request.Msg.GetAccountId())
	}
	return connect.NewResponse(&managementapi.ListNetworksResponse{Networks: []*managementapi.Network{{NetworkId: "network-id", Name: "default"}}, Page: &managementapi.PageResponse{}}), nil
}

func (handler *managementRouteHandler) ListAdvertisedRoutes(_ context.Context, request *connect.Request[managementapi.ListAdvertisedRoutesRequest]) (*connect.Response[managementapi.ListAdvertisedRoutesResponse], error) {
	handler.requireAuthorization(request)
	if request.Msg.GetNetworkId() != "network-id" {
		handler.t.Fatalf("network ID = %q", request.Msg.GetNetworkId())
	}
	handler.listRouteCalls++
	if handler.listRouteCalls == 1 {
		return connect.NewResponse(&managementapi.ListAdvertisedRoutesResponse{
			Routes: []*managementapi.AdvertisedRoute{{NetworkId: "network-id", NodeId: "other", Cidr: "198.51.100.0/24", Version: 41}},
			Page:   &managementapi.PageResponse{NextPageToken: "next"},
		}), nil
	}
	if request.Msg.GetPage().GetPageToken() != "next" {
		handler.t.Fatalf("route page token = %q", request.Msg.GetPage().GetPageToken())
	}
	return connect.NewResponse(&managementapi.ListAdvertisedRoutesResponse{
		Routes: []*managementapi.AdvertisedRoute{{NetworkId: "network-id", NodeId: "router", Hostname: "router-a", Cidr: "192.0.2.0/24", Version: 42}},
		Page:   &managementapi.PageResponse{},
	}), nil
}

func (handler *managementRouteHandler) SetAdvertisedRouteApproval(_ context.Context, request *connect.Request[managementapi.SetAdvertisedRouteApprovalRequest]) (*connect.Response[managementapi.SetAdvertisedRouteApprovalResponse], error) {
	handler.requireAuthorization(request)
	handler.approvalRequest = request.Msg
	return connect.NewResponse(&managementapi.SetAdvertisedRouteApprovalResponse{
		Route:   &managementapi.AdvertisedRoute{NetworkId: request.Msg.GetNetworkId(), NodeId: request.Msg.GetNodeId(), Hostname: "router-a", Cidr: request.Msg.GetCidr(), Approved: request.Msg.GetApproved(), Version: 43},
		Network: &managementapi.Network{NetworkId: request.Msg.GetNetworkId(), Revision: 43},
	}), nil
}

func TestManagementRouteAPIUsesConnectPaginationAndDisplayedRevision(t *testing.T) {
	handler := &managementRouteHandler{t: t}
	path, connectHandler := managementapiconnect.NewManagementAdminServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, connectHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	api := &managementRouteAPI{
		client: managementapiconnect.NewManagementAdminServiceClient(server.Client(), server.URL),
		token:  "session-token",
	}
	response, err := api.SetAdvertisedRouteApproval(t.Context(), "default", "router", "192.0.2.0/24", true)
	if err != nil {
		t.Fatal(err)
	}
	if handler.listRouteCalls != 2 {
		t.Fatalf("route list calls = %d", handler.listRouteCalls)
	}
	request := handler.approvalRequest
	if request == nil || request.GetNetworkId() != "network-id" || request.GetNodeId() != "router" || request.GetCidr() != "192.0.2.0/24" || !request.GetApproved() || request.GetPrecondition().GetExpectedVersion() != 42 {
		t.Fatalf("approval request = %+v", request)
	}
	if response.GetNetwork().GetRevision() != 43 || !response.GetRoute().GetApproved() {
		t.Fatalf("approval response = %+v", response)
	}
}
