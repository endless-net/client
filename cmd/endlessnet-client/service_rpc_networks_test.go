package main

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/internal/client"
)

func TestAgentRPCNetworksUsesAccountScopedTypedPages(t *testing.T) {
	calls := 0
	items, err := readAgentRPCNetworks(t.Context(), client.ClientRPCNetworksInput{AccountID: "account", SessionToken: "synthetic-session"}, func(_ context.Context, request *connect.Request[clientrpc.ListNetworksRequest]) (*connect.Response[clientrpc.ListNetworksResponse], error) {
		calls++
		if request.Msg.AccountId != "account" || request.Msg.Page.PageSize != 200 || request.Header().Get("Authorization") != "Bearer synthetic-session" {
			t.Fatal("wrong catalog scope")
		}
		response := &clientrpc.ListNetworksResponse{Networks: []*clientrpc.Network{{NetworkId: "a", Name: "name"}}, Page: &clientrpc.PageResponse{}}
		if calls == 1 {
			response.Page.NextPageToken = "next"
		} else if request.Msg.Page.PageToken != "next" {
			t.Fatal("lost page token")
		}
		return connect.NewResponse(response), nil
	})
	if err != nil || len(items) != 2 || items[0].AccountId != "account" || calls != 2 {
		t.Fatal("missing catalog pages", err)
	}
}

func TestAgentRPCNetworksRejectsInvalidPagesWithoutRetry(t *testing.T) {
	for _, mode := range []string{"nil", "missing-page", "loop", "empty", "limit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := readAgentRPCNetworks(ctx, client.ClientRPCNetworksInput{AccountID: "account", SessionToken: "synthetic"}, func(context.Context, *connect.Request[clientrpc.ListNetworksRequest]) (*connect.Response[clientrpc.ListNetworksResponse], error) {
				calls++
				response := &clientrpc.ListNetworksResponse{Page: &clientrpc.PageResponse{NextPageToken: "next"}}
				switch mode {
				case "nil":
					return nil, nil
				case "missing-page":
					response.Page = nil
				case "loop":
					response.Networks = []*clientrpc.Network{{NetworkId: "a"}}
				case "limit":
					response.Networks = make([]*clientrpc.Network, 201)
				case "cancel":
					cancel()
				}
				return connect.NewResponse(response), nil
			})
			if err == nil || calls > 2 {
				t.Fatal("invalid pagination was accepted/retried", err, calls)
			}
		})
	}
}
