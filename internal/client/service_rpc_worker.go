package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type clientRPCProfileWorker struct {
	ctx  context.Context
	wake chan struct{}
}

// StartProfileWorker must precede serving the listener. The caller owns ctx and
// must await done on shutdown. Unexpected errors require the host to stop serving
// and recover; unfinished durable operations are resumed by the next worker.
// Request cancellation never cancels accepted work.
func (s *ClientRPCService) StartProfileWorker(ctx context.Context, driver ClientRPCProfileDriver) (<-chan error, error) {
	if driver.Lock == nil || driver.Stop == nil || driver.Start == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	if s.profileWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1)}
	s.profileWorker = w
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			s.profileMu.Lock()
			s.profileWorker = nil
			s.profileMu.Unlock()
			done <- err
			close(done)
		}()
		for {
			// Also reconcile once at startup: no request needs to be replayed
			// to recover an accepted operation after a process crash.
			if err = s.mutations.ReconcileProfileSwitch(ctx, driver); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case <-w.wake:
			}
		}
	}()
	return done, nil
}

func (s *ClientRPCService) SelectProfile(ctx context.Context, request *connect.Request[ipc.SelectProfileRequest]) (*connect.Response[ipc.SelectProfileResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	// Keep acceptance and wakeup serialized with worker shutdown. A process
	// crash at either boundary is covered by the durable startup scan.
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.selectProfileAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.SelectProfileResponse{Operation: op}), nil
}
