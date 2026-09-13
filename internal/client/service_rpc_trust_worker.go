package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Trust has its own worker: public inspection and credential renewal must not
// hold up the profile worker's Disconnect. Only adoption takes the tunnel lock.
func (s *ClientRPCService) StartTrustWorker(ctx context.Context, driver ClientRPCProfileDriver) (<-chan error, error) {
	if s.ServerIdentityProvider == nil || s.TrustRecoveryProvider == nil || driver.Lock == nil || driver.Stop == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.trustMu.Lock()
	defer s.trustMu.Unlock()
	if s.trustWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.trustWorker = w
	s.mutations.setWorkerCapabilities(w, true, ipc.Capability_CAPABILITY_IDENTITY_RECOVERY)
	clearReadiness := func() { s.mutations.setWorkerCapabilities(w, false, ipc.Capability_CAPABILITY_IDENTITY_RECOVERY) }
	stopReadiness := context.AfterFunc(ctx, clearReadiness)
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			stopReadiness()
			clearReadiness()
			s.trustMu.Lock()
			s.trustWorker = nil
			s.trustMu.Unlock()
			close(w.done)
			done <- err
			close(done)
		}()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if err = s.mutations.ReconcileTrustAnnouncement(ctx, s.ServerIdentityProvider); err != nil {
				return
			}
			if err = s.mutations.ReconcileTrustAdoption(ctx, driver); err != nil {
				return
			}
			if err = s.mutations.ReconcileTrustRecovery(ctx, s.TrustRecoveryProvider); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case <-w.wake:
			case <-ticker.C:
			}
		}
	}()
	return done, nil
}

func (s *ClientRPCService) TrustServerIdentity(ctx context.Context, request *connect.Request[ipc.TrustServerIdentityRequest]) (*connect.Response[ipc.TrustServerIdentityResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.trustMu.Lock()
	defer s.trustMu.Unlock()
	w := s.trustWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.trustServerIdentityAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.TrustServerIdentityResponse{Operation: op}), nil
}
