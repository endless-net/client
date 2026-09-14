package client

import (
	"context"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// startExitWorker owns retries independently of request lifetime. It does not
// advertise readiness: native host wiring must first restore OS protection and
// provide a fully implemented adapter using the shared runtime effect lock.
// The host must cancel and join this worker before closing its engine.
func (s *ClientRPCService) startExitWorker(ctx context.Context, executor clientRPCExitExecutor) (<-chan error, error) {
	if executor.Lock == nil || executor.Apply == nil || executor.Contain == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
	if s.exitWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	executor.Modes = slices.Clone(executor.Modes)
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.exitWorker = w
	done := make(chan error, 1)
	go func() {
		var result error
		defer func() {
			s.exitMu.Lock()
			s.exitWorker = nil
			s.exitMu.Unlock()
			close(w.done)
			done <- result
			close(done)
		}()
		for {
			result = s.mutations.reconcileExitChange(ctx, executor)
			if ctx.Err() != nil {
				result = ctx.Err()
				return
			}
			if result != nil && !exitWorkerRetryable(result) {
				return
			}
			// Pending work has a durable retry deadline checked by reconciliation.
			// A bounded scan also catches context changes and containment without
			// relying on the original caller (or another worker) to send a wake.
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				result = ctx.Err()
				return
			case <-w.wake:
			case <-timer.C:
			}
			timer.Stop()
		}
	}()
	return done, nil
}

func exitWorkerRetryable(err error) bool {
	switch rpc.FailureFromError(err).GetCode() {
	case ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED:
		return true
	default:
		return false
	}
}
