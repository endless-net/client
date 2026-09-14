package client

import (
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

// Private durable execution input. No bearer belongs in the public Operation.
type clientRPCSessionRenewal struct {
	OperationID        string                               `json:"operation_id"`
	ProfileID          string                               `json:"profile_id"`
	ControlOrigin      string                               `json:"control_origin"`
	TokenBinding       string                               `json:"token_binding"`
	UserID             string                               `json:"user_id"`
	OwnerID            string                               `json:"owner_id"`
	Request            *backend.RenewSessionRequest         `json:"request"`
	Authorization      *backend.SessionRenewalAuthorization `json:"authorization"`
	BackendOperationID string                               `json:"backend_operation_id,omitempty"`
	PollAuthorization  string                               `json:"poll_authorization,omitempty"`
	ReplayExpiresAt    *timestamppb.Timestamp               `json:"replay_expires_at,omitempty"`
	NextPollAt         time.Time                            `json:"next_poll_at,omitempty"`
}

func (m *ClientRPCMutations) renewSessionAs(peer local.Peer, request *ipc.RenewSessionRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/RenewSession", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.RPCState.SessionRenewal != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		stored := cfg.UserSession
		if stored == nil || stored.TokenBinding != sessionTokenBinding(cfg.Token) || stored.ControlOrigin != profile.ControlOrigin || api.ValidateSessionResponse(stored.Response) != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
		}
		if !validRetainedSessionGrant(stored.RenewalGrant, stored.Response.Session, m.now()) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
		}
		authority := stored.RenewalGrant.RenewalAuthorization
		if stored.Response.Session.State == backend.UserSessionState_USER_SESSION_STATE_REVOKED {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
		}
		backendRequest := &backend.RenewSessionRequest{RequestId: op.RequestId, ExpectedSessionId: stored.Response.Session.SessionId}
		if api.ValidateSessionRenewalRequest(backendRequest) != nil {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		cfg.RPCState.SessionRenewal = &clientRPCSessionRenewal{OperationID: op.Id, ProfileID: profile.ID, ControlOrigin: profile.ControlOrigin, TokenBinding: stored.TokenBinding, UserID: stored.Response.Session.UserId, OwnerID: cfg.LocalOwnerID, Request: backendRequest, Authorization: proto.Clone(authority).(*backend.SessionRenewalAuthorization)}
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
		return nil
	})
	return op, err
}
