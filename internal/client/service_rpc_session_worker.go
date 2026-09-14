package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Session approval must not hold the tunnel/profile worker while waiting for a
// user or the control plane. Shutdown joins this worker before closing the host.
func (s *ClientRPCService) StartSessionWorker(ctx context.Context) (<-chan error, error) {
	provider := s.SessionRenewalProvider
	if provider.Renew == nil || provider.Poll == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessionWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.sessionWorker = w
	s.mutations.setWorkerCapabilities(w, true, ipc.Capability_CAPABILITY_SESSION_RENEWAL)
	clear := func() { s.mutations.setWorkerCapabilities(w, false, ipc.Capability_CAPABILITY_SESSION_RENEWAL) }
	stopReadiness := context.AfterFunc(ctx, clear)
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			stopReadiness()
			clear()
			s.sessionMu.Lock()
			s.sessionWorker = nil
			s.sessionMu.Unlock()
			close(w.done)
			done <- err
			close(done)
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if err = s.mutations.ReconcileSessionRenewal(ctx, provider); err != nil {
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

func (s *ClientRPCService) RenewSession(ctx context.Context, request *connect.Request[ipc.RenewSessionRequest]) (*connect.Response[ipc.RenewSessionResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.admitSessionRenewal(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.RenewSessionResponse{Operation: op}), nil
}

func (s *ClientRPCService) admitSessionRenewal(ctx context.Context, peer local.Peer, request *ipc.RenewSessionRequest) (*ipc.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/RenewSession"), s.mutations.store.Read()); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	w := s.sessionWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.renewSessionAs(peer, request)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return op, nil
}
