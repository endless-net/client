package client

import (
	"slices"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type clientRPCExitChange struct {
	OperationID    string               `json:"operation_id"`
	ProfileID      string               `json:"profile_id"`
	OwnerID        string               `json:"owner_id"`
	ControlOrigin  string               `json:"control_origin"`
	NodeID         string               `json:"node_id"`
	NetworkID      string               `json:"network_id"`
	RouteTable     string               `json:"route_table"`
	MapHash        string               `json:"map_hash,omitempty"`
	Requested      *ClientExitSelection `json:"requested,omitempty"`
	Previous       *ClientExitSelection `json:"previous,omitempty"`
	PreviousIntent *ConnectionIntent    `json:"previous_intent,omitempty"`
	NextAttemptAt  time.Time            `json:"next_attempt_at,omitempty"`
	Containing     bool                 `json:"containing,omitempty"`
	FailureCode    ipc.ErrorCode        `json:"failure_code,omitempty"`
	FailureReason  string               `json:"failure_reason,omitempty"`
}

// Trusted executor support is a set of exact pairs, never a Cartesian product
// that could widen LAN permissions for another address-family implementation.
type clientRPCExitMode struct {
	Family api.ExitFamilyMode
	LAN    api.ExitLANAccess
}

func cloneExitSelection(selection *ClientExitSelection) *ClientExitSelection {
	if selection == nil {
		return nil
	}
	copy := *selection
	return &copy
}

func (m *ClientRPCMutations) prepareExitChange(cfg *Config, op *ipc.Operation, ref *ipc.ProfileRef) (*clientRPCExitChange, error) {
	profile, err := rpcFindProfile(cfg, ref)
	if err != nil {
		return nil, err
	}
	if profile.ID != cfg.RPCState.ActiveProfileID {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if cfg.RPCState.ExitChange != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
	}
	for _, record := range cfg.RPCState.Operations {
		pending := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, pending) != nil {
			return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		if !rpcOperationTerminal(pending.State) {
			return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
	}
	op.ProfileId = profile.ID
	plan := &clientRPCExitChange{OperationID: op.Id, ProfileID: profile.ID, OwnerID: cfg.LocalOwnerID, ControlOrigin: profile.ControlOrigin, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Previous: cloneExitSelection(cfg.ExitSelection)}
	plan.RouteTable = cfg.WireGuardRouteTable
	if cfg.ConnectionIntent != nil {
		intent := *cfg.ConnectionIntent
		plan.PreviousIntent = &intent
	}
	return plan, nil
}

func (m *ClientRPCMutations) selectExitNodeAs(peer local.Peer, request *ipc.SelectExitNodeRequest, supported []clientRPCExitMode) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/SelectExitNode", request, func(cfg *Config, op *ipc.Operation) error {
		plan, err := m.prepareExitChange(cfg, op, request.Profile)
		if err != nil {
			return err
		}
		mode := clientRPCExitMode{}
		switch request.FamilyMode {
		case ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY:
			mode.Family = api.ExitFamilyIPv4Only
		case ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY:
			mode.Family = api.ExitFamilyIPv6Only
		case ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK:
			mode.Family = api.ExitFamilyDualStack
		default:
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		switch request.LanAccess {
		case ipc.LanAccess_LAN_ACCESS_BLOCK:
			mode.LAN = api.ExitLANBlock
		case ipc.LanAccess_LAN_ACCESS_ALLOW:
			mode.LAN = api.ExitLANAllow
		default:
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if !slices.Contains(supported, mode) {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		if cfg.CachedMap == nil || cfg.CachedMap.Network.ClientPolicy == nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
		}
		if cfg.CachedMap.Network.Revision != cfg.MapRevision || cfg.CachedMap.Revision.Global != cfg.MapGlobalRevision {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		var selected *ClientExitSelection
		for _, grant := range cfg.CachedMap.Network.ClientPolicy.ExitNodes {
			if grant.ID == request.ExitNodeId {
				selected = &ClientExitSelection{ID: grant.ID, NetworkID: cfg.NetworkID, NodeID: cfg.NodeID, Host: grant.Host, Family: mode.Family, LAN: mode.LAN}
				break
			}
		}
		if selected == nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
		}
		if _, err := exitRoutePeers(*cfg, *cfg.CachedMap, selected, m.now()); err != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
		}
		plan.Requested = selected
		plan.MapHash = cfg.CachedMap.MapSignature.PayloadHash
		cfg.RPCState.ExitChange = plan
		// Acceptance records intent only; the engine must not observe a newly
		// active selection before enforcement and application have completed.
		return nil
	})
	return op, err
}

func (m *ClientRPCMutations) clearExitNodeAs(peer local.Peer, request *ipc.ClearExitNodeRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/ClearExitNode", request, func(cfg *Config, op *ipc.Operation) error {
		plan, err := m.prepareExitChange(cfg, op, request.Profile)
		if err != nil {
			return err
		}
		// Clear does not need a live map/grant; withdrawn authorization must not
		// prevent removing local routing. It still requires an executor to apply.
		cfg.RPCState.ExitChange = plan
		return nil
	})
	return op, err
}
