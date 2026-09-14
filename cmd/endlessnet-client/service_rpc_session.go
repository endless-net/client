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
