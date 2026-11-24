package control

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"
	billing "github.com/endless-net/billing/api/public/v1"
	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	ipc "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/management/api/dashboard"
	remote "github.com/endless-net/management/api/v1"
	"github.com/endless-net/management/api/v1/managementapiconnect"
)

var ErrDashboardConflict = errors.New("dashboard revision conflict")
var ErrDashboardInput = errors.New("invalid dashboard input")
var ErrDashboardUnavailable = errors.New("dashboard upstream unavailable")

type Dashboard struct {
	http          *http.Client
	announcements Announcements
	config        TargetConfig
}

func NewDashboard(client *http.Client, announcements Announcements, config TargetConfig) (*Dashboard, error) {
	if announcements == nil || config.MaxCalls <= 0 || config.MaxAccounts <= 0 || config.UpstreamPageSize == 0 || config.UpstreamPageSize > 500 {
		return nil, ErrDashboardInput
	}
	bootstrap, err := NewBootstrap(client, int64(config.MaxResponseBytes), config.Now)
	if err != nil {
		return nil, err
	}
	config.Now = bootstrap.now
	return &Dashboard{http: bootstrap.client, announcements: announcements, config: config}, nil
}

func (d *Dashboard) call(parent context.Context, profile state.Profile, run func(context.Context, string) error) error {
	session, err := profile.CurrentSession()
	if err != nil {
		return err
	}
	remaining := session.ExpiresAt.Sub(d.config.Now())
	if remaining <= 0 || session.Access == "" || session.SessionID == "" || session.UserID == "" {
		return ErrSessionDenied
	}
	ctx, cancel := context.WithTimeout(parent, min(30*time.Second, remaining))
	defer cancel()
	announcement, err := d.announcements.Fetch(ctx, profile.ControlOrigin)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !announcement.MatchesProfile(profile, d.config.Now()) {
		return ErrBootstrap
	}
	err = run(ctx, session.Access)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !d.config.Now().Before(session.ExpiresAt) {
		return ErrSessionDenied
	}
	return err
}

func dashboardError(err error) error {
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		return ErrSessionDenied
	case connect.CodePermissionDenied, connect.CodeNotFound:
		return ErrTargetDenied
	case connect.CodeAborted:
		return ErrDashboardConflict
	case connect.CodeInvalidArgument:
		return ErrDashboardInput
	case connect.CodeCanceled:
		return context.Canceled
	case connect.CodeDeadlineExceeded:
		return context.DeadlineExceeded
	default:
		return ErrDashboardUnavailable
	}
}

