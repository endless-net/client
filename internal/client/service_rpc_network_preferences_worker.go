package client

import (
	"context"
	"reflect"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func (m *ClientRPCMutations) networkPreferenceCandidate(cfg Config, plan *clientRPCNetworkPreferenceChange) (Config, error) {
	stale := func() (Config, error) {
		return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if plan == nil || cfg.RPCState == nil || cfg.RPCState.ActiveProfileID != plan.ProfileID || cfg.LocalOwnerID != plan.OwnerID || cfg.NodeID != plan.NodeID || cfg.NetworkID != plan.NetworkID || cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || cfg.CachedMap.MapSignature.PayloadHash != plan.MapHash || cfg.MapRevision != cfg.CachedMap.Network.Revision || cfg.MapGlobalRevision != cfg.CachedMap.Revision.Global {
		return stale()
	}
	profile, exists := cfg.RPCState.Profiles[plan.ProfileID]
	if !exists || profile.ControlOrigin != plan.ControlOrigin || !reflect.DeepEqual(cfg.NetworkPreferences, plan.Previous) || !reflect.DeepEqual(profile.UIQuit, plan.PreviousUIQuit) || !reflect.DeepEqual(cfg.ConnectionIntent, plan.PreviousIntent) {
		return stale()
	}
	cfg.NetworkPreferences = cloneNetworkPreferences(plan.Requested)
	if _, err := resolveNetworkAcceptance(cfg, *cfg.CachedMap, m.now()); err != nil {
		return Config{}, err
	}
	profile.UIQuit = cloneLifecycleBehavior(plan.RequestedUIQuit)
	if _, err := m.uiQuitSetting(cfg, profile); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ReconcileNetworkPreferences shares the agent's driver lock. A crash while
// applying retries the authenticated candidate; a failed or superseded apply
// durably enters containment before Down. Down failure leaves that plan pending
// for recovery, with disconnected intent preventing automatic reapplication.
func (m *ClientRPCMutations) ReconcileNetworkPreferences(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Start == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkPreferenceChange == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkPreferenceChange
	id := plan.OperationID
	if _, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if cfg.RPCState.NetworkPreferenceChange == nil || cfg.RPCState.NetworkPreferenceChange.OperationID != id || op.ProfileId != plan.ProfileID || (op.Kind != ipc.OperationKind_OPERATION_KIND_SET_PREFERENCES && op.Kind != ipc.OperationKind_OPERATION_KIND_RESET_PREFERENCES) || (op.State != ipc.OperationState_OPERATION_STATE_PENDING && op.State != ipc.OperationState_OPERATION_STATE_RUNNING) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	}); err != nil {
		return err
	}
	if !plan.Containing {
		candidate, err := m.networkPreferenceCandidate(m.store.Read(), plan)
		if err == nil && candidate.ConnectionIntent != nil && candidate.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected {
			err = m.applyProfileConnection(ctx, driver, candidate)
		} else if err == nil {
			_, err = driver.Stop(ctx)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			_, err = m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
				if _, err := m.networkPreferenceCandidate(*cfg, plan); err != nil {
					return err
				}
				cfg.NetworkPreferences = cloneNetworkPreferences(plan.Requested)
				profile := cfg.RPCState.Profiles[plan.ProfileID]
				profile.UIQuit = cloneLifecycleBehavior(plan.RequestedUIQuit)
				cfg.RPCState.Profiles[profile.ID] = profile
				cfg.RPCState.NetworkPreferenceChange = nil
				op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
				op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
				op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: plan.Changed}}
				return nil
			})
			if err == nil {
				return nil
			}
		}
		if _, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
			if cfg.RPCState.NetworkPreferenceChange == nil || cfg.RPCState.NetworkPreferenceChange.OperationID != id {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			cfg.RPCState.NetworkPreferenceChange.Containing = true
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "preferences_apply_failed", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
			return nil
		}); err != nil {
			return err
		}
	}
	if _, err := driver.Stop(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		cfg.RPCState.NetworkPreferenceChange = nil
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "preferences_apply_contained"}}
		return nil
	})
	return err
}
