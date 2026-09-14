package client

import (
	"reflect"
	"strings"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func exitAppliedResultMatches(plan *clientRPCExitChange, status *ipc.ExitNodeStatus) bool {
	if plan == nil || status == nil || status.ProfileId != plan.ProfileID || status.ApplyState != ipc.ApplyState_APPLY_STATE_APPLIED || status.Failure != nil || status.Ipv4 == nil || status.Ipv6 == nil {
		return false
	}
	selection := plan.Requested
	if selection == nil {
		return status.RequestedExitNodeId == nil && status.EffectiveExitNodeId == nil && !status.FailClosed && status.RequestedFamilyMode == ipc.ExitFamilyMode_EXIT_FAMILY_MODE_NONE &&
			status.RequestedLanAccess == ipc.LanAccess_LAN_ACCESS_UNSPECIFIED && status.EffectiveLanAccess == ipc.LanAccess_LAN_ACCESS_UNSPECIFIED &&
			exitAppliedFamilyMatches(status.Ipv4, "", false) && exitAppliedFamilyMatches(status.Ipv6, "", false)
	}
	family := map[api.ExitFamilyMode]ipc.ExitFamilyMode{api.ExitFamilyIPv4Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, api.ExitFamilyIPv6Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, api.ExitFamilyDualStack: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[selection.Family]
	lan := map[api.ExitLANAccess]ipc.LanAccess{api.ExitLANBlock: ipc.LanAccess_LAN_ACCESS_BLOCK, api.ExitLANAllow: ipc.LanAccess_LAN_ACCESS_ALLOW}[selection.LAN]
	if family == ipc.ExitFamilyMode_EXIT_FAMILY_MODE_UNSPECIFIED || lan == ipc.LanAccess_LAN_ACCESS_UNSPECIFIED || selection.ID == "" {
		return false
	}
	if status.GetRequestedExitNodeId() != selection.ID || status.GetEffectiveExitNodeId() != selection.ID || !status.FailClosed || status.RequestedFamilyMode != family || status.RequestedLanAccess != lan || status.EffectiveLanAccess != lan {
		return false
	}
	return exitAppliedFamilyMatches(status.Ipv4, selection.ID, selection.Family != api.ExitFamilyIPv6Only) && exitAppliedFamilyMatches(status.Ipv6, selection.ID, selection.Family != api.ExitFamilyIPv4Only)
}

func exitAppliedFamilyMatches(status *ipc.ExitFamilyStatus, id string, enabled bool) bool {
	if status == nil || status.ApplyState != ipc.ApplyState_APPLY_STATE_APPLIED || status.Failure != nil {
		return false
	}
	if !enabled {
		return status.RequestedExitNodeId == nil && status.EffectiveExitNodeId == nil && !status.FailClosed
	}
	return status.GetRequestedExitNodeId() == id && status.GetEffectiveExitNodeId() == id && status.FailClosed
}

// Only a trusted executor may supply observed OS enforcement. Requested routes
// or an empty error alone cannot satisfy the per-family result contract.
func (m *ClientRPCMutations) completeExitChange(id string, observed *ipc.ExitNodeStatus, continuity ipc.ConnectionContinuity) (*ipc.Operation, error) {
	return m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
		plan := cfg.RPCState.ExitChange
		if plan == nil || plan.OperationID != id || plan.ProfileID != op.ProfileId || plan.ProfileID != cfg.RPCState.ActiveProfileID ||
			!strings.EqualFold(plan.OwnerID, cfg.LocalOwnerID) || plan.NodeID != cfg.NodeID || plan.NetworkID != cfg.NetworkID || plan.ControlOrigin != cfg.RPCState.Profiles[plan.ProfileID].ControlOrigin ||
			!reflect.DeepEqual(plan.Previous, cfg.ExitSelection) || op.State != ipc.OperationState_OPERATION_STATE_RUNNING {
			return stale()
		}
		if (plan.Requested == nil && op.Kind != ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE) || (plan.Requested != nil && op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE) {
			return stale()
		}
		if !exitAppliedResultMatches(plan, observed) {
			return stale()
		}
		if plan.Requested != nil {
			if cfg.CachedMap == nil || cfg.CachedMap.Network.Revision != cfg.MapRevision || cfg.CachedMap.Revision.Global != cfg.MapGlobalRevision {
				return stale()
			}
			if _, err := exitRoutePeers(*cfg, *cfg.CachedMap, plan.Requested, m.now()); err != nil {
				return stale()
			}
		}
		cfg.ExitSelection = cloneExitSelection(plan.Requested)
		cfg.RPCState.ExitChange = nil
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.UserAction = nil
		op.Continuity = continuity
		result := &ipc.SelectionResult{}
		if plan.Requested != nil {
			result.SelectedId = plan.Requested.ID
		}
		op.Outcome = &ipc.Operation_Selection{Selection: result}
		return nil
	})
}