func (d *Dashboard) List(ctx context.Context, profile state.Profile) (*ipc.ListDashboardSpacesResponse, error) {
	client := billingpublicapiconnect.NewBillingUserServiceClient(d.http, profile.ControlOrigin,
		connect.WithReadMaxBytes(d.config.MaxResponseBytes), connect.WithSendMaxBytes(4096))
	result := &ipc.ListDashboardSpacesResponse{}
	err := d.call(ctx, profile, func(ctx context.Context, access string) error {
		seen := map[string]bool{}
		tokens := map[string]bool{}
		for token := ""; ; {
			if tokens[token] || len(tokens)+1 >= d.config.MaxCalls {
				return ErrQueryLimit
			}
			tokens[token] = true
			request := &billing.ListAccountsRequest{Page: &billing.PageRequest{PageSize: d.config.UpstreamPageSize, PageToken: token}}
			response, err := client.ListAccounts(ctx, bearer(request, access))
			if err != nil {
				return dashboardError(err)
			}
			if !knownRPC(response.Msg.ProtoReflect()) {
				return ErrTargetProtocol
			}
			for _, account := range response.Msg.Accounts {
				id, name := account.GetAccountId(), account.GetName()
				if contract.ValidateGet(&remote.GetBoardRequest{AccountId: id}) != nil || seen[id] ||
					strings.TrimSpace(name) == "" || len(name) > 512 || strings.ContainsFunc(name, unicode.IsControl) {
					return ErrTargetProtocol
				}
				if len(seen) >= d.config.MaxAccounts {
					return ErrQueryLimit
				}
				seen[id] = true
				result.Spaces = append(result.Spaces, &ipc.DashboardSpace{AccountId: id, DisplayName: name})
			}
			token = response.Msg.GetPage().GetNextPageToken()
			if token == "" {
				break
			}
		}
		primary, err := client.GetPrimaryAccount(ctx, bearer(&billing.GetPrimaryAccountRequest{}, access))
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil
		}
		if err != nil {
			return dashboardError(err)
		}
		if !knownRPC(primary.Msg.ProtoReflect()) || !seen[primary.Msg.GetAccount().GetAccountId()] {
			return ErrTargetProtocol
		}
		result.PrimaryAccountId = primary.Msg.Account.AccountId
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *Dashboard) remote(profile state.Profile) managementapiconnect.ManagementDashboardServiceClient {
	return managementapiconnect.NewManagementDashboardServiceClient(d.http, profile.ControlOrigin,
		connect.WithReadMaxBytes(d.config.MaxResponseBytes), connect.WithSendMaxBytes(4096))
}

func (d *Dashboard) Get(ctx context.Context, profile state.Profile, account string) (*ipc.DashboardBoard, error) {
	request := &remote.GetBoardRequest{AccountId: account}
	if contract.ValidateGet(request) != nil {
		return nil, ErrDashboardInput
	}
	var result *ipc.DashboardBoard
	err := d.call(ctx, profile, func(ctx context.Context, access string) error {
		response, err := d.remote(profile).GetBoard(ctx, bearer(request, access))
		if err != nil {
			return dashboardError(err)
		}
		result, err = projectDashboard(account, response.Msg.GetBoard())
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *Dashboard) Replace(ctx context.Context, profile state.Profile, input *ipc.ReplaceDashboardBoardRequest) (*ipc.DashboardBoard, error) {
	if input == nil || input.Layout == nil || !knownRPC(input.ProtoReflect()) || len(input.Layout.Widgets) > 6 {
		return nil, ErrDashboardInput
	}
	request := &remote.ReplaceBoardRequest{
		AccountId: input.AccountId, ExpectedRevision: input.ExpectedRevision,
		Layout: &remote.DashboardLayout{Image: remote.DashboardImage(input.Layout.Image)},
	}
	for _, widget := range input.Layout.Widgets {
		request.Layout.Widgets = append(request.Layout.Widgets, &remote.DashboardWidget{
			Kind: remote.DashboardWidgetKind(widget.GetKind()), Size: remote.DashboardWidgetSize(widget.GetSize()),
		})
	}
	if contract.ValidateReplace(request) != nil {
		return nil, ErrDashboardInput
	}
	var result *ipc.DashboardBoard
	err := d.call(ctx, profile, func(ctx context.Context, access string) error {
		response, err := d.remote(profile).ReplaceBoard(ctx, bearer(request, access))
		if err != nil {
			return dashboardError(err)
		}
		result, err = projectDashboard(input.AccountId, response.Msg.GetBoard())
		if err == nil && (result.Revision != input.GetExpectedRevision()+1 || !result.CanEdit) {
			return ErrTargetProtocol
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func projectDashboard(account string, board *remote.DashboardBoard) (*ipc.DashboardBoard, error) {
	if board == nil || len(board.ProtoReflect().GetUnknown()) != 0 || board.AccountId != account ||
		board.Revision > math.MaxInt64 || contract.ValidateLayout(board.Layout) != nil {
		return nil, ErrTargetProtocol
	}
	layout := &ipc.DashboardLayout{Image: ipc.DashboardImage(board.Layout.Image)}
	for _, widget := range board.Layout.Widgets {
		layout.Widgets = append(layout.Widgets, &ipc.DashboardWidget{
			Kind: ipc.DashboardWidgetKind(widget.Kind), Size: ipc.DashboardWidgetSize(widget.Size),
		})
	}
	return &ipc.DashboardBoard{AccountId: account, Revision: board.Revision, Layout: layout, CanEdit: board.CanEdit}, nil
}
