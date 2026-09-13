package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/endless-net/client/internal/client"
)

type rpcNetworkCatalogBackend struct {
	clientrpcconnect.UnimplementedUserServiceHandler
	t       *testing.T
	calls   int
	failure connect.Code
}

func (b *rpcNetworkCatalogBackend) ListNetworks(_ context.Context, request *connect.Request[clientrpc.ListNetworksRequest]) (*connect.Response[clientrpc.ListNetworksResponse], error) {
	b.calls++
	if request.Msg.AccountId != "profile-account" || request.Msg.Page.PageSize != 200 || request.Header().Get("Authorization") != "Bearer synthetic-session" {
		b.t.Error("typed transport lost account, pagination or user authorization")
	}
	if request.Header().Get("X-Endlessnet-Node-Credential") != "" {
		b.t.Error("node credential used for user catalog")
	}
	if b.calls == 1 {
		if request.Msg.Page.PageToken != "" {
			b.t.Error("first page has a cursor")
		}
		return connect.NewResponse(&clientrpc.ListNetworksResponse{Networks: []*clientrpc.Network{{NetworkId: "first", Name: "First"}}, Page: &clientrpc.PageResponse{NextPageToken: "opaque-next"}}), nil
	}
	if request.Msg.Page.PageToken != "opaque-next" {
		b.t.Error("typed transport lost opaque continuation")
	}
	if b.failure != 0 {
		return nil, connect.NewError(b.failure, errors.New("private upstream diagnostic"))
	}
	return connect.NewResponse(&clientrpc.ListNetworksResponse{Networks: []*clientrpc.Network{{NetworkId: "second", Name: "Second"}}, Page: &clientrpc.PageResponse{}}), nil
}

func TestAgentNetworkCatalogTypedBackendTransport(t *testing.T) {
	for _, code := range []connect.Code{0, connect.CodeUnauthenticated, connect.CodePermissionDenied, connect.CodeUnavailable} {
		t.Run(code.String(), func(t *testing.T) {
			backend := &rpcNetworkCatalogBackend{t: t, failure: code}
			_, handler := clientrpcconnect.NewUserServiceHandler(backend)
			server := httptest.NewServer(handler)
			defer server.Close()
			consumer := clientrpcconnect.NewUserServiceClient(server.Client(), server.URL)
			items, err := readAgentRPCNetworks(t.Context(), client.ClientRPCNetworksInput{AccountID: "profile-account", SessionToken: "synthetic-session"}, consumer.ListNetworks)
			if backend.calls != 2 {
				t.Fatal("catalog repeated or omitted backend calls", backend.calls)
			}
			if code != 0 {
				if items != nil || connect.CodeOf(err) != code {
					t.Fatal("partial catalog or lost backend error category", err)
				}
				return
			}
			if err != nil || len(items) != 2 || items[0].Id != "first" || items[1].Id != "second" || items[0].AccountId != "profile-account" {
				t.Fatal("typed catalog did not survive transport", err)
			}
		})
	}
}
