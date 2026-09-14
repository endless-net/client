package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionRenewalExecutorRestartPollingAndFailure(t *testing.T) {
	for _, scenario := range []string{"success", "rejected", "expired", "foreign_operation", "token_changed", "token_changed_during_poll", "malformed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			now := time.Now()
			m.now = func() time.Time { return now }
			grant := strings.Repeat("grant-", 8)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = strings.Repeat("access-", 8)
				response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: grant, ExpiresAt: timestamppb.New(now.Add(time.Hour))}}
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.renewSessionAs(owner, &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			calls, polls := 0, 0
			pollAuth := strings.Repeat("poll-", 8)
			replay := timestamppb.New(now.Add(time.Hour))
			provider := ClientRPCSessionRenewalProvider{
				Renew: func(_ context.Context, origin, auth string, request *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
					calls++
					if origin != "https://control.test" || auth != grant || request.RequestId != op.RequestId || request.ExpectedSessionId != "session" {
						t.Fatal("dispatch binding lost")
					}
					cfg := m.store.Read()
					persisted := new(ipc.Operation)
					if err := proto.Unmarshal(cfg.RPCState.Operations[op.RequestId].Operation, persisted); err != nil {
						t.Fatal(err)
					}
					if persisted.State != ipc.OperationState_OPERATION_STATE_RUNNING || cfg.RPCState.SessionRenewal.NextPollAt.IsZero() {
						t.Fatal("dispatch preceded durable checkpoint")
					}
					if calls == 1 {
						return nil, errors.New("ambiguous response")
					}
					return &backend.RenewSessionResponse{PollAuthorization: pollAuth, Operation: &backend.SessionRenewal{OperationId: "backend-operation", RequestId: request.RequestId, PreviousSessionId: "session", State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER, ReplayExpiresAt: replay, PollAfterSeconds: 10, Outcome: &backend.SessionRenewal_BrowserAction{BrowserAction: &backend.SessionBrowserAction{ApprovalUrl: "https://control.test/approve", ExpiresAt: timestamppb.New(now.Add(time.Minute))}}}}, nil
				},
				Poll: func(_ context.Context, origin, auth string, request *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
					polls++
					if origin != "https://control.test" || auth != pollAuth || request.OperationId != "backend-operation" {
						t.Fatal("poll used wrong authority or operation")
					}
					result := &backend.SessionRenewal{OperationId: request.OperationId, RequestId: op.RequestId, PreviousSessionId: "session", ReplayExpiresAt: replay, State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, Outcome: &backend.SessionRenewal_Result{Result: &backend.SessionRenewalResult{AccessBearer: strings.Repeat("rotated-", 8), Session: &backend.UserSession{SessionId: "new-session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE}}}}
					if scenario == "rejected" {
						result.State = backend.SessionRenewalState_SESSION_RENEWAL_STATE_REJECTED
						result.Outcome = nil
					}
					if scenario == "foreign_operation" {
						result.OperationId = "other"
					}
					if scenario == "token_changed_during_poll" {
						if err := m.store.Update(func(cfg *Config) error { cfg.Token = "replacement"; return nil }); err != nil {
							t.Fatal(err)
						}
					}
					if scenario == "malformed" {
						return nil, nil
					}
					return &backend.GetSessionRenewalResponse{Operation: result}, nil
				},
			}
			run := func() {
				t.Helper()
				if err := m.ReconcileSessionRenewal(t.Context(), provider); err != nil {
					t.Fatal(err)
				}
			}
			run()
			run()
			if calls != 1 {
				t.Fatal("retry delay ignored")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			m.now = func() time.Time { return now }
			now = now.Add(5 * time.Second)
			run()
			run()
			if calls != 2 || polls != 0 {
				t.Fatal("restart replay or server poll delay violated")
			}
			now = now.Add(10 * time.Second)
			switch scenario {
			case "expired":
				now = now.Add(2 * time.Hour)
			case "token_changed":
				if err := m.store.Update(func(cfg *Config) error { cfg.Token = "replacement"; return nil }); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if !errors.Is(m.ReconcileSessionRenewal(ctx, provider), context.Canceled) || polls != 0 {
					t.Fatal("cancelled executor dispatched")
				}
				return
			}
			run()
			cfg := m.store.Read()
			completed := new(ipc.Operation)
			if err := proto.Unmarshal(cfg.RPCState.Operations[op.RequestId].Operation, completed); err != nil {
				t.Fatal(err)
			}
			if cfg.RPCState.SessionRenewal != nil {
				t.Fatal("terminal result retained private authority")
			}
			if scenario == "success" {
				if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.Token != strings.Repeat("rotated-", 8) {
					t.Fatal("success did not rotate token")
				}
			} else if completed.State != ipc.OperationState_OPERATION_STATE_FAILED || completed.GetFailure() == nil {
				t.Fatal("failure not durable")
			}
			if (scenario == "expired" || scenario == "token_changed") && polls != 0 {
				t.Fatal("invalid context reached backend")
			}
			if scenario == "token_changed_during_poll" && (cfg.Token != "replacement" || completed.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_STALE_STATE) {
				t.Fatal("late response overwrote new session context")
			}
			run()
		})
	}
}
