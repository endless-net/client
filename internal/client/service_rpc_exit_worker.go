package client

import (
	"context"
	"slices"
	"strings"
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
	if executor.Lock == nil || executor.Apply == nil || executor.Contain == nil || executor.Release == nil || strings.TrimSpace(executor.InterfaceName) != executor.InterfaceName || !safeWireGuardInterfaceName(executor.InterfaceName) || executor.InterfaceName == "lo" {
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
	workerCtx, cancelWorker := context.WithCancel(ctx)
	w := &clientRPCProfileWorker{ctx: workerCtx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.exitWorker = w
	s.exitModes = slices.Clone(executor.Modes)
	observationCtx, cancelObservation := context.WithCancel(workerCtx)
	if executor.Observe != nil {
		s.exitObservation = &clientRPCExitObservationSource{ctx: observationCtx, lock: executor.Lock, observe: executor.Observe}
	}
	observationSource := s.exitObservation
	// Catalog/control readiness changed independently of enforcement evidence.
	// Rebootstrap existing streams without claiming a production capability.
	s.mutations.mu.Lock()
	s.mutations.invalidateStreamsLocked()
	s.mutations.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		var result error
		stopMaintenance := s.mutations.startExitMaintenance(workerCtx, executor)
		stopObservationEvents := s.startExitObservationEvents(workerCtx, observationSource)
		defer func() {
			cancelWorker()
			stopMaintenance()
			cancelObservation()
			stopObservationEvents()
			s.exitMu.Lock()
			if s.exitWorker == w {
				s.exitWorker = nil
				s.exitModes = nil
				s.exitObservation = nil
				s.mutations.mu.Lock()
				s.mutations.invalidateStreamsLocked()
				s.mutations.mu.Unlock()
			}
			s.exitMu.Unlock()
			close(w.done)
			done <- result
			close(done)
		}()
		for {
			result = s.mutations.reconcileExitChange(workerCtx, executor)
			if result == nil {
				result = s.mutations.reconcileSavedExit(workerCtx, executor)
			}
			if workerCtx.Err() != nil {
				result = workerCtx.Err()
				return
			}
			if result != nil && !rpcFailureTemporary(result) {
				return
			}
			// Pending work has a durable retry deadline checked by reconciliation.
			// A bounded scan also catches context changes and containment without
			// relying on the original caller (or another worker) to send a wake.
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-workerCtx.Done():
				timer.Stop()
				result = workerCtx.Err()
				return
			case <-w.wake:
			case <-timer.C:
			}
			timer.Stop()
		}
	}()
	return done, nil
}
