package localapi

import (
	"context"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Controller) GetSession(ctx context.Context, request *clientv0.GetSessionRequest) (*clientv0.GetSessionResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.getSession(ctx, caller, request)
}

// A pure local snapshot: no expiry write, remote query, claim or worker wake.
func (c *Controller) getSession(parent context.Context, caller localipc.Principal, request *clientv0.GetSessionRequest) (*clientv0.GetSessionResponse, error) {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || request.Profile != nil && (len(request.Profile.ProtoReflect().GetUnknown()) != 0 || request.Profile.ProfileId != "" && !validID(request.Profile.ProfileId)) {
		return nil, status.Error(codes.InvalidArgument, "client.session.invalid_input")
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err := owner(&s, caller, false); err != nil {
		return nil, rpcError(err)
	}
	profile, ok := s.Profiles[profileID(request.Profile, s.ActiveProfileID)]
	if !ok {
		return nil, status.Error(codes.NotFound, "client.session.profile_not_found")
	}
	current, err := profile.CurrentSession()
	if err != nil {
		return nil, rpcError(err)
	}
	return &clientv0.GetSessionResponse{
		Session:      c.projectSession(current),
		Metadata:     &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())},
		LoginAttempt: c.projectCurrentSessionLogin(s, profile),
	}, nil
}

func (c *Controller) projectSession(current state.SessionCredentials) *clientv0.Session {
	result := &clientv0.Session{State: clientv0.SessionState_SESSION_STATE_NOT_AUTHENTICATED}
	if current.Access == "" || current.ExpiresAt.IsZero() {
		return result
	}
	result.ExpiresAt = timestamppb.New(current.ExpiresAt)
	result.State = clientv0.SessionState_SESSION_STATE_ACTIVE
	if !c.config.Now().Before(current.ExpiresAt) {
		result.State = clientv0.SessionState_SESSION_STATE_EXPIRED
	}
	return result
}
