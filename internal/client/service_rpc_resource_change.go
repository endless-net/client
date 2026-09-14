package client

import (
	"maps"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func (m *ClientRPCMutations) setResourceEnabledAs(peer local.Peer, request *ipc.SetResourceEnabledRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/SetResourceEnabled", request, func(cfg *Config, op *ipc.Operation) error {
		if request.ResourceId == "" || len(request.ResourceId) > 2048 || len(request.ProtoReflect().GetUnknown()) != 0 {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		// Shared preparation validates active context, signature and conflicts,
		// and snapshots all configuration domains without applying any of them.
		if err := m.prepareNetworkPreferences(cfg, op, request.Profile, nil, nil); err != nil {
			return err
		}
		setting, err := resolveResourcePreference(*cfg, request.ResourceId, m.now())
		if err != nil {
			return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		if setting.Managed != nil && setting.Managed.Locked && setting.Managed.Enabled != request.Enabled {
			return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
		}
		plan := cfg.RPCState.NetworkPreferenceChange
		plan.Requested = cloneNetworkPreferences(plan.Previous)
		plan.ResourceID = request.ResourceId
		if plan.RequestedResources == nil {
			plan.RequestedResources = make(map[string]bool)
		}
		plan.RequestedResources[request.ResourceId] = request.Enabled
		plan.Changed = !maps.Equal(plan.PreviousResources, plan.RequestedResources)
		candidate := *cfg
		candidate.ResourcePreferences = plan.RequestedResources
		if _, err := compileResourceDenials(candidate, m.now()); err != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		return nil
	})
	return op, err
}
