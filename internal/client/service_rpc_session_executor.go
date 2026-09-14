package client

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Providers use producer DTOs. Renew authenticates with renewal authority;
// Poll authenticates only with the operation-specific polling authority.
type ClientRPCSessionRenewalProvider struct {
	Renew func(context.Context, string, string, *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error)
	Poll  func(context.Context, string, string, *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error)
}

func sessionRenewalBound(cfg *Config, plan *clientRPCSessionRenewal) bool {
	if cfg.RPCState == nil || plan == nil || plan.Request == nil || plan.Authorization == nil ||
		plan.ProfileID != cfg.RPCState.ActiveProfileID || !strings.EqualFold(plan.OwnerID, cfg.LocalOwnerID) ||
		plan.TokenBinding != sessionTokenBinding(cfg.Token) || plan.ControlOrigin != cfg.RPCState.Profiles[plan.ProfileID].ControlOrigin {
		return false
	}
	stored := cfg.UserSession
	return stored != nil && stored.ControlOrigin == plan.ControlOrigin && stored.TokenBinding == plan.TokenBinding &&
		api.ValidateSessionResponse(stored.Response) == nil && api.ValidateSessionResponse(stored.RenewalGrant) == nil &&
		stored.Response.Session.State != backend.UserSessionState_USER_SESSION_STATE_REVOKED &&
		stored.Response.Session.SessionId == plan.Request.ExpectedSessionId && stored.Response.Session.UserId == plan.UserID &&
		proto.Equal(stored.RenewalGrant.RenewalAuthorization, plan.Authorization)
}

func failSessionRenewal(cfg *Config, op *ipc.Operation, code ipc.ErrorCode, reason string) {
	cfg.RPCState.SessionRenewal = nil
	op.State = ipc.OperationState_OPERATION_STATE_FAILED
	op.UserAction = nil
	op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: code, ReasonKey: reason}}
}

// ReconcileSessionRenewal performs at most one call. Scheduling and RUNNING are
// durable before dispatch, so restart repeats the same request, never new intent.
func (m *ClientRPCMutations) ReconcileSessionRenewal(ctx context.Context, provider ClientRPCSessionRenewalProvider) error {
	m.sessionWorker.Lock()
	defer m.sessionWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if provider.Renew == nil || provider.Poll == nil {
		return errors.New("session renewal provider is required")
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.SessionRenewal == nil {
		return nil
	}
	id := cfg.RPCState.SessionRenewal.OperationID
	var plan *clientRPCSessionRenewal
	op, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		current := cfg.RPCState.SessionRenewal
		if current == nil || current.OperationID != id || rpcOperationTerminal(op.State) {
			return errRPCNoChange
		}
		if !sessionRenewalBound(cfg, current) {
			failSessionRenewal(cfg, op, ipc.ErrorCode_ERROR_CODE_STALE_STATE, "session_renewal_context_changed")
			return nil
		}
		now := m.now()
		if (current.BackendOperationID == "" && !now.Before(current.Authorization.ExpiresAt.AsTime())) ||
			(current.BackendOperationID != "" && (current.ReplayExpiresAt == nil || !now.Before(current.ReplayExpiresAt.AsTime()))) {
			failSessionRenewal(cfg, op, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN, "session_renewal_expired")
			return nil
		}
		if now.Before(current.NextPollAt) {
			return errRPCNoChange
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		op.UserAction = nil
		current.NextPollAt = now.Add(5 * time.Second)
		copy := *current
		copy.Request = proto.Clone(current.Request).(*backend.RenewSessionRequest)
		copy.Authorization = proto.Clone(current.Authorization).(*backend.SessionRenewalAuthorization)
		plan = &copy
		return nil
	})
	if errors.Is(err, errRPCNoChange) {
		return nil
	}
	if err != nil || rpcOperationTerminal(op.State) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var response *backend.RenewSessionResponse
	if plan.BackendOperationID == "" {
		response, err = provider.Renew(ctx, plan.ControlOrigin, plan.Authorization.Bearer, plan.Request)
	} else {
		var polled *backend.GetSessionRenewalResponse
		polled, err = provider.Poll(ctx, plan.ControlOrigin, plan.PollAuthorization, &backend.GetSessionRenewalRequest{OperationId: plan.BackendOperationID})
		if polled != nil {
			response = &backend.RenewSessionResponse{Operation: polled.Operation, PollAuthorization: plan.PollAuthorization}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Transport failure is ambiguous. Keep the original durable intent and retry
	// after its checkpointed delay; never expose the provider's error or bearer.
	if err != nil {
		return nil
	}
	result := response.GetOperation()
	if api.ValidateSessionRenewal(result, plan.Request, []string{plan.ControlOrigin}) != nil ||
		(plan.BackendOperationID != "" && (result.OperationId != plan.BackendOperationID || !proto.Equal(result.ReplayExpiresAt, plan.ReplayExpiresAt))) {
		return m.rejectSessionRenewal(id, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, "session_renewal_invalid_response")
	}
	switch result.State {
	case backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED:
		_, err = m.completeSessionRenewal(id, result)
	case backend.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER:
		_, err = m.checkpointSessionBrowser(id, response)
	case backend.SessionRenewalState_SESSION_RENEWAL_STATE_REJECTED, backend.SessionRenewalState_SESSION_RENEWAL_STATE_EXPIRED:
		return m.rejectSessionRenewal(id, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN, "session_renewal_not_approved")
	}
	if connect.CodeOf(err) == connect.CodeFailedPrecondition {
		return m.rejectSessionRenewal(id, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, "session_renewal_result_rejected")
	}
	return err
}

func (m *ClientRPCMutations) rejectSessionRenewal(id string, code ipc.ErrorCode, reason string) error {
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if cfg.RPCState.SessionRenewal == nil || cfg.RPCState.SessionRenewal.OperationID != id || rpcOperationTerminal(op.State) {
			return errRPCNoChange
		}
		if !sessionRenewalBound(cfg, cfg.RPCState.SessionRenewal) {
			code, reason = ipc.ErrorCode_ERROR_CODE_STALE_STATE, "session_renewal_context_changed"
		}
		failSessionRenewal(cfg, op, code, reason)
		return nil
	})
	if errors.Is(err, errRPCNoChange) {
		return nil
	}
	return err
}
