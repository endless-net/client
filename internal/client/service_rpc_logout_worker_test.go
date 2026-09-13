package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCDisconnectPreemptsSlowLogoutWithoutLosingProgress(t *testing.T) {
	m, peer, enroll := enrollmentAdmissionTest(t)
	if err := m.store.Update(func(cfg *Config) error { cfg.Token = "synthetic-session"; return nil }); err != nil {
		t.Fatal(err)
	}
	logout, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
	if err != nil {
		t.Fatal(err)
	}
	entered, exited, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		close(stopped)
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	},
		Logout: func(ctx context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
			defer close(exited)
			if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}); err != nil {
				return "", err
			}
			close(entered)
			<-ctx.Done()
			return "", ctx.Err()
		}}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	service := NewClientRPCService(m, nil)
	done, err := service.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("logout provider did not start")
	}
	request := &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}
	request.Mutation.ExpectedRevision++
	_, err = m.disconnectAs(peer, request)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	select {
	case <-exited:
		t.Fatal("rejected Disconnect cancelled logout")
	default:
	}
	request.Mutation.ExpectedRevision--
	disconnect, err := m.disconnectAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("Disconnect waited for remote logout")
	}
	select {
	case <-exited:
	default:
		t.Fatal("remote request did not yield")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: disconnect.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if result.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Disconnect did not persist completion")
		}
	}
	current, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: logout.Id}})
	if err != nil || current.State != ipc.OperationState_OPERATION_STATE_RUNNING || m.store.Read().RPCState.Logout == nil || !m.store.Read().RPCState.Logout.Progress.NodeRevoked || m.store.Read().Token == "" {
		t.Fatal("preemption discarded logout progress or session", err)
	}
}

func TestRPCLogoutWorkerResumesConfirmedNodeAfterShutdown(t *testing.T) {
	m, peer, enroll := enrollmentAdmissionTest(t)
	if err := m.store.Update(func(cfg *Config) error { cfg.Token = "synthetic-session"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	_, err := s.StartProfileWorker(t.Context(), driver)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	driver.Logout = func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
		if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}); err != nil {
			return "", err
		}
		cancel()
		return "", context.Canceled
	}
	done, err := s.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logout worker did not stop")
	}
	if m.store.Read().RPCState.Logout == nil || m.store.Read().Token == "" {
		t.Fatal("shutdown lost unfinished logout")
	}
	resumeCtx, stop := context.WithCancel(t.Context())
	defer stop()
	driver.Logout = func(_ context.Context, _ Config, progress ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
		if !progress.NodeRevoked || progress.SessionRevoked {
			t.Error("confirmed progress not resumed")
		}
		return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
	}
	done, err = s.StartProfileWorker(resumeCtx, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { stop(); <-done }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for m.store.Read().RPCState.Logout != nil {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("logout was not resumed")
		}
	}
	if m.store.Read().Token != "" {
		t.Fatal("resumed logout retained session")
	}
}
