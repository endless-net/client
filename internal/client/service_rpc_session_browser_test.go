package client

import (
	"reflect"
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionBrowserCheckpointPrivacyAndBinding(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign_origin", "expired_action", "short_poll", "secret_url", "encoded_secret_url"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = strings.Repeat("access-", 8)
				response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("grant-", 8), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.renewSessionAs(owner, &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
				op.State = ipc.OperationState_OPERATION_STATE_RUNNING
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			action := &backend.SessionBrowserAction{ApprovalUrl: "https://control.test/approve", ExpiresAt: timestamppb.New(m.now().Add(5 * time.Minute))}
			response := &backend.RenewSessionResponse{PollAuthorization: strings.Repeat("private-poll-", 4), Operation: &backend.SessionRenewal{OperationId: "backend-operation", RequestId: op.RequestId, PreviousSessionId: "session", State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER, ReplayExpiresAt: timestamppb.New(m.now().Add(time.Hour)), PollAfterSeconds: 5, Outcome: &backend.SessionRenewal_BrowserAction{BrowserAction: action}}}
			switch scenario {
			case "foreign_origin":
				action.ApprovalUrl = "https://foreign.test/approve"
			case "expired_action":
				action.ExpiresAt = timestamppb.New(m.now().Add(-time.Minute))
			case "short_poll":
				response.PollAuthorization = "short"
			case "secret_url":
				action.ApprovalUrl += "?token=" + response.PollAuthorization
			case "encoded_secret_url":
				action.ApprovalUrl += "?token=" + strings.ReplaceAll(response.PollAuthorization, "p", "%70")
			}
			before := m.store.Read()
			waiting, err := m.checkpointSessionBrowser(op.Id, response)
			if scenario != "valid" {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("invalid checkpoint changed durable state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if waiting.State != ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER || waiting.UserAction.BrowserUrl != action.ApprovalUrl {
				t.Fatal("missing browser action")
			}
			wire, _ := proto.Marshal(waiting)
			if strings.Contains(string(wire), "private-poll") {
				t.Fatal("public operation leaked poll authority")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			plan := m.store.Read().RPCState.SessionRenewal
			if plan.BackendOperationID != response.Operation.OperationId || plan.PollAuthorization != response.PollAuthorization || plan.NextPollAt.IsZero() || !proto.Equal(plan.ReplayExpiresAt, response.Operation.ReplayExpiresAt) {
				t.Fatal("restart lost poll binding")
			}
			if _, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
				op.State = ipc.OperationState_OPERATION_STATE_RUNNING
				op.UserAction = nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			response.Operation.OperationId = "foreign-operation"
			_, err = m.checkpointSessionBrowser(op.Id, response)
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		})
	}
}
