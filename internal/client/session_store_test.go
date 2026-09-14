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

func TestRPCSessionAuthorityPersistenceAndCleanup(t *testing.T) {
	for _, cleanup := range []string{"logout", "rotate", "remove"} {
		t.Run(cleanup, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error { cfg.Token = "private-session-token"; return nil }); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			response := &backend.GetSessionResponse{
				Session:              &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true, ExpiresAt: timestamppb.New(m.now().Add(time.Hour))},
				RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("private-renewal-", 4), ExpiresAt: timestamppb.New(m.now().Add(2 * time.Hour))},
			}
			s.SessionProvider = func(context.Context, string, string) (*backend.GetSessionResponse, error) { return response, nil }
			request := &ipc.GetSessionRequest{Profile: profile}
			got, err := s.sessionAs(t.Context(), peer, request)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := proto.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "private") {
				t.Fatal("renewal bearer escaped into IPC")
			}
			stored := m.store.Read().UserSession
			if stored == nil || !proto.Equal(stored.Response, response) || stored.ControlOrigin != "https://control.test" || stored.TokenBinding != sessionTokenBinding("private-session-token") {
				t.Fatal("authority was not persisted with context binding")
			}
			revision := m.Metadata().Revision
			if got.Metadata.Revision != revision {
				t.Fatal("read returned pre-persistence metadata")
			}
			if _, err := s.sessionAs(t.Context(), peer, request); err != nil || m.Metadata().Revision != revision {
				t.Fatal("identical session read rewrote durable state", err)
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if persisted := m.store.Read().UserSession; persisted == nil || !proto.Equal(persisted.Response, response) {
				t.Fatal("restart lost renewal authority")
			}
			response.RenewalAuthorization.Bearer = "changed-provider-value"
			if m.store.Read().UserSession.Response.RenewalAuthorization.Bearer == response.RenewalAuthorization.Bearer {
				t.Fatal("provider owns stored secret alias")
			}
			if err := m.store.Update(func(cfg *Config) error {
				switch cleanup {
				case "logout":
					return ApplyLocalLogoutCleanup(cfg, m.now())
				case "rotate":
					cfg.Token = "next-user-session"
				case "remove":
					cfg.Token = ""
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if m.store.Read().UserSession != nil {
				t.Fatal("old renewal authority survived session cleanup")
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if m.store.Read().UserSession != nil {
				t.Fatal("restart restored removed renewal authority")
			}
		})
	}
}
