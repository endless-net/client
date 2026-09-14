package client

import (
	"context"
	"reflect"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileNetworkSelectionRegistration runs only against a prepared target.
// Errors retain the private plan for retry/cleanup; registration readiness does
// not complete SelectNetwork or permit activation without verified handover.
func (m *ClientRPCMutations) ReconcileNetworkSelectionRegistration(ctx context.Context, provider ClientRPCNetworkRegistrationProvider) error {
	m.networkSelectionWorker.Lock()
	defer m.networkSelectionWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if provider == nil {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	if plan.Target == nil || plan.DownStarted || plan.AbortFailure != nil || !networkSelectionSourceMatches(cfg, plan) {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if plan.RegistrationReady && networkSelectionTargetReady(*plan.Target, m.now()) {
		return nil
	}
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.ProfileId != plan.Profile.ID || !reflect.DeepEqual(current.RPCState.NetworkSelection, plan) || !networkSelectionSourceMatches(*current, plan) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		current.RPCState.NetworkSelection.RegistrationReady = false
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		op.UserAction = nil
		return nil
	})
	if err != nil {
		return err
	}
	cfg = m.store.Read()
	plan = cfg.RPCState.NetworkSelection
	input := ClientRPCNetworkRegistrationInput{OperationID: plan.OperationID, NetworkID: plan.NetworkID}
	if state := plan.Source.CachedMap; state != nil {
		input.Hostname = state.Node.Hostname
		input.Tags = append([]string(nil), state.Node.Tags...)
	}
	save := m.NetworkSelectionSaveCallback(ctx, plan.OperationID, cfg)
	var checkpointErr error
	last := clonePersistentConfig(*plan.Target)
	action, executeErr := provider(ctx, last, input, func(next Config) error {
		if checkpointErr != nil {
			return checkpointErr
		}
		checkpointErr = save(next)
		if checkpointErr == nil {
			last = clonePersistentConfig(next)
		}
		return checkpointErr
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	if executeErr != nil {
		return executeErr
	}
	if action != nil && (action.Kind != ipc.UserAction_KIND_WAIT_FOR_APPROVAL || action.BrowserUrl != "") {
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	ready := networkSelectionTargetReady(last, m.now())
	expectedPlan := *plan
	expectedPlan.Target = &last
	if action == nil && !ready {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending := current.RPCState.NetworkSelection
		if pending == nil || !reflect.DeepEqual(pending, &expectedPlan) || !networkSelectionSourceMatches(*current, plan) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		pending.RegistrationReady = ready
		op.UserAction = nil
		if !ready {
			op.State = ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER
			op.UserAction = &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL, ReasonKey: "node_approval_pending"}
		}
		return nil
	})
	return err
}

func networkSelectionTargetReady(cfg Config, now time.Time) bool {
	state := cfg.CachedMap
	if state == nil || cfg.NodeID == "" || cfg.DeviceFingerprint == "" || cfg.NodeApprovalState != api.NodeApprovalApproved || cfg.PendingDirectRegistration != nil ||
		cfg.MapSigningTrust == nil || cfg.NodeCredentialSigningTrust == nil || state.MapSignature == nil || !now.Before(state.MapSignature.ExpiresAt) ||
		state.Node.ID != cfg.NodeID || state.Node.NetworkID != cfg.NetworkID || state.Network.ID != cfg.NetworkID || state.Network.AccountID != cfg.ActiveAccountID ||
		state.Network.Revision != cfg.MapRevision || state.Revision.Global != cfg.MapGlobalRevision || state.MapSignature.PayloadHash != cfg.MapHash ||
		(state.Node.ApprovalState != "" && state.Node.ApprovalState != api.NodeApprovalApproved) || api.ValidateNetworkMap(*state) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*state, *cfg.MapSigningTrust) != nil {
		return false
	}
	claims, err := api.VerifyNodeCredentialWithTrustBundle(cfg.NodeCredential, *cfg.NodeCredentialSigningTrust, "node:map", now)
	if err != nil || claims.NodeID != cfg.NodeID || claims.NetworkID != cfg.NetworkID {
		return false
	}
	public, err := wgkeys.PublicKey(cfg.PrivateKey)
	if err != nil || public != state.Node.PublicKey {
		return false
	}
	identity, err := IdentityPublicKey(cfg.IdentityPrivateKey)
	return err == nil && identity == state.Node.IdentityPublicKey && cfg.DeviceFingerprint == state.Node.DeviceFingerprint
}
