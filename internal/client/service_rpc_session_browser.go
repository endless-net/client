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
		if !sessionRenewalBound(cfg, plan) || response == nil || plan.OperationID != id || op.Kind != ipc.OperationKind_OPERATION_KIND_RENEW_SESSION || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || plan.ProfileID != op.ProfileId {
			return stale()
		}
		result := response.Operation
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
