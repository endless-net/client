package client

import (
	"context"
	"reflect"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Private journal: registration authority and rollback context never enter an
// observable operation. No public admission until the handover worker is wired.
type clientRPCNetworkSelection struct {
	OperationID string           `json:"operation_id"`
	NetworkID   string           `json:"network_id"`
	Profile     clientRPCProfile `json:"profile"`
	Source      Config           `json:"source"`
	Target      *Config          `json:"target,omitempty"`
}

func networkSelectionContext(cfg Config) Config {
	cfg.RPCState = nil
	return clonePersistentConfig(cfg)
}

func (m *ClientRPCMutations) beginNetworkSelectionAs(peer local.Peer, request *ipc.SelectNetworkRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/SelectNetwork", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if request.NetworkId == "" || len(request.NetworkId) > 256 || !utf8.ValidString(request.NetworkId) {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if profile.ID != cfg.RPCState.ActiveProfileID || request.NetworkId == cfg.NetworkID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.RPCState.NetworkSelection != nil || cfg.RPCState.ProfileSwitch != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		if cfg.NodeID == "" || cfg.NetworkID == "" || cfg.NodeCredential == "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
		}
		if cfg.Token == "" || cfg.ActiveAccountID == "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
		}
		op.ProfileId = profile.ID
		cfg.RPCState.NetworkSelection = &clientRPCNetworkSelection{OperationID: op.Id, NetworkID: request.NetworkId, Profile: profile, Source: networkSelectionContext(*cfg)}
		return nil
	}, false)
	return op, err
}

// ReconcileNetworkSelectionPreparation checkpoints fresh catalog authorization
// and an isolated candidate. Source changes invalidate the checkpoint, including
// intent/owner/profile changes while the provider runs. There are no OS effects.
func (m *ClientRPCMutations) ReconcileNetworkSelectionPreparation(ctx context.Context, provider ClientRPCNetworksProvider) error {
	m.networkSelectionWorker.Lock()
	defer m.networkSelectionWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.NetworkSelection == nil {
		return nil
	}
	plan := cfg.RPCState.NetworkSelection
	if plan.Target != nil {
		return nil
	}
	if !networkSelectionSourceMatches(cfg, plan) {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	target, err := PrepareNetworkSelectionTarget(ctx, cfg, plan.NetworkID, provider)
	if err != nil {
		return err
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending := current.RPCState.NetworkSelection
		if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.ProfileId != plan.Profile.ID ||
			pending == nil || !reflect.DeepEqual(pending, plan) || !networkSelectionSourceMatches(*current, plan) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		pending.Target = &target
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	return err
}

func networkSelectionSourceMatches(cfg Config, plan *clientRPCNetworkSelection) bool {
	return cfg.RPCState != nil && cfg.RPCState.ActiveProfileID == plan.Profile.ID &&
		reflect.DeepEqual(cfg.RPCState.Profiles[plan.Profile.ID], plan.Profile) &&
		reflect.DeepEqual(networkSelectionContext(cfg), plan.Source)
}
