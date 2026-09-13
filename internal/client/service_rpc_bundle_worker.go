package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func (s *ClientRPCService) startBundleWorker(ctx context.Context) (<-chan error, error) {
	if s.bundleStore == nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.bundleMu.Lock()
	defer s.bundleMu.Unlock()
	if s.bundleWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.bundleWorker = w
	done := make(chan error, 1)
	go func() {
		var err error
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		defer func() {
			s.bundleMu.Lock()
			s.bundleWorker = nil
			s.bundleMu.Unlock()
			close(w.done)
			done <- err
			close(done)
		}()
		for {
			if err = s.purgeBundles(); err != nil {
				return
			}
			if err = s.reconcileBundles(ctx); err != nil {
				return
			}
			if err = s.purgeBundles(); err != nil {
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

func (s *ClientRPCService) CreateDiagnosticsBundle(ctx context.Context, request *connect.Request[ipc.CreateDiagnosticsBundleRequest]) (*connect.Response[ipc.CreateDiagnosticsBundleResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.bundleMu.Lock()
	defer s.bundleMu.Unlock()
	w := s.bundleWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if s.DiagnosticsProvider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	op, err := s.mutations.createBundleAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.CreateDiagnosticsBundleResponse{Operation: op}), nil
}
