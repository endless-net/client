package client

import (
	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Resolve policy from the authenticated recipient map, never from a UI value or
// a different profile's active map. The result describes event behavior, not an
// already completed tunnel transition.
func (m *ClientRPCMutations) uiQuitSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	setting := &ipc.LifecycleSetting{
		Effective: rpcUIQuit(profile), Requested: profile.UIQuit,
		AllowedValues: []ipc.LifecycleBehavior{ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT},
		Control:       &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_DEFAULT, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}},
	}
	if profile.UIQuit != nil {
		setting.Requested = profile.UIQuit.Enum()
		setting.Control.Source = ipc.SettingSource_SETTING_SOURCE_USER
	}
	if profile.ID != cfg.RPCState.ActiveProfileID {
		setting.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_requires_active_profile", ActionOwner: ipc.ActionOwner_ACTION_OWNER_USER}
		// Inactive profiles retain their own configuration, never active policy.
		cfg = profile.Configuration
	}
	if state := cfg.CachedMap; state != nil {
		if cfg.MapSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" || state.Node.ID != cfg.NodeID || state.Network.ID != cfg.NetworkID || state.Network.Revision != cfg.MapRevision || state.Revision.Global != cfg.MapGlobalRevision || api.ValidateNetworkMap(*state) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*state, *cfg.MapSigningTrust) != nil || state.MapSignature == nil || !m.now().Before(state.MapSignature.ExpiresAt) {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		if policy := state.Network.ClientPolicy; policy != nil {
			for _, managed := range policy.Settings {
				if managed.Key != api.ClientSettingUIQuit {
					continue
				}
				var value ipc.LifecycleBehavior
				switch *managed.LifecycleValue {
				case api.ClientLifecycleKeepIntent:
					value = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
				case api.ClientLifecycleDisconnect:
					value = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT
				default:
					// CONNECT requires a lifecycle connect executor. Do not execute
					// a local default in place of an unsupported managed behavior.
					return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
				}
				setting.Control.PolicyId = managed.PolicyID
				setting.Control.Locked = managed.Locked
				if managed.Locked || profile.UIQuit == nil {
					setting.Effective = value
					setting.Control.Source = ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
					actor := ipc.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
					if managed.Source == api.ClientPolicyDevice {
						setting.Control.Source = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY
						actor = ipc.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
					}
					if managed.Locked {
						setting.AllowedValues = []ipc.LifecycleBehavior{value}
						setting.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_POLICY_BLOCKED, ReasonKey: "preference_locked_by_policy", ActionOwner: actor}
					}
				}
			}
		}
	}
	if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	return setting, nil
}
