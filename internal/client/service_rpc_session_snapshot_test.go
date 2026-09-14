package client

import (
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionSnapshotUsesBoundAuthorityAndOwnClock(t *testing.T) {
	for _, scenario := range []string{"valid", "expired", "foreign_origin", "foreign_token", "absent", "logout"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, _ := rpcConnectFixture(t)
			deadline := m.now().Add(time.Hour)
			if scenario == "expired" {
				deadline = m.now().Add(-time.Minute)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = "user-token"
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, ExpiresAt: timestamppb.New(deadline)}}}
				switch scenario {
				case "foreign_origin":
					cfg.UserSession.ControlOrigin = "https://other.test"
				case "foreign_token":
					cfg.UserSession.TokenBinding = sessionTokenBinding("other-token")
				case "absent":
					cfg.UserSession = nil
				case "logout":
					return ApplyLocalLogoutCleanup(cfg, m.now())
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			// A delayed provider must never override the authoritative session.
			m.observedStatus = &ipc.Status{Session: &ipc.Session{State: ipc.SessionState_SESSION_STATE_RENEWING}, Credential: &ipc.CredentialStatus{State: ipc.CredentialState_CREDENTIAL_STATE_VALID}}
			for _, peer := range []local.Peer{owner, {Identity: "uid:2000"}} {
				snapshot, err := m.snapshotAs(peer, nil)
				if err != nil {
					t.Fatal(err)
				}
				got := snapshot.Status.Session
				if peer != owner || scenario == "foreign_origin" || scenario == "foreign_token" || scenario == "absent" {
					if got != nil {
						t.Fatal("unbound or private session became visible")
					}
					continue
				}
				want := ipc.SessionState_SESSION_STATE_ACTIVE
				if scenario == "expired" {
					want = ipc.SessionState_SESSION_STATE_EXPIRED
				}
				if scenario == "logout" {
					want = ipc.SessionState_SESSION_STATE_NOT_AUTHENTICATED
				}
				if got.GetState() != want {
					t.Fatal("snapshot used stale or credential-derived session state")
				}
			}
		})
	}
}
