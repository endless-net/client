package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionWorkerAdmissionReadinessAndShutdown(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.Token = strings.Repeat("access-", 8)
		response := &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, RenewalSupported: true}, RenewalAuthorization: &backend.SessionRenewalAuthorization{Bearer: strings.Repeat("grant-", 8), ExpiresAt: timestamppb.New(m.now().Add(time.Hour))}}
		cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: response, RenewalGrant: proto.Clone(response).(*backend.GetSessionResponse)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	request := &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
	_, err := s.admitSessionRenewal(t.Context(), owner, request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	if _, err := s.StartSessionWorker(t.Context()); err == nil {
		t.Fatal("missing provider accepted")
	}
	entered := make(chan struct{})
	s.SessionRenewalProvider = ClientRPCSessionRenewalProvider{
		Renew: func(ctx context.Context, _, _ string, _ *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		Poll: func(context.Context, string, string, *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
			t.Error("unexpected poll")
			return nil, errors.New("unexpected poll")
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done, err := s.StartSessionWorker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartSessionWorker(ctx); err == nil {
		t.Fatal("duplicate worker accepted")
	}
	project := func() *ipc.Session {
		m.mu.Lock()
		defer m.mu.Unlock()
		session := &ipc.Session{State: ipc.SessionState_SESSION_STATE_ACTIVE}
		cfg := m.store.Read()
		m.projectSessionRenewalLocked(session, cfg, profile.ProfileId)
		return session
	}
	if project().Renewal.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
		t.Fatal("ready worker with valid grant unavailable")
	}
	_, err = s.admitSessionRenewal(t.Context(), local.Peer{Identity: "uid:unrelated"}, request)
	if err == nil || m.store.Read().RPCState.SessionRenewal != nil {
		t.Fatal("observer admitted renewal")
	}
	op, err := s.admitSessionRenewal(t.Context(), owner, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not dispatch accepted operation")
	}
	replay, err := s.admitSessionRenewal(t.Context(), owner, request)
	if err != nil || replay.Id != op.Id {
		t.Fatal("replay lost accepted operation", err)
	}
	session := project()
	if session.State != ipc.SessionState_SESSION_STATE_RENEWING || session.RenewalOperationId != op.Id {
		t.Fatal("renewal identity absent from session")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker shutdown not joined")
	}
	m.mu.Lock()
	w := m.capabilityWorkers[ipc.Capability_CAPABILITY_SESSION_RENEWAL]
	m.mu.Unlock()
	if w != nil {
		t.Fatal("stopped worker still ready")
	}
	_, err = s.admitSessionRenewal(t.Context(), owner, request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	if m.store.Read().RPCState.SessionRenewal == nil {
		t.Fatal("shutdown lost resumable execution plan")
	}
}
