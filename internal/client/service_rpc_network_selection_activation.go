package client

import (
	"context"
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileNetworkSelectionActivation removes source routes before atomically
// adopting the registered target. DownStarted excludes automatic reconciliation
// until the owning worker finishes apply/cleanup. Activation is not completion.
func (m *ClientRPCMutations) ReconcileNetworkSelectionActivation(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.networkSelectionWorker.Lock()
	defer m.networkSelectionWorker.Unlock()
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
	if plan.Activated {
		if plan.Target == nil || !reflect.DeepEqual(networkSelectionContext(cfg), *plan.Target) {
			return stale()
		}
		return nil
	}
	if plan.Target == nil || !plan.RegistrationReady || !networkSelectionSourceMatches(cfg, plan) || !networkSelectionTargetReady(*plan.Target, m.now()) {
		return stale()
	}
	resuming := plan.DownStarted
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || op.ProfileId != plan.Profile.ID ||
			!reflect.DeepEqual(current.RPCState.NetworkSelection, plan) || !networkSelectionSourceMatches(*current, plan) {
			return stale()
		}
		current.RPCState.NetworkSelection.DownStarted = true
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		return nil
	})
	if err != nil {
		return err
	}
	plan.DownStarted = true
	continuity, err := driver.Stop(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err // Keep the barrier: an error does not confirm route removal.
	}
	if continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if resuming && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || op.ProfileId != plan.Profile.ID ||
			!reflect.DeepEqual(current.RPCState.NetworkSelection, plan) || !networkSelectionSourceMatches(*current, plan) || !networkSelectionTargetReady(*plan.Target, m.now()) {
			return stale()
		}
		next := clonePersistentConfig(*plan.Target)
		// Preserve requested connectivity, but no source network preferences,
		// resources, exit selection, map or node authority cross this boundary.
		next.ConnectionIntent = clonePersistentConfig(plan.Source).ConnectionIntent
		if next.ConnectionIntent != nil {
			next.ConnectionIntent.StartupRecovery = nil
		}
		state := current.RPCState
		profile := state.Profiles[plan.Profile.ID]
		profile.Configuration = profileConfiguration(next)
		state.Profiles[profile.ID] = profile
		state.NetworkSelection.Activated = true
		activated := networkSelectionContext(next)
		state.NetworkSelection.Target = &activated
		next.RPCState = state
		*current = next
		op.Continuity = continuity
		return nil
	})
	return err
}
