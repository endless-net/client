package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNetworkAbortBeforePreparationPreservesSource(t *testing.T) {
	m, owner, request := networkSelectionPlanFixture(t)
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	source := networkSelectionContext(m.store.Read())
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Fatal("unstarted selection stopped source")
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, nil, ipc.ErrorCode_ERROR_CODE_CANCELLED); err != nil {
		t.Fatal(err)
	}
	result := networkApplyOperation(t, m, op.Id)
	if result.State != ipc.OperationState_OPERATION_STATE_CANCELLED || result.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED ||
		m.store.Read().RPCState.NetworkSelection != nil || !reflect.DeepEqual(networkSelectionContext(m.store.Read()), source) {
		t.Fatal("preparation cancellation lost source or terminal outcome")
	}
	if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, nil, ipc.ErrorCode_ERROR_CODE_CANCELLED); err != nil {
		t.Fatal("terminal replay repeated work", err)
	}
}

func TestNetworkAbortCheckpointsRecoveredTargetAfterSourceChange(t *testing.T) {
	m, target := networkRegistrationExecutorFixture(t)
	id := m.store.Read().RPCState.NetworkSelection.OperationID
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "new_disconnect"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := networkSelectionContext(m.store.Read())
	cleanupCalls := 0
	provider := func(_ context.Context, cfg Config, input ClientRPCNetworkRegistrationInput, save func(Config) error) error {
		cleanupCalls++
		if input.OperationID != id || input.NetworkID != target.NetworkID || cfg.NodeID != "" {
			t.Fatal("cleanup lost isolated registration binding")
		}
		if err := save(target); err != nil {
			return err
		}
		if m.store.Read().RPCState.NetworkSelection.Target.NodeCredential != target.NodeCredential || !reflect.DeepEqual(networkSelectionContext(m.store.Read()), source) {
			t.Fatal("recovered authority was not checkpointed privately")
		}
		return nil
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Fatal("cleanup of isolated node stopped active source")
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, provider, ipc.ErrorCode_ERROR_CODE_CANCELLED); err != nil {
		t.Fatal(err)
	}
	if cleanupCalls != 1 || networkApplyOperation(t, m, id).State != ipc.OperationState_OPERATION_STATE_CANCELLED || !reflect.DeepEqual(networkSelectionContext(m.store.Read()), source) {
		t.Fatal("cleanup changed newer source intent or lost cancellation")
	}
}

func TestNetworkAbortPersistsRevocationBeforeRetryingLocalStop(t *testing.T) {
	m, _ := readyNetworkActivationFixture(t)
	id := m.store.Read().RPCState.NetworkSelection.OperationID
	if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.NetworkSelection.DownStarted = true; return nil }); err != nil {
		t.Fatal(err)
	}
	cleanupCalls, stops := 0, 0
	provider := func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error {
		cleanupCalls++
		return nil
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		if stops == 1 {
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("synthetic down failure")
		}
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, provider, ipc.ErrorCode_ERROR_CODE_STALE_STATE); err == nil {
		t.Fatal("failed stop reported terminal cancellation")
	}
	plan := m.store.Read().RPCState.NetworkSelection
	if plan == nil || !plan.TargetRevoked || !plan.DownStarted || plan.AbortFailure == nil || networkApplyOperation(t, m, id).State != ipc.OperationState_OPERATION_STATE_RUNNING {
		t.Fatal("cleanup progress or barrier missing")
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, provider, ipc.ErrorCode_ERROR_CODE_CANCELLED); err != nil {
		t.Fatal(err)
	}
	op := networkApplyOperation(t, m, id)
	if cleanupCalls != 1 || stops != 2 || op.State != ipc.OperationState_OPERATION_STATE_FAILED || op.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_STALE_STATE || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN {
		t.Fatal("restart repeated remote revoke or changed the durable failure", op)
	}
}

func TestNetworkAbortRejectsActiveNodeAndRetainsUncertainCleanup(t *testing.T) {
	for _, mode := range []string{"active_node", "profile_node", "checkpoint_active_node", "remote_failure"} {
		t.Run(mode, func(t *testing.T) {
			m, target := networkRegistrationExecutorFixture(t)
			if mode == "active_node" {
				if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.NetworkSelection.Target.NodeID = cfg.NodeID; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "profile_node" {
				if err := m.store.Update(func(cfg *Config) error {
					profile := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
					profile.Configuration.NodeID = target.NodeID
					cfg.RPCState.Profiles[profile.ID] = profile
					cfg.RPCState.NetworkSelection.Target.NodeID = target.NodeID
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			provider := func(_ context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, save func(Config) error) error {
				calls++
				if mode == "checkpoint_active_node" {
					target.NodeID = m.store.Read().NodeID
					target.CachedMap = nil
					_ = save(target) // Ignoring checkpoint failure must not complete cleanup.
					return nil
				}
				return errors.New("synthetic uncertain remote result")
			}
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				t.Fatal("unconfirmed remote cleanup touched source routes")
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}}
			if err := m.ReconcileNetworkSelectionAbort(t.Context(), driver, provider, ipc.ErrorCode_ERROR_CODE_CANCELLED); err == nil {
				t.Fatal("unsafe cleanup accepted")
			}
			plan := m.store.Read().RPCState.NetworkSelection
			if plan == nil || plan.AbortFailure == nil || plan.TargetRevoked || ((mode == "active_node" || mode == "profile_node") && calls != 0) {
				t.Fatal("unsafe or uncertain target cleanup lost its journal")
			}
			if err := m.ReconcileNetworkSelectionRegistration(t.Context(), func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) (*ipc.UserAction, error) {
				t.Fatal("aborted selection resumed registration")
				return nil, nil
			}); err == nil {
				t.Fatal("registration ignored durable abort")
			}
			if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err == nil {
				t.Fatal("activation ignored durable abort")
			}
		})
	}
}
