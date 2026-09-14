package client

import (
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionRenewalAdmissionAndDurableReplay(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.Token = "access-token"
		cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: &backend.GetSessionResponse{
			Session:              &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true, ExpiresAt: timestamppb.New(m.now().Add(-time.Hour))},
			RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("private-renewal-authority", 2), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))},
		}}
		cfg.UserSession.RenewalGrant = proto.Clone(cfg.UserSession.Response).(*backend.GetSessionResponse)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
	before := m.Metadata().Revision
	_, err := m.renewSessionAs(local.Peer{Identity: "uid:2000"}, request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if m.Metadata().Revision != before || m.store.Read().RPCState.SessionRenewal != nil {
		t.Fatal("unauthorized renewal was persisted")
	}
	op, err := m.renewSessionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != ipc.OperationKind_OPERATION_KIND_RENEW_SESSION || op.State != ipc.OperationState_OPERATION_STATE_PENDING {
		t.Fatal("renewal was not durably admitted as pending")
	}
	wire, err := proto.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "private-renewal") {
		t.Fatal("operation leaked renewal authority")
	}
	plan := m.store.Read().RPCState.SessionRenewal
	if plan.Request.RequestId != request.Mutation.RequestId || plan.Request.ExpectedSessionId != "session" || plan.OperationID != op.Id {
		t.Fatal("backend request not bound to admitted operation")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	revision := m.Metadata().Revision
	replayed, err := m.renewSessionAs(owner, request)
	if err != nil || !proto.Equal(op, replayed) || m.Metadata().Revision != revision {
		t.Fatal("renewal replay did not survive restart", err)
	}
	if restored := m.store.Read().RPCState.SessionRenewal; restored == nil || !proto.Equal(restored.Request, plan.Request) || !proto.Equal(restored.Authorization, plan.Authorization) {
		t.Fatal("restart lost exact backend request or authority")
	}
	_, err = m.renewSessionAs(owner, &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
}
