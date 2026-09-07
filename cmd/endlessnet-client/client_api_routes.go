package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/endless-net/client/internal/client"
)

const userRoutePageSize = 200

type userRouteAPI struct {
	client    clientrpcconnect.UserServiceClient
	token     string
	accountID string
}

func loadUserRouteAPI(configPath string) (*userRouteAPI, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("not logged in; run endlessnet-client login first")
	}
	controlURLs := cfg.ControlURLs()
	if len(controlURLs) == 0 {
		return nil, errors.New("control URL is required; run endlessnet-client login")
	}
	controlURL := strings.TrimRight(strings.TrimSpace(controlURLs[0]), "/")
	parsed, err := url.Parse(controlURL)
	if err != nil || !isSecureOriginURL(parsed) {
		return nil, errors.New("control URL must be a secure origin; run endlessnet-client login")
	}
	return &userRouteAPI{
		client: clientrpcconnect.NewUserServiceClient(
			clientapi.NewControlPlaneHTTPClient(15*time.Second, nil),
			controlURL,
		),
		token:     cfg.Token,
		accountID: strings.TrimSpace(cfg.ActiveAccountID),
	}, nil
}

func (api *userRouteAPI) ListAdvertisedRoutes(ctx context.Context, networkRef string) ([]*clientrpc.AdvertisedRoute, error) {
	networkID, err := api.resolveNetworkID(ctx, networkRef)
	if err != nil {
		return nil, err
	}
	routes := make([]*clientrpc.AdvertisedRoute, 0)
	seen := make(map[string]bool)
	token := ""
	for {
		request := connect.NewRequest(&clientrpc.ListAdvertisedRoutesRequest{
			NetworkId: networkID,
			Page:      &clientrpc.PageRequest{PageSize: userRoutePageSize, PageToken: token},
		})
		api.authorize(request.Header())
		response, err := api.client.ListAdvertisedRoutes(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("list advertised routes: %w", err)
		}
		routes = append(routes, response.Msg.GetRoutes()...)
		token = response.Msg.GetPage().GetNextPageToken()
		if token == "" {
			return routes, nil
		}
		if seen[token] {
			return nil, errors.New("list advertised routes returned a repeated page token")
		}
		seen[token] = true
	}
}

func (api *userRouteAPI) ListBillingPlans(ctx context.Context) ([]*clientrpc.Plan, error) {
	request := connect.NewRequest(&clientrpc.ListBillingPlansRequest{})
	api.authorize(request.Header())
	response, err := api.client.ListBillingPlans(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("list billing plans: %w", err)
	}
	return response.Msg.GetPlans(), nil
}

func (api *userRouteAPI) ListBillingAccounts(ctx context.Context) ([]*clientrpc.Account, error) {
	return api.fetchAccounts(ctx)
}

func (api *userRouteAPI) GetBillingSubscription(ctx context.Context, accountID string) (*clientrpc.Subscription, error) {
	request := connect.NewRequest(&clientrpc.GetBillingSubscriptionRequest{AccountId: strings.TrimSpace(accountID)})
	api.authorize(request.Header())
	response, err := api.client.GetBillingSubscription(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get billing subscription: %w", err)
	}
	if response.Msg.GetSubscription() == nil {
		return nil, errors.New("get billing subscription returned an incomplete response")
	}
	return response.Msg.GetSubscription(), nil
}

func (api *userRouteAPI) GetBillingUsage(ctx context.Context, accountID string) (*clientrpc.Usage, error) {
	request := connect.NewRequest(&clientrpc.GetBillingUsageRequest{AccountId: strings.TrimSpace(accountID)})
	api.authorize(request.Header())
	response, err := api.client.GetBillingUsage(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("get billing usage: %w", err)
	}
	if response.Msg.GetUsage() == nil {
		return nil, errors.New("get billing usage returned an incomplete response")
	}
	return response.Msg.GetUsage(), nil
}

func (api *userRouteAPI) CreateBillingCheckout(ctx context.Context, accountID, planID, period, idempotencyKey string) (*clientrpc.Checkout, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			return nil, fmt.Errorf("generate checkout idempotency key: %w", err)
		}
		key = base64.RawURLEncoding.EncodeToString(random)
	}
	request := connect.NewRequest(&clientrpc.CreateBillingCheckoutRequest{
		AccountId: strings.TrimSpace(accountID), PlanId: strings.TrimSpace(planID), BillingPeriod: strings.TrimSpace(period),
		Operation: &clientrpc.OperationMetadata{IdempotencyKey: key},
	})
	api.authorize(request.Header())
	response, err := api.client.CreateBillingCheckout(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("create billing checkout: %w", err)
	}
	if response.Msg.GetCheckout() == nil {
		return nil, errors.New("create billing checkout returned an incomplete response")
	}
	return response.Msg.GetCheckout(), nil
}

