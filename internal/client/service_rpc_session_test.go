package client

import (
	"context"
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionReadProjectionAndContext(t *testing.T) {
	for _, scenario := range []string{"active", "warning", "expired", "revoked", "unknown_deadline", "invalid", "changed_context", "revoked_owner", "cancelled", "anonymous", "observer", "no_token"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = "private-user-token"
				if scenario == "no_token" {
					cfg.Token = ""
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			now := m.now()
			calls := 0
			response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "private-session-id", UserId: "private-user-id", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(now.Add(time.Hour))}}
			want := ipc.SessionState_SESSION_STATE_ACTIVE
			failure := ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
			switch scenario {
			case "warning":
				response.Session.WarningAt = timestamppb.New(now.Add(-time.Minute))
				want = ipc.SessionState_SESSION_STATE_EXPIRING
			case "expired":
				response.Session.ExpiresAt = timestamppb.New(now.Add(-time.Minute))
				want = ipc.SessionState_SESSION_STATE_EXPIRED
			case "revoked":
				response.Session.State = backend.UserSessionState_USER_SESSION_STATE_REVOKED
				want = ipc.SessionState_SESSION_STATE_NOT_AUTHENTICATED
			case "unknown_deadline":
				response.Session.ExpiresAt = nil
			case "invalid":
				response.Session.State = backend.UserSessionState(999)
				failure = ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			case "changed_context":
				failure = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			case "revoked_owner", "observer":
				failure = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			case "anonymous":
				failure = ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED
			case "no_token":
				want = ipc.SessionState_SESSION_STATE_NOT_AUTHENTICATED
			}
			s.SessionProvider = func(_ context.Context, origin, token string) (*backend.GetSessionResponse, error) {
				calls++
				if origin != "https://control.test" || token != "private-user-token" {
					t.Fatal("wrong session authorization context")
				}
				if scenario == "changed_context" || scenario == "revoked_owner" {
					if err := m.store.Update(func(cfg *Config) error {
						if scenario == "revoked_owner" {
							cfg.LocalOwnerID = "uid:2000"
						} else {
							cfg.Token = "rotated"
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "cancelled" {
					cancel()
				}
				return response, nil
			}
			peer := owner
			if scenario == "anonymous" {
				peer = local.Peer{}
			}
			if scenario == "observer" {
				peer = local.Peer{Identity: "uid:2000"}
			}
			result, err := s.sessionAs(ctx, peer, &ipc.GetSessionRequest{Profile: profile})
			if scenario == "cancelled" {
				if err != context.Canceled || result != nil {
					t.Fatal("cancelled read published result", err)
				}
				return
			}
			if failure != ipc.ErrorCode_ERROR_CODE_UNSPECIFIED {
				assertRPCFailure(t, err, failure)
				if result != nil {
					t.Fatal("failed read exposed session")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if result.Session.State != want || result.Session.SeamlessRenewalSupported || result.Session.Renewal.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
					t.Fatal("incorrect session projection")
				}
				wire, err := proto.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(wire), "private") {
					t.Fatal("private backend session data leaked")
				}
				if scenario == "unknown_deadline" && result.Session.ExpiresAt != nil {
					t.Fatal("invented expiry")
				}
				if result.Session.ExpiresAt != nil {
					response.Session.ExpiresAt.Seconds++
					if proto.Equal(response.Session.ExpiresAt, result.Session.ExpiresAt) {
						t.Fatal("mutable deadline alias")
					}
				}
			}
			if (scenario == "anonymous" || scenario == "observer" || scenario == "no_token") && calls != 0 {
				t.Fatal("unauthorized or unauthenticated profile invoked backend")
			}
		})
	}
}
