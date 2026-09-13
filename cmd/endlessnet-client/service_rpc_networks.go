package main

import (
	"context"
	"errors"
	"net/url"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func agentRPCNetworks(ctx context.Context, input client.ClientRPCNetworksInput) ([]*ipc.Network, error) {
	origin, err := url.Parse(input.ControlOrigin)
	if err != nil || !isSecureOriginURL(origin) || input.AccountID == "" || input.SessionToken == "" {
		return nil, errors.New("network catalog requires profile authorization")
	}
	api := clientrpcconnect.NewUserServiceClient(clientapi.NewControlPlaneHTTPClient(15*time.Second, nil), input.ControlOrigin)
	return readAgentRPCNetworks(ctx, input, api.ListNetworks)
}

func readAgentRPCNetworks(ctx context.Context, input client.ClientRPCNetworksInput, read func(context.Context, *connect.Request[clientrpc.ListNetworksRequest]) (*connect.Response[clientrpc.ListNetworksResponse], error)) ([]*ipc.Network, error) {
	var items []*ipc.Network
	token := ""
	seen := map[string]bool{}
	for page := 0; page < 5; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(token) > 2048 || seen[token] {
			return nil, errors.New("invalid network catalog pagination")
		}
		seen[token] = true
		request := connect.NewRequest(&clientrpc.ListNetworksRequest{AccountId: input.AccountID, Page: &clientrpc.PageRequest{PageSize: 200, PageToken: token}})
		request.Header().Set("Authorization", "Bearer "+input.SessionToken)
		response, err := read(ctx, request)
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if response == nil || response.Msg == nil || response.Msg.Page == nil || len(response.Msg.Networks) > 200 {
			return nil, errors.New("invalid network catalog response")
		}
		for _, network := range response.Msg.Networks {
			if network == nil || network.NetworkId == "" {
				return nil, errors.New("invalid network catalog entry")
			}
			items = append(items, &ipc.Network{Id: network.NetworkId, Name: network.Name, AccountId: input.AccountID})
		}
		token = response.Msg.Page.NextPageToken
		if token == "" {
			return items, nil
		}
		if len(response.Msg.Networks) == 0 {
			return nil, errors.New("empty network continuation")
		}
	}
	return nil, errors.New("network catalog exceeds collection limit")
}
