package client

import (
	"context"
	"reflect"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type ClientRPCNetworkSelectionProviders struct {
	Networks ClientRPCNetworksProvider
	Register ClientRPCNetworkRegistrationProvider
	Cleanup  ClientRPCNetworkTargetCleanupProvider
}

// ReconcileNetworkSelection dispatches by durable phase; it never starts
// registration again after teardown, activation or a recorded abort.
func (m *ClientRPCMutations) ReconcileNetworkSelection(ctx context.Context, driver ClientRPCProfileDriver, providers ClientRPCNetworkSelectionProviders) error {
	m.networkCoordinator.Lock()
	defer m.networkCoordinator.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	if plan.AbortFailure != nil {
		return m.ReconcileNetworkSelectionAbort(ctx, driver, providers.Cleanup, plan.AbortFailure.Code)
	}
	if plan.Activated {
		return m.ReconcileNetworkSelectionApply(ctx, driver)
	}
	if !networkSelectionSourceMatches(cfg, plan) {
		code := ipc.ErrorCode_ERROR_CODE_STALE_STATE
		if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredDisconnected && !reflect.DeepEqual(cfg.ConnectionIntent, plan.Source.ConnectionIntent) {
			code = ipc.ErrorCode_ERROR_CODE_CANCELLED
		}
		return m.ReconcileNetworkSelectionAbort(ctx, driver, providers.Cleanup, code)
	}
	var err error
	switch {
	case plan.Target == nil:
		err = m.ReconcileNetworkSelectionPreparation(ctx, providers.Networks)
	case plan.DownStarted || (plan.RegistrationReady && networkSelectionTargetReady(*plan.Target, m.now())):
		err = m.ReconcileNetworkSelectionActivation(ctx, driver)
	default:
		err = m.ReconcileNetworkSelectionRegistration(ctx, providers.Register)
	}
	if err == nil || ctx.Err() != nil {
		return err
	}
	failure := rpc.FailureFromError(err)
	if failure == nil {
		return err // Unexpected failures stop the host with its journal intact.
	}
	switch failure.Code {
	case ipc.ErrorCode_ERROR_CODE_CANCELLED, ipc.ErrorCode_ERROR_CODE_STALE_STATE, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED, ipc.ErrorCode_ERROR_CODE_APPROVAL_REJECTED, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED:
		return m.ReconcileNetworkSelectionAbort(ctx, driver, providers.Cleanup, failure.Code)
	case ipc.ErrorCode_ERROR_CODE_UNAVAILABLE:
		return nil // Retry the same durable registration/phase, never a new ID.
	default:
		return err
	}
}

// StartNetworkSelectionWorker resumes accepted operations before any request
// replay. Host lifetime, not the accepting request, owns registration and apply.
// Readiness is not advertised until public admission is wired and verified.
func (s *ClientRPCService) StartNetworkSelectionWorker(ctx context.Context, driver ClientRPCProfileDriver) (<-chan error, error) {
	providers := s.NetworkSelectionProviders
	if driver.Lock == nil || driver.Stop == nil || driver.Start == nil || providers.Networks == nil || providers.Register == nil || providers.Cleanup == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	if s.networkWorker != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	w := &clientRPCProfileWorker{ctx: ctx, wake: make(chan struct{}, 1), done: make(chan struct{})}
	s.networkWorker = w
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			s.networkMu.Lock()
			s.networkWorker = nil
			s.networkMu.Unlock()
			close(w.done)
			done <- err
			close(done)
		}()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			before := s.mutations.store.Read()
			if err = s.mutations.ReconcileNetworkSelection(ctx, driver, providers); err != nil {
				return
			}
			after := s.mutations.store.Read()
			// Advance newly completed phases immediately. Approval polling and
			// transient failures wait even when they update operation metadata.
			if networkSelectionPhaseAdvanced(before, after) {
				continue
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

func networkSelectionPhaseAdvanced(before, after Config) bool {
	if before.RPCState == nil || before.RPCState.NetworkSelection == nil || after.RPCState == nil || after.RPCState.NetworkSelection == nil {
		return false
	}
	a, b := before.RPCState.NetworkSelection, after.RPCState.NetworkSelection
	return (a.Target == nil && b.Target != nil) || (!a.RegistrationReady && b.RegistrationReady) || (!a.Activated && b.Activated)
}
