package localapi

import (
	"context"
	"errors"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type DashboardSource interface {
	List(context.Context, state.Profile) (*clientv0.ListDashboardSpacesResponse, error)
	Get(context.Context, state.Profile, string) (*clientv0.DashboardBoard, error)
	Replace(context.Context, state.Profile, *clientv0.ReplaceDashboardBoardRequest) (*clientv0.DashboardBoard, error)
}

func dashboardSession(profile state.Profile, now time.Time) (bool, error) {
	session, err := profile.CurrentSession()
	if err != nil {
		return false, err
	}
	return session.Access != "" && session.SessionID != "" && session.UserID != "" && now.Before(session.ExpiresAt), nil
}

func (c *Controller) dashboardProfile(ctx context.Context, ref *clientv0.ProfileRef) (state.Profile, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return state.Profile{}, err
	}
	current, err := c.store.Load(ctx)
	if err != nil {
		return state.Profile{}, rpcError(err)
	}
	defer clearProfileKeys(current)
	if err = owner(&current, caller, false); err != nil {
		return state.Profile{}, err
	}
	if c.config.Dashboard == nil {
		return state.Profile{}, status.Error(codes.Unavailable, "dashboard service unavailable")
	}
	profile, found := current.Profiles[profileID(ref, current.ActiveProfileID)]
	if !found {
		return state.Profile{}, status.Error(codes.NotFound, "profile not found")
	}
	available, err := dashboardSession(profile, c.config.Now())
	if err != nil {
		return state.Profile{}, rpcError(err)
	}
	if !available {
		return state.Profile{}, dashboardRPCError(control.ErrSessionDenied)
	}
	return profile, nil
}

func dashboardRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, control.ErrSessionDenied):
		return status.Error(codes.Unauthenticated, "native user session required")
	case errors.Is(err, control.ErrTargetDenied):
		return status.Error(codes.PermissionDenied, "dashboard access denied")
	case errors.Is(err, control.ErrDashboardConflict):
		return status.Error(codes.Aborted, "dashboard revision changed")
	case errors.Is(err, control.ErrDashboardInput):
		return status.Error(codes.InvalidArgument, "invalid dashboard request")
	case errors.Is(err, control.ErrQueryLimit):
		return status.Error(codes.ResourceExhausted, "space catalog budget exceeded")
	default:
		return status.Error(codes.Unavailable, "dashboard request unavailable")
	}
}

func validDashboardRequest(req proto.Message, ref *clientv0.ProfileRef) bool {
	message := req.ProtoReflect()
	return message.IsValid() && len(message.GetUnknown()) == 0 &&
		(ref == nil || len(ref.ProtoReflect().GetUnknown()) == 0)
}

func (c *Controller) ListDashboardSpaces(ctx context.Context, req *clientv0.ListDashboardSpacesRequest) (*clientv0.ListDashboardSpacesResponse, error) {
	if !validDashboardRequest(req, req.GetProfile()) {
		return nil, dashboardRPCError(control.ErrDashboardInput)
	}
	profile, err := c.dashboardProfile(ctx, req.Profile)
	if err != nil {
		return nil, err
	}
	result, err := c.config.Dashboard.List(ctx, profile)
	return result, dashboardRPCError(err)
}

func (c *Controller) GetDashboardBoard(ctx context.Context, req *clientv0.GetDashboardBoardRequest) (*clientv0.GetDashboardBoardResponse, error) {
	if !validDashboardRequest(req, req.GetProfile()) {
		return nil, dashboardRPCError(control.ErrDashboardInput)
	}
	profile, err := c.dashboardProfile(ctx, req.Profile)
	if err != nil {
		return nil, err
	}
	board, err := c.config.Dashboard.Get(ctx, profile, req.AccountId)
	if err != nil {
		return nil, dashboardRPCError(err)
	}
	return &clientv0.GetDashboardBoardResponse{Board: board}, nil
}

func (c *Controller) ReplaceDashboardBoard(ctx context.Context, req *clientv0.ReplaceDashboardBoardRequest) (*clientv0.ReplaceDashboardBoardResponse, error) {
	if !validDashboardRequest(req, req.GetProfile()) {
		return nil, dashboardRPCError(control.ErrDashboardInput)
	}
	profile, err := c.dashboardProfile(ctx, req.Profile)
	if err != nil {
		return nil, err
	}
	board, err := c.config.Dashboard.Replace(ctx, profile, req)
	if err != nil {
		return nil, dashboardRPCError(err)
	}
	return &clientv0.ReplaceDashboardBoardResponse{Board: board}, nil
}
