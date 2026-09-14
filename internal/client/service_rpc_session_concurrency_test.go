package client

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionRenewalConflictsAndIndependentDisconnect(t *testing.T) {
	for _, scenario := range []string{"renewal_first", "logout_first", "switch_first", "disconnect_during_renewal"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			create := rpcCreateRequest(t, m)
			create.ControlOrigin = "https://target.test"
			target, err := m.createProfileAs(owner, create)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = strings.Repeat("access-", 8)
				response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("grant-", 8), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			logout := func() error {
				_, err := m.logoutAs(owner, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
				return err
			}
			switchProfile := func() error {
				_, err := m.selectProfileAs(owner, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: target.ProfileId}})
				return err
			}
			renew := func() (*ipc.Operation, error) {
				return m.renewSessionAs(owner, &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			}
			if scenario == "logout_first" || scenario == "switch_first" {
				if scenario == "logout_first" {
					err = logout()
				} else {
					err = switchProfile()
				}
				if err != nil {
					t.Fatal(err)
				}
				before := m.store.Read()
				_, err = renew()
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("conflicting renewal changed accepted operation")
				}
				return
			}
			op, err := renew()
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "renewal_first" {
				before := m.store.Read()
				assertRPCFailure(t, logout(), ipc.ErrorCode_ERROR_CODE_BUSY)
				assertRPCFailure(t, switchProfile(), ipc.ErrorCode_ERROR_CODE_BUSY)
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("rejected conflict changed renewal")
				}
				return
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				finished <- m.ReconcileSessionRenewal(ctx, ClientRPCSessionRenewalProvider{
					Renew: func(ctx context.Context, _, _ string, _ *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
						return &backend.RenewSessionResponse{Operation: &backend.SessionRenewal{OperationId: "backend-operation", RequestId: op.RequestId, PreviousSessionId: "session", ReplayExpiresAt: timestamppb.New(m.now().Add(time.Hour)), State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, Outcome: &backend.SessionRenewal_Result{Result: &backend.SessionRenewalResult{AccessBearer: strings.Repeat("rotated-", 8), Session: &backend.UserSession{SessionId: "renewed-session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE}}}}}, nil
					},
					Poll: func(context.Context, string, string, *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
						t.Error("unexpected poll")
						return nil, nil
					},
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("provider not entered")
			}
			before := m.store.Read()
			if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
				t.Fatal(err)
			}
			stopped := make(chan error, 1)
			go func() {
				stopped <- m.ReconcileDisconnect(ctx, ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
				}})
			}()
			select {
			case err := <-stopped:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("renewal blocked Disconnect")
			}
			close(release)
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("renewal did not finish")
			}
			cfg := m.store.Read()
			if cfg.Token != strings.Repeat("rotated-", 8) || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.NodeID != before.NodeID || cfg.NodeCredential != before.NodeCredential {
				t.Fatal("renewal undid disconnect or changed node authority")
			}
		})
	}
}
