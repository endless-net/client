package client

import (
	"reflect"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type clientRPCNetworkPreferenceChange struct {
	OperationID     string                    `json:"operation_id"`
	ProfileID       string                    `json:"profile_id"`
	OwnerID         string                    `json:"owner_id"`
	ControlOrigin   string                    `json:"control_origin"`
	NodeID          string                    `json:"node_id"`
	NetworkID       string                    `json:"network_id"`
	MapHash         string                    `json:"map_hash"`
	Previous        *ClientNetworkPreferences `json:"previous,omitempty"`
	Requested       *ClientNetworkPreferences `json:"requested,omitempty"`
	PreviousUIQuit  *ipc.LifecycleBehavior    `json:"previous_ui_quit,omitempty"`
	RequestedUIQuit *ipc.LifecycleBehavior    `json:"requested_ui_quit,omitempty"`
	Changed         bool                      `json:"changed"`
	PreviousIntent  *ConnectionIntent         `json:"previous_intent,omitempty"`
	Containing      bool                      `json:"containing,omitempty"`
	FailureCode     ipc.ErrorCode             `json:"failure_code,omitempty"`
	FailureReason   string                    `json:"failure_reason,omitempty"`
}

func cloneNetworkPreferences(value *ClientNetworkPreferences) *ClientNetworkPreferences {
	if value == nil {
		return nil
	}
	copy := *value
	if value.AllowInbound != nil {
		copy.AllowInbound = proto.Bool(*value.AllowInbound)
	}
	if value.AcceptDNS != nil {
		copy.AcceptDNS = proto.Bool(*value.AcceptDNS)
	}
	if value.AcceptRoutes != nil {
		copy.AcceptRoutes = proto.Bool(*value.AcceptRoutes)
	}
	return &copy
}

func cloneLifecycleBehavior(value *ipc.LifecycleBehavior) *ipc.LifecycleBehavior {
	if value == nil {
		return nil
	}
	return value.Enum()
}

// Admission only. Do not change the active configuration or publish SUCCEEDED
// before the runtime worker has applied or rolled back the entire patch.
func (m *ClientRPCMutations) setNetworkPreferencesAs(peer local.Peer, request *ipc.SetPreferencesRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/SetPreferences", request, func(cfg *Config, op *ipc.Operation) error {
		keys, err := rpcPreferencePatchKeys(request.Patch)
		if err != nil {
			return err
		}
		return m.prepareNetworkPreferences(cfg, op, request.Profile, keys, request.Patch)
	})
	return op, err
}

func (m *ClientRPCMutations) resetNetworkPreferencesAs(peer local.Peer, request *ipc.ResetPreferencesRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/ResetPreferences", request, func(cfg *Config, op *ipc.Operation) error {
		if err := rpcValidatePreferenceReset(request.Keys); err != nil {
			return err
		}
		return m.prepareNetworkPreferences(cfg, op, request.Profile, request.Keys, nil)
	})
	return op, err
}

func (m *ClientRPCMutations) prepareNetworkPreferences(cfg *Config, op *ipc.Operation, ref *ipc.ProfileRef, keys []ipc.PreferenceKey, patch *ipc.PreferencesPatch) error {
	for _, key := range keys {
		if key != ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND && key != ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS && key != ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES && key != ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
	}
	profile, err := rpcFindProfile(cfg, ref)
	if err != nil {
		return err
	}
	if profile.ID != cfg.RPCState.ActiveProfileID {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if cfg.RPCState.NetworkPreferenceChange != nil {
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
	if cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if cfg.MapRevision != cfg.CachedMap.Network.Revision || cfg.MapGlobalRevision != cfg.CachedMap.Revision.Global {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if _, err := resolveNetworkAcceptance(*cfg, *cfg.CachedMap, m.now()); err != nil {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	requested := cloneNetworkPreferences(cfg.NetworkPreferences)
	if requested == nil {
		requested = &ClientNetworkPreferences{}
	}
	uiQuit := cloneLifecycleBehavior(profile.UIQuit)
	for _, key := range keys {
		var value *bool
		var policyKey api.ClientSettingKey
		switch key {
		case ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND:
			if patch != nil {
				value = proto.Bool(patch.GetAllowInbound())
			}
			requested.AllowInbound, policyKey = value, api.ClientSettingAllowInbound
		case ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS:
			if patch != nil {
				value = proto.Bool(patch.GetAcceptDns())
			}
			requested.AcceptDNS, policyKey = value, api.ClientSettingAcceptDNS
		case ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES:
			if patch != nil {
				value = proto.Bool(patch.GetAcceptRoutes())
			}
			requested.AcceptRoutes, policyKey = value, api.ClientSettingAcceptRoutes
		case ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT:
			uiQuit = nil
			if patch != nil {
				uiQuit = patch.GetUiQuit().Enum()
				if *uiQuit != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && *uiQuit != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
					return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
				}
			}
			candidate := profile
			candidate.UIQuit = uiQuit
			setting, err := m.uiQuitSetting(*cfg, candidate)
			if err != nil {
				return err
			}
			if uiQuit != nil && setting.Control.Locked && setting.Effective != *uiQuit {
				return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
			}
		}
		if policy := cfg.CachedMap.Network.ClientPolicy; policy != nil && value != nil {
			for _, managed := range policy.Settings {
				if managed.Key == policyKey && managed.Locked && *managed.BooleanValue != *value {
					return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
				}
			}
		}
	}
	if requested.AcceptDNS == nil && requested.AcceptRoutes == nil && requested.AllowInbound == nil {
		requested = nil
	}
	op.ProfileId = profile.ID
	cfg.RPCState.NetworkPreferenceChange = &clientRPCNetworkPreferenceChange{
		OperationID: op.Id, ProfileID: profile.ID, OwnerID: cfg.LocalOwnerID, ControlOrigin: profile.ControlOrigin,
		NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, MapHash: cfg.CachedMap.MapSignature.PayloadHash,
		Previous: cloneNetworkPreferences(cfg.NetworkPreferences), Requested: requested,
		PreviousUIQuit: cloneLifecycleBehavior(profile.UIQuit), RequestedUIQuit: uiQuit,
		Changed: !reflect.DeepEqual(cfg.NetworkPreferences, requested) || !reflect.DeepEqual(profile.UIQuit, uiQuit),
	}
	if cfg.ConnectionIntent != nil {
		intent := *cfg.ConnectionIntent
		cfg.RPCState.NetworkPreferenceChange.PreviousIntent = &intent
	}
	return nil
}
