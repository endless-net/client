package main

import (
	"context"
	"errors"
	"net/url"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/endless-net/client/internal/client"
)

func agentRPCSession(ctx context.Context, origin, token string) (*backend.GetSessionResponse, error) {
	parsed, err := url.Parse(origin)
	if err != nil || !isSecureOriginURL(parsed) || token == "" {
		return nil, errors.New("session requires secure origin and user authorization")
	}
	client := clientrpcconnect.NewSessionServiceClient(api.NewControlPlaneHTTPClient(15*time.Second, nil), origin,
		connect.WithReadMaxBytes(64<<10), connect.WithSendMaxBytes(4096))
	request := connect.NewRequest(&backend.GetSessionRequest{})
	request.Header().Set("Authorization", "Bearer "+token)
	response, err := client.GetSession(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("session response missing")
	}
	return response.Msg, nil
}

func agentRPCSessionRenewal() client.ClientRPCSessionRenewalProvider {
	return agentRPCSessionRenewalWithHTTPClient(api.NewControlPlaneHTTPClient(15*time.Second, nil))
}

func agentRPCSessionRenewalWithHTTPClient(httpClient connect.HTTPClient) client.ClientRPCSessionRenewalProvider {
	newClient := func(origin, authority string) (clientrpcconnect.SessionServiceClient, error) {
		parsed, err := url.Parse(origin)
		if err != nil || !isSecureOriginURL(parsed) || authority == "" {
			return nil, errors.New("session renewal requires secure origin and dedicated authorization")
		}
		return clientrpcconnect.NewSessionServiceClient(httpClient, origin,
			connect.WithReadMaxBytes(64<<10), connect.WithSendMaxBytes(4096)), nil
	}
	return client.ClientRPCSessionRenewalProvider{
		Renew: func(ctx context.Context, origin, authority string, input *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
			client, err := newClient(origin, authority)
			if err != nil {
				return nil, err
			}
			if err := api.ValidateSessionRenewalRequest(input); err != nil {
				return nil, err
			}
			request := connect.NewRequest(input)
			request.Header().Set("Authorization", "Bearer "+authority)
			response, err := client.RenewSession(ctx, request)
			if err != nil {
				return nil, err
			}
			if response == nil {
				return nil, errors.New("session renewal response missing")
			}
			return response.Msg, nil
		},
		Poll: func(ctx context.Context, origin, authority string, input *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
			client, err := newClient(origin, authority)
			if err != nil {
				return nil, err
			}
			if input == nil || input.OperationId == "" {
				return nil, errors.New("session renewal operation is required")
			}
			request := connect.NewRequest(input)
			request.Header().Set("Authorization", "Bearer "+authority)
			response, err := client.GetSessionRenewal(ctx, request)
			if err != nil {
				return nil, err
			}
			if response == nil {
				return nil, errors.New("session polling response missing")
			}
			return response.Msg, nil
		},
	}
}
