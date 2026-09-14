package client

import (
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (m *ClientRPCMutations) checkpointSessionBrowser(id string, response *backend.RenewSessionResponse) (*ipc.Operation, error) {
	return m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
		plan := cfg.RPCState.SessionRenewal
		if plan == nil || plan.Request == nil || response == nil || plan.OperationID != id || op.Kind != ipc.OperationKind_OPERATION_KIND_RENEW_SESSION || op.State != ipc.OperationState_OPERATION_STATE_RUNNING ||
			plan.ProfileID != cfg.RPCState.ActiveProfileID || plan.ProfileID != op.ProfileId || !strings.EqualFold(plan.OwnerID, cfg.LocalOwnerID) ||
			plan.TokenBinding != sessionTokenBinding(cfg.Token) || plan.ControlOrigin != cfg.RPCState.Profiles[plan.ProfileID].ControlOrigin {
			return stale()
		}
		result := response.Operation
		stored := cfg.UserSession
		if stored == nil || api.ValidateSessionResponse(stored.Response) != nil || api.ValidateSessionResponse(stored.RenewalGrant) != nil ||
			stored.Response.Session.State == backend.UserSessionState_USER_SESSION_STATE_REVOKED || stored.Response.Session.UserId != plan.UserID ||
			stored.Response.Session.SessionId != plan.Request.ExpectedSessionId || !proto.Equal(stored.RenewalGrant.RenewalAuthorization, plan.Authorization) {
			return stale()
		}
		if api.ValidateSessionRenewal(result, plan.Request, []string{plan.ControlOrigin}) != nil || result.State != backend.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER ||
			!m.now().Before(result.ReplayExpiresAt.AsTime()) || !m.now().Before(result.GetBrowserAction().ExpiresAt.AsTime()) {
			return stale()
		}
		poll := response.PollAuthorization
		if len(poll) < 32 || len(poll) > 4096 || strings.IndexFunc(poll, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
			return stale()
		}
		if plan.BackendOperationID != "" && (plan.BackendOperationID != result.OperationId || plan.PollAuthorization != poll || !proto.Equal(plan.ReplayExpiresAt, result.ReplayExpiresAt)) {
			return stale()
		}
		action := result.GetBrowserAction()
		decodedURL, err := url.QueryUnescape(action.ApprovalUrl)
		if err != nil || strings.Contains(decodedURL, poll) || (plan.Authorization != nil && strings.Contains(decodedURL, plan.Authorization.Bearer)) || (cfg.Token != "" && strings.Contains(decodedURL, cfg.Token)) {
			return stale()
		}
		plan.BackendOperationID = result.OperationId
		plan.PollAuthorization = poll
		plan.ReplayExpiresAt = proto.Clone(result.ReplayExpiresAt).(*timestamppb.Timestamp)
		plan.NextPollAt = m.now().Add(time.Duration(result.PollAfterSeconds) * time.Second)
		op.State = ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER
		op.UserAction = &ipc.UserAction{Kind: ipc.UserAction_KIND_OPEN_BROWSER, BrowserUrl: action.ApprovalUrl, ExpiresAt: proto.Clone(action.ExpiresAt).(*timestamppb.Timestamp), ReasonKey: "session_renewal_browser_approval"}
		return nil
	})
}
