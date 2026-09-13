package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// StartEnrollmentWorker owns registration independently of tunnel application:
// a slow approval/control request must not hold up an accepted Disconnect.
// Start before serving and await completion on shutdown. Unexpected errors must
// stop the host listener; a new worker resumes the durable plan automatically.
func (s *ClientRPCService) StartEnrollmentWorker(ctx context.Context, provider ClientRPCEnrollmentProvider) (<-chan error, error) {
	if provider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.enrollmentMu.Lock()
	defer s.enrollmentMu.Unlock()
	if s.enrollmentWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.enrollmentWorker = w
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			s.enrollmentMu.Lock()
			s.enrollmentWorker = nil
			s.enrollmentMu.Unlock()
			close(w.done)
			done <- err
			close(done)
		}()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if err = s.mutations.ReconcileEnrollment(ctx, provider); err != nil {
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

func (s *ClientRPCService) Enroll(ctx context.Context, request *connect.Request[ipc.EnrollRequest]) (*connect.Response[ipc.EnrollResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.enrollmentMu.Lock()
	defer s.enrollmentMu.Unlock()
	w := s.enrollmentWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.enrollAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.EnrollResponse{Operation: op}), nil
}
