package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

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
