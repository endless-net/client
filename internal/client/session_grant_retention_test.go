package client

import (
	"context"
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionGrantRetentionAfterExpiryObservation(t *testing.T) {
	for _, scenario := range []string{"expired", "revoked", "different_user", "different_session", "grant_expired", "renewal_disabled"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			now := m.now()
			base := now
			m.now = func() time.Time { return now }
			if err := m.store.Update(func(cfg *Config) error { cfg.Token = "access-token"; return nil }); err != nil {
				t.Fatal(err)
			}
			response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true, ExpiresAt: timestamppb.New(base.Add(time.Minute))}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("private-grant-", 4), ExpiresAt: timestamppb.New(base.Add(time.Hour))}}
			s := NewClientRPCService(m, nil)
			s.SessionProvider = func(context.Context, string, string) (*backend.GetSessionResponse, error) { return response, nil }
			request := &ipc.GetSessionRequest{Profile: profile}
			if _, err := s.sessionAs(t.Context(), owner, request); err != nil {
				t.Fatal(err)
			}
			original := proto.Clone(response)
			now = base.Add(2 * time.Minute)
			response = &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_EXPIRED, ExpiresAt: timestamppb.New(base.Add(time.Minute))}}
			switch scenario {
			case "revoked":
				response.Session.State = backend.UserSessionState_USER_SESSION_STATE_REVOKED
			case "different_user":
				response.Session.UserId = "other-user"
			case "different_session":
				response.Session.SessionId = "other-session"
			case "grant_expired":
				now = base.Add(2 * time.Hour)
			case "renewal_disabled":
				response.Session.State = backend.UserSessionState_USER_SESSION_STATE_ACTIVE
				response.Session.ExpiresAt = timestamppb.New(now.Add(time.Hour))
			}
			if _, err := s.sessionAs(t.Context(), owner, request); err != nil {
				t.Fatal(err)
			}
			m, err := NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			m.now = func() time.Time { return now }
			stored := m.store.Read().UserSession
			if !proto.Equal(stored.Response, response) {
				t.Fatal("retaining authority overwrote latest observation")
			}
			if scenario == "expired" {
				if !proto.Equal(stored.RenewalGrant, original) {
					t.Fatal("expiry discarded live grant")
				}
			} else if stored.RenewalGrant != nil {
				t.Fatal("unusable or revoked grant survived")
			}
			op, err := m.renewSessionAs(owner, &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if scenario == "expired" {
				if err != nil || op.State != ipc.OperationState_OPERATION_STATE_PENDING {
					t.Fatal("saved grant could not admit renewal", err)
				}
			} else {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
			}
		})
	}
}
