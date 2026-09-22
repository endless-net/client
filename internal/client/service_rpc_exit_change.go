package client

import (
	"reflect"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// OS ownership survives terminal operations and never confers control-plane
// authority. Only explicit clear with confirmed release removes this record.
type clientRPCExitProtection struct {
	OperationID   string            `json:"operation_id"`
	ProfileID     string            `json:"profile_id"`
	OwnerID       string            `json:"owner_id"`
	NodeID        string            `json:"node_id"`
	NetworkID     string            `json:"network_id"`
	InterfaceName string            `json:"interface_name"`
	RouteTable    string            `json:"route_table"`
	LAN           *exitLANOwnership `json:"lan,omitempty"`
}

func cloneExitProtection(protection *clientRPCExitProtection) *clientRPCExitProtection {
	if protection == nil {
		return nil
	}
	copy := *protection
	copy.LAN = cloneExitLANOwnership(protection.LAN)
	return &copy
}

type clientRPCExitChange struct {
	OperationID              string                   `json:"operation_id"`
	ProfileID                string                   `json:"profile_id"`
	ActiveProfileID          string                   `json:"active_profile_id"`
	Protection               *clientRPCExitProtection `json:"protection,omitempty"`
	OwnerID                  string                   `json:"owner_id"`
	ControlOrigin            string                   `json:"control_origin"`
	NodeID                   string                   `json:"node_id"`
	NetworkID                string                   `json:"network_id"`
	RouteTable               string                   `json:"route_table"`
	MapHash                  string                   `json:"map_hash,omitempty"`
	Requested                *ClientExitSelection     `json:"requested,omitempty"`
	Previous                 *ClientExitSelection     `json:"previous,omitempty"`
	PreviousProfileSelection *ClientExitSelection     `json:"previous_profile_selection,omitempty"`
	PreviousIntent           *ConnectionIntent        `json:"previous_intent,omitempty"`
	NextAttemptAt            time.Time                `json:"next_attempt_at,omitempty"`
	Containing               bool                     `json:"containing,omitempty"`
	// Releasing durably authorizes removal of protection only after clear's
	// route cleanup was observed with both families still blocked. Retain the
	// journal and previous selection until release is positively confirmed.
	Releasing     bool          `json:"releasing,omitempty"`
	FailureCode   ipc.ErrorCode `json:"failure_code,omitempty"`
	FailureReason string        `json:"failure_reason,omitempty"`
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
	clear := op.Kind == ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE
	protection := cfg.RPCState.ExitProtection
	if protection != nil && (!strings.EqualFold(protection.OwnerID, cfg.LocalOwnerID) || protection.ProfileID != profile.ID) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if profile.ID != cfg.RPCState.ActiveProfileID && (!clear || protection == nil || cfg.ExitSelection != nil) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if protection != nil && !clear && (protection.NodeID != cfg.NodeID || protection.NetworkID != cfg.NetworkID || protection.RouteTable != cfg.WireGuardRouteTable) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if cfg.ExitSelection != nil && cfg.ExitSelection.RouteTable != cfg.WireGuardRouteTable && (!clear || protection == nil || cfg.ExitSelection.NodeID != protection.NodeID || cfg.ExitSelection.NetworkID != protection.NetworkID || cfg.ExitSelection.RouteTable != protection.RouteTable) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if clear && protection != nil && cfg.ExitSelection != nil && (cfg.ExitSelection.NodeID != protection.NodeID || cfg.ExitSelection.NetworkID != protection.NetworkID || cfg.ExitSelection.RouteTable != protection.RouteTable) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if profile.ID != cfg.RPCState.ActiveProfileID && profile.Configuration.ExitSelection != nil {
		selected := profile.Configuration.ExitSelection
		if selected.NodeID != protection.NodeID || selected.NetworkID != protection.NetworkID || selected.RouteTable != protection.RouteTable {
			return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
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
	table := cfg.WireGuardRouteTable
	if clear {
		node, network := cfg.NodeID, cfg.NetworkID
		if protection != nil {
			// Cleanup belongs to the retained scope, even after enrollment or
			// the active profile has changed. Current credentials are irrelevant.
			node, network, table = protection.NodeID, protection.NetworkID, protection.RouteTable
			if protection.OperationID == "" || !safeWireGuardInterfaceName(protection.InterfaceName) || protection.InterfaceName == "lo" || strings.TrimSpace(protection.InterfaceName) != protection.InterfaceName {
				return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
		}
		if strings.TrimSpace(node) == "" || strings.TrimSpace(network) == "" {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
	}
	// Both selection and cleanup must be executable in a dedicated route scope.
	// Clear uses retained ownership above; selection uses the current context.
	normalized, err := NormalizeWireGuardRouteTable(table)
	if err != nil || normalized != table {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if table == "off" || table == "253" || table == "254" || table == "255" {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	op.ProfileId = profile.ID
	plan := &clientRPCExitChange{OperationID: op.Id, ProfileID: profile.ID, OwnerID: cfg.LocalOwnerID, ControlOrigin: profile.ControlOrigin, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Previous: cloneExitSelection(cfg.ExitSelection)}
	plan.ActiveProfileID = cfg.RPCState.ActiveProfileID
	plan.Protection = cloneExitProtection(protection)
	if profile.ID != cfg.RPCState.ActiveProfileID {
		plan.PreviousProfileSelection = cloneExitSelection(profile.Configuration.ExitSelection)
	}
	plan.RouteTable = cfg.WireGuardRouteTable
	if cfg.ConnectionIntent != nil {
		intent := *cfg.ConnectionIntent
		plan.PreviousIntent = &intent
	}
	return plan, nil
}

func exitProtectionBound(cfg *Config, plan *clientRPCExitChange) bool {
	return reflect.DeepEqual(plan.Protection, cfg.RPCState.ExitProtection)
}

// Selecting an exit changes routing for an already connected runtime. It must
// never act as Connect or replace a missing/disconnected local intent.
func exitSelectionConnectionReady(cfg Config) bool {
	return cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected
}

func (m *ClientRPCMutations) selectExitNodeAs(peer local.Peer, request *ipc.SelectExitNodeRequest, supported []clientRPCExitMode) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/SelectExitNode", request, func(cfg *Config, op *ipc.Operation) error {
		plan, err := m.prepareExitChange(cfg, op, request.Profile)
		if err != nil {
			return err
		}
		if !exitSelectionConnectionReady(*cfg) {
			return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
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
				selected.RouteTable = plan.RouteTable
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
