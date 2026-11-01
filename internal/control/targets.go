package control

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"
	billingapi "github.com/endless-net/billing/api/public/v1"
	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinatorapi "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
)

var ErrQueryLimit = errors.New("authorized target query budget exhausted")
var ErrTargetDenied = errors.New("target access denied")
var ErrSessionDenied = errors.New("native session unavailable")
var ErrTargetProtocol = errors.New("target query response invalid")

type TargetConfig struct {
	MaxCalls, MaxAccounts, MaxTargets, MaxResponseBytes int
	UpstreamPageSize                                    uint32
	Now                                                 func() time.Time
}
type Targets struct {
	http          *http.Client
	announcements Announcements
	config        TargetConfig
}

func NewTargets(client *http.Client, announcements Announcements, config TargetConfig) (*Targets, error) {
	if announcements == nil || config.MaxCalls <= 0 || config.MaxAccounts <= 0 || config.MaxTargets <= 0 || config.MaxResponseBytes <= 0 || config.UpstreamPageSize == 0 || config.UpstreamPageSize > 500 {
		return nil, ErrTargetProtocol
	}
	b, err := NewBootstrap(client, int64(config.MaxResponseBytes), config.Now)
	if err != nil {
		return nil, err
	}
	config.Now = b.now
	return &Targets{http: b.client, announcements: announcements, config: config}, nil
}

func (q *Targets) before(ctx context.Context, p state.Profile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Attempt == nil || (p.Attempt.Phase != enrollment.ChoosingContext && p.Attempt.Phase != enrollment.Registering) || p.Attempt.Uncertain || !q.config.Now().Before(p.Attempt.Deadline) || p.Secrets.Access == "" || !q.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return ErrSessionDenied
	}
	a, err := q.announcements.Fetch(ctx, p.ControlOrigin)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err != nil || a.Validate(q.config.Now()) != nil || a.Origin != p.ControlOrigin || a.KeyID != p.Trust.KeyID || a.ID != p.Trust.AnnouncementID || !bytes.Equal(a.Bundle, p.Trust.Bundle) {
		return ErrBootstrap
	}
	return nil
}

// Fetch returns a complete, bounded caller-visible selection catalog. It is not
// a node.enroll grant or a snapshot-isolation guarantee. Every invocation/page
// reauthorizes at producers; exceeding budgets fails instead of truncating.
func (q *Targets) Fetch(ctx context.Context, p state.Profile) ([]*clientv0.EnrollmentTarget, error) {
	if p.Attempt == nil {
		return nil, ErrSessionDenied
	}
	remaining := p.Attempt.Deadline.Sub(q.config.Now())
	if value := p.Secrets.SessionExpiresAt.Sub(q.config.Now()); value < remaining {
		remaining = value
	}
	if remaining <= 0 {
		return nil, ErrSessionDenied
	}
	if remaining > 30*time.Second {
		remaining = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	billing := billingpublicapiconnect.NewBillingUserServiceClient(q.http, p.ControlOrigin, connect.WithReadMaxBytes(q.config.MaxResponseBytes))
	coordinator := publicqueryv1connect.NewCoordinatorUserServiceClient(q.http, p.ControlOrigin, connect.WithReadMaxBytes(q.config.MaxResponseBytes))
	calls := 0
	admit := func() error {
		if calls >= q.config.MaxCalls {
			return ErrQueryLimit
		}
		calls++
		return q.before(ctx, p)
	}
	accounts := map[string]bool{}
	accountTokens := map[string]bool{}
	var results []*clientv0.EnrollmentTarget
	for token := ""; ; {
		if accountTokens[token] {
			return nil, ErrTargetProtocol
		}
		accountTokens[token] = true
		if err := admit(); err != nil {
			return nil, err
		}
		req := connect.NewRequest(&billingapi.ListAccountsRequest{Page: &billingapi.PageRequest{PageSize: q.config.UpstreamPageSize, PageToken: token}})
		req.Header().Set("Authorization", "Bearer "+p.Secrets.Access)
		response, err := billing.ListAccounts(ctx, req)
		if err != nil {
			return nil, targetQueryError(err)
		}
		for _, account := range response.Msg.Accounts {
			if account == nil || !targetID(account.AccountId) || accounts[account.AccountId] {
				return nil, ErrTargetProtocol
			}
			accounts[account.AccountId] = true
			if len(accounts) > q.config.MaxAccounts {
				return nil, ErrQueryLimit
			}
			networks := map[string]bool{}
			networkTokens := map[string]bool{}
			for networkToken := ""; ; {
				if networkTokens[networkToken] {
					return nil, ErrTargetProtocol
				}
				networkTokens[networkToken] = true
				if err := admit(); err != nil {
					return nil, err
				}
				req := connect.NewRequest(&coordinatorapi.ListNetworksRequest{AccountId: account.AccountId, Page: &coordinatorapi.PageRequest{PageSize: q.config.UpstreamPageSize, PageToken: networkToken}})
				req.Header().Set("Authorization", "Bearer "+p.Secrets.Access)
				response, err := coordinator.ListNetworks(ctx, req)
				if err != nil {
					return nil, targetQueryError(err)
				}
				for _, network := range response.Msg.Networks {
					if network == nil || !targetID(network.NetworkId) || networks[network.NetworkId] {
						return nil, ErrTargetProtocol
					}
					networks[network.NetworkId] = true
					if len(results) >= q.config.MaxTargets {
						return nil, ErrQueryLimit
					}
					results = append(results, &clientv0.EnrollmentTarget{AccountId: account.AccountId, AccountName: account.Name, NetworkId: network.NetworkId, NetworkName: network.Name, Restriction: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}})
				}
				networkToken = response.Msg.GetPage().GetNextPageToken()
				if networkToken == "" {
					break
				}
			}
		}
		token = response.Msg.GetPage().GetNextPageToken()
		if token == "" {
			break
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].AccountId != results[j].AccountId {
			return results[i].AccountId < results[j].AccountId
		}
		return results[i].NetworkId < results[j].NetworkId
	})
	return results, nil
}

func targetID(value string) bool {
	return value != "" && len(value) <= 256 && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
func targetQueryError(err error) error {
	switch connect.CodeOf(err) {
	case connect.CodeCanceled:
		return context.Canceled
	case connect.CodeDeadlineExceeded:
		return context.DeadlineExceeded
	case connect.CodeUnauthenticated:
		return ErrSessionDenied
	case connect.CodePermissionDenied, connect.CodeNotFound:
		return ErrTargetDenied
	default:
		return errors.New("target upstream unavailable")
	}
}
