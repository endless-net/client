package client

import (
	"context"
	"reflect"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The response remains inside the trusted core; renewal authority must never
// be forwarded to local IPC. Renewal persistence/execution is a separate path.
type ClientRPCSessionProvider func(context.Context, string, string) (*backend.GetSessionResponse, error)

func (s *ClientRPCService) GetSession(ctx context.Context, request *connect.Request[ipc.GetSessionRequest]) (*connect.Response[ipc.GetSessionResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	response, err := s.sessionAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (s *ClientRPCService) sessionAs(ctx context.Context, peer local.Peer, request *ipc.GetSessionRequest) (*ipc.GetSessionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/GetSession")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	selected := profile.Configuration
	if profile.ID == cfg.RPCState.ActiveProfileID {
		selected = cfg
	}
	session := &ipc.Session{State: ipc.SessionState_SESSION_STATE_NOT_AUTHENTICATED,
		Renewal: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "session_renewal_not_implemented"}}
	var stored *StoredUserSession
	if selected.Token != "" {
		if s.SessionProvider == nil {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		response, err := s.SessionProvider(ctx, profile.ControlOrigin, selected.Token)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, rpcNetworkCatalogFailure(err)
		}
		session, err = rpcProjectSession(response, s.mutations.now())
		if err != nil {
			return nil, err
		}
		stored = &StoredUserSession{ControlOrigin: profile.ControlOrigin, TokenBinding: sessionTokenBinding(selected.Token), Response: proto.Clone(response).(*backend.GetSessionResponse)}
	}
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	current := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, method, current); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(current)) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unchanged := stored != nil && selected.UserSession != nil &&
		stored.ControlOrigin == selected.UserSession.ControlOrigin && stored.TokenBinding == selected.UserSession.TokenBinding &&
		proto.Equal(stored.Response, selected.UserSession.Response)
	if stored != nil && !unchanged {
		if err := s.mutations.store.Update(func(next *Config) error {
			if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(*next)) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			if profile.ID == next.RPCState.ActiveProfileID {
				next.UserSession = stored
			} else {
				p := next.RPCState.Profiles[profile.ID]
				p.Configuration.UserSession = stored
				next.RPCState.Profiles[profile.ID] = p
			}
			next.RPCState.Revision++
			return nil
		}); err != nil {
			return nil, err
		}
		cfg = s.mutations.store.Read()
		s.mutations.publishMutationLocked(nil, ipc.Domain_DOMAIN_SESSION)
	}
	return &ipc.GetSessionResponse{Session: session, Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}}, nil
}

func rpcProjectSession(response *backend.GetSessionResponse, now time.Time) (*ipc.Session, error) {
	if api.ValidateSessionResponse(response) != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	observed := response.Session
	session := &ipc.Session{Renewal: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "session_renewal_not_implemented"}}
	switch observed.State {
	case backend.UserSessionState_USER_SESSION_STATE_ACTIVE:
		session.State = ipc.SessionState_SESSION_STATE_ACTIVE
	case backend.UserSessionState_USER_SESSION_STATE_EXPIRING:
		session.State = ipc.SessionState_SESSION_STATE_EXPIRING
	case backend.UserSessionState_USER_SESSION_STATE_EXPIRED:
		session.State = ipc.SessionState_SESSION_STATE_EXPIRED
	case backend.UserSessionState_USER_SESSION_STATE_REVOKED:
		session.State = ipc.SessionState_SESSION_STATE_NOT_AUTHENTICATED
	default:
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if observed.ExpiresAt != nil {
		session.ExpiresAt = proto.Clone(observed.ExpiresAt).(*timestamppb.Timestamp)
	}
	if observed.WarningAt != nil {
		session.WarningAt = proto.Clone(observed.WarningAt).(*timestamppb.Timestamp)
	}
	if session.State == ipc.SessionState_SESSION_STATE_ACTIVE || session.State == ipc.SessionState_SESSION_STATE_EXPIRING {
		if session.ExpiresAt != nil && !now.Before(session.ExpiresAt.AsTime()) {
			session.State = ipc.SessionState_SESSION_STATE_EXPIRED
		} else if session.WarningAt != nil && !now.Before(session.WarningAt.AsTime()) {
			session.State = ipc.SessionState_SESSION_STATE_EXPIRING
		}
	}
	return session, nil
}
