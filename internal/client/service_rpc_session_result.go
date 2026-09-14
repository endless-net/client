package client

import (
	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Commit a validated successful result and its rotated authority atomically.
// The executor must have durably entered RUNNING before making the backend call.
func (m *ClientRPCMutations) completeSessionRenewal(id string, result *backend.SessionRenewal) (*ipc.Operation, error) {
	return m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
		plan := cfg.RPCState.SessionRenewal
		if !sessionRenewalBound(cfg, plan) || plan.OperationID != id || op.Kind != ipc.OperationKind_OPERATION_KIND_RENEW_SESSION || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || plan.ProfileID != op.ProfileId {
			return stale()
		}
		if api.ValidateSessionRenewal(result, plan.Request, []string{plan.ControlOrigin}) != nil || result.State != backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED ||
			!m.now().Before(result.ReplayExpiresAt.AsTime()) {
			return stale()
		}
		if plan.BackendOperationID != "" && (plan.BackendOperationID != result.OperationId || !proto.Equal(plan.ReplayExpiresAt, result.ReplayExpiresAt)) {
			return stale()
		}
		rotated := result.GetResult()
		if rotated.Session.UserId != plan.UserID || rotated.AccessBearer == cfg.Token ||
			(rotated.Session.ExpiresAt != nil && !m.now().Before(rotated.Session.ExpiresAt.AsTime())) {
			return stale()
		}
		response := proto.Clone(&backend.GetSessionResponse{Session: rotated.Session, RenewalAuthorization: rotated.RenewalAuthorization}).(*backend.GetSessionResponse)
		cfg.Token = rotated.AccessBearer
		cfg.UserSession = &StoredUserSession{ControlOrigin: plan.ControlOrigin, TokenBinding: sessionTokenBinding(cfg.Token), Response: response}
		if response.Session.RenewalSupported {
			cfg.UserSession.RenewalGrant = proto.Clone(response).(*backend.GetSessionResponse)
		}
		cfg.RPCState.SessionRenewal = nil
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.UserAction = nil
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
		outcome := &ipc.RenewalResult{}
		if response.Session.ExpiresAt != nil {
			outcome.ExpiresAt = proto.Clone(response.Session.ExpiresAt).(*timestamppb.Timestamp)
		}
		op.Outcome = &ipc.Operation_Renewal{Renewal: outcome}
		return nil
	})
}
