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

func TestRPCSessionRenewalResultAtomicRotationAndBinding(t *testing.T) {
	for _, scenario := range []string{"success", "foreign_user", "foreign_request", "expired_result", "expired_replay", "owner_changed", "token_changed"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = strings.Repeat("old-token-", 4)
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected}
				response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "old-session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("grant-", 8), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			op, err := m.renewSessionAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
				op.State = ipc.OperationState_OPERATION_STATE_RUNNING
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			result := &backend.SessionRenewal{OperationId: "backend-operation", RequestId: op.RequestId, PreviousSessionId: "old-session", State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, ReplayExpiresAt: timestamppb.New(m.now().Add(time.Hour)), Outcome: &backend.SessionRenewal_Result{Result: &backend.SessionRenewalResult{AccessBearer: strings.Repeat("private-new-token-", 3), Session: &backend.UserSession{SessionId: "new-session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}}}
			switch scenario {
			case "foreign_user":
				result.GetResult().Session.UserId = "other-user"
			case "foreign_request":
				result.RequestId = rpcCreateRequest(t, m).Mutation.RequestId
			case "expired_result":
				result.GetResult().Session.ExpiresAt = timestamppb.New(m.now().Add(-time.Minute))
			case "expired_replay":
				result.ReplayExpiresAt = timestamppb.New(m.now().Add(-time.Minute))
			case "owner_changed", "token_changed":
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "owner_changed" {
						cfg.LocalOwnerID = "uid:2000"
					} else {
						cfg.Token = "replaced"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.store.Read()
			completed, err := m.completeSessionRenewal(op.Id, result)
			if scenario != "success" {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("invalid result partially changed state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || completed.GetRenewal() == nil || cfg.Token != result.GetResult().AccessBearer || cfg.UserSession.Response.Session.SessionId != "new-session" || cfg.RPCState.SessionRenewal != nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.NodeID != before.NodeID {
				t.Fatal("rotation not atomic or changed connection context")
			}
			wire, _ := proto.Marshal(completed)
			if strings.Contains(string(wire), "private-new-token") {
				t.Fatal("renewal operation leaked bearer")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := m.renewSessionAs(owner, request)
			if err != nil || !proto.Equal(completed, replayed) || m.store.Read().Token != cfg.Token {
				t.Fatal("restart did not preserve completion and rotated token", err)
			}
		})
	}
}