func (api *userRouteAPI) ListBillingInvoices(ctx context.Context, accountID string) ([]*clientrpc.Invoice, error) {
	items := make([]*clientrpc.Invoice, 0)
	err := api.readPages(ctx, func(token string) (string, error) {
		request := connect.NewRequest(&clientrpc.ListBillingInvoicesRequest{AccountId: strings.TrimSpace(accountID), Page: &clientrpc.PageRequest{PageSize: userRoutePageSize, PageToken: token}})
		api.authorize(request.Header())
		response, err := api.client.ListBillingInvoices(ctx, request)
		if err != nil {
			return "", fmt.Errorf("list billing invoices: %w", err)
		}
		items = append(items, response.Msg.GetInvoices()...)
		return response.Msg.GetPage().GetNextPageToken(), nil
	})
	return items, err
}

func (api *userRouteAPI) resolveNetworkID(ctx context.Context, networkRef string) (string, error) {
	ref := strings.TrimSpace(networkRef)
	if ref == "" {
		return "", errors.New("network name or ID is required")
	}
	accounts, err := api.listAccounts(ctx)
	if err != nil {
		return "", err
	}
	matches := make([]*clientrpc.Network, 0, 1)
	for _, account := range accounts {
		networks, listErr := api.listNetworks(ctx, account.GetAccountId())
		if listErr != nil {
			return "", listErr
		}
		for _, network := range networks {
			if network.GetNetworkId() == ref {
				return network.GetNetworkId(), nil
			}
			if network.GetName() == ref {
				matches = append(matches, network)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0].GetNetworkId(), nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("network name %q is ambiguous; use a network ID", ref)
	}
	return "", fmt.Errorf("network %q was not found", ref)
}

func (api *userRouteAPI) listAccounts(ctx context.Context) ([]*clientrpc.Account, error) {
	if api.accountID != "" {
		return []*clientrpc.Account{{AccountId: api.accountID}}, nil
	}
	return api.fetchAccounts(ctx)
}

func (api *userRouteAPI) fetchAccounts(ctx context.Context) ([]*clientrpc.Account, error) {
	items := make([]*clientrpc.Account, 0)
	err := api.readPages(ctx, func(token string) (string, error) {
		request := connect.NewRequest(&clientrpc.ListAccountsRequest{Page: &clientrpc.PageRequest{PageSize: userRoutePageSize, PageToken: token}})
		api.authorize(request.Header())
		response, err := api.client.ListAccounts(ctx, request)
		if err != nil {
			return "", fmt.Errorf("list accounts: %w", err)
		}
		items = append(items, response.Msg.GetAccounts()...)
		return response.Msg.GetPage().GetNextPageToken(), nil
	})
	return items, err
}

func (api *userRouteAPI) listNetworks(ctx context.Context, accountID string) ([]*clientrpc.Network, error) {
	items := make([]*clientrpc.Network, 0)
	err := api.readPages(ctx, func(token string) (string, error) {
		request := connect.NewRequest(&clientrpc.ListNetworksRequest{AccountId: accountID, Page: &clientrpc.PageRequest{PageSize: userRoutePageSize, PageToken: token}})
		api.authorize(request.Header())
		response, err := api.client.ListNetworks(ctx, request)
		if err != nil {
			return "", fmt.Errorf("list networks: %w", err)
		}
		items = append(items, response.Msg.GetNetworks()...)
		return response.Msg.GetPage().GetNextPageToken(), nil
	})
	return items, err
}

func (api *userRouteAPI) readPages(ctx context.Context, read func(string) (string, error)) error {
	seen := make(map[string]bool)
	token := ""
	for {
		next, err := read(token)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if seen[next] {
			return errors.New("client API returned a repeated page token")
		}
		seen[next] = true
		token = next
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (api *userRouteAPI) authorize(header interface{ Set(string, string) }) {
	header.Set("Authorization", "Bearer "+strings.TrimSpace(api.token))
}
