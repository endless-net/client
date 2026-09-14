package client

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCForgetCancelsSessionRenewalAndDrainsBeforeCleanup(t *testing.T) {
	for _, scenario := range []string{"running", "queued_restart", "waiting_restart"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			owner.Administrator = true
			if err := m.store.Update(func(cfg *Config) error {
				cfg.Token = strings.Repeat("access-", 8)
				response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("grant-", 8), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}
				cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			renewal, err := m.renewSessionAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, exited := make(chan struct{}), make(chan struct{})
			finished := make(chan error, 1)
			provider := ClientRPCSessionRenewalProvider{
				Renew: func(ctx context.Context, _, _ string, _ *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
					defer close(exited)
					close(entered)
					<-ctx.Done()
					// A provider can return a result even after cancellation. It must
					// never revive the session or cause the host worker to fail.
					return &backend.RenewSessionResponse{Operation: &backend.SessionRenewal{OperationId: "late-operation", RequestId: renewal.RequestId, PreviousSessionId: "session", ReplayExpiresAt: timestamppb.New(m.now().Add(time.Hour)), State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED, Outcome: &backend.SessionRenewal_Result{Result: &backend.SessionRenewalResult{AccessBearer: strings.Repeat("late-access-", 8), Session: &backend.UserSession{SessionId: "late-session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE}}}}}, nil
				},
				Poll: func(context.Context, string, string, *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
					t.Error("cancelled approval polled")
					return nil, nil
				},
			}
			switch scenario {
			case "running":
				go func() { finished <- m.ReconcileSessionRenewal(ctx, provider) }()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("renewal did not dispatch")
				}
			case "waiting_restart":
				if _, err := m.ReconcileOperation(renewal.Id, func(_ *Config, op *ipc.Operation) error {
					op.State = ipc.OperationState_OPERATION_STATE_RUNNING
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				_, err := m.checkpointSessionBrowser(renewal.Id, &backend.RenewSessionResponse{PollAuthorization: strings.Repeat("poll-", 8), Operation: &backend.SessionRenewal{OperationId: "backend-operation", RequestId: renewal.RequestId, PreviousSessionId: "session", State: backend.SessionRenewalState_SESSION_RENEWAL_STATE_WAITING_FOR_USER, ReplayExpiresAt: timestamppb.New(m.now().Add(time.Hour)), PollAfterSeconds: 10, Outcome: &backend.SessionRenewal_BrowserAction{BrowserAction: &backend.SessionBrowserAction{ApprovalUrl: "https://control.test/approve", ExpiresAt: timestamppb.New(m.now().Add(time.Minute))}}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			forget := &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			if _, err := m.forgetEnrollmentAs(owner, forget); err == nil {
				t.Fatal("unconfirmed forget accepted")
			}
			if m.sessionRenewalCancellationRequested() {
				t.Fatal("rejected request persisted cancellation")
			}
			select {
			case <-exited:
				t.Fatal("rejected request stopped renewal")
			default:
			}
			forget.Confirmed = true
			cleanup, err := m.forgetEnrollmentAs(owner, forget)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "running" {
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				if !m.sessionRenewalCancellationRequested() {
					t.Fatal("restart lost cancellation marker")
				}
			}
			err = m.ReconcileDisconnect(ctx, ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				if scenario == "running" {
					select {
					case <-exited:
					default:
						t.Error("cleanup preceded provider drain")
					}
				}
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "running" {
				select {
				case err := <-finished:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("provider did not finish")
				}
			}
			cfg := m.store.Read()
			if cfg.Token != "" || cfg.UserSession != nil || cfg.RPCState.SessionRenewal != nil {
				t.Fatal("cleanup retained session authority")
			}
			replayed, err := m.renewSessionAs(owner, request)
			if err != nil || replayed.Id != renewal.Id || replayed.State != ipc.OperationState_OPERATION_STATE_CANCELLED || replayed.UserAction != nil {
				t.Fatal("cancelled renewal replay changed", err)
			}
			result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: cleanup.Id}})
			if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatal("forget did not complete", err)
			}
		})
	}
}
