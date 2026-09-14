package client

import (
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// Resolve policy from the authenticated recipient map, never from a UI value or
// a different profile's active map. The result describes event behavior, not an
// already completed tunnel transition.
func (m *ClientRPCMutations) uiQuitSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	return lifecycleSetting(cfg, profile, api.ClientSettingUIQuit, profile.UIQuit, m.now())
}

func (m *ClientRPCMutations) runtimeStartSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	return lifecycleSetting(cfg, profile, api.ClientSettingRuntimeStart, profile.RuntimeStart, m.now())
}

func (m *ClientRPCMutations) userLogoffSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	return lifecycleSetting(cfg, profile, api.ClientSettingUserLogoff, profile.UserLogoff, m.now())
}

func (m *ClientRPCMutations) suspendSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	return lifecycleSetting(cfg, profile, api.ClientSettingSuspend, profile.Suspend, m.now())
}

func (m *ClientRPCMutations) resumeSetting(cfg Config, profile clientRPCProfile) (*ipc.LifecycleSetting, error) {
	return lifecycleSetting(cfg, profile, api.ClientSettingResume, profile.Resume, m.now())
}

func lifecycleSetting(cfg Config, profile clientRPCProfile, key api.ClientSettingKey, requested *ipc.LifecycleBehavior, now time.Time) (*ipc.LifecycleSetting, error) {
	setting := &ipc.LifecycleSetting{
		Effective:     ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT,
		AllowedValues: []ipc.LifecycleBehavior{ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT},
		Control:       &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_DEFAULT, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}},
	}
	if key == api.ClientSettingRuntimeStart {
		setting.AllowedValues = append(setting.AllowedValues, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT)
	}
	if requested != nil {
		setting.Requested = requested.Enum()
		setting.Effective = *requested
		setting.Control.Source = ipc.SettingSource_SETTING_SOURCE_USER
	}
	if cfg.RPCState != nil && profile.ID != cfg.RPCState.ActiveProfileID {
		setting.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_requires_active_profile", ActionOwner: ipc.ActionOwner_ACTION_OWNER_USER}
		// Inactive profiles retain their own configuration, never active policy.
		cfg = profile.Configuration
	}
	if key != api.ClientSettingUIQuit && cfg.CachedMap == nil && rpcConfigHasEnrollment(cfg) {
		// Retain the user's requested value without fabricating an effective
		// default or a known unlocked policy for an enrolled identity.
		setting.Effective = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED
		setting.AllowedValues = nil
		setting.Control = &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_UNSPECIFIED, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: string(key) + "_policy_unavailable"}}
		return setting, nil
	}
	if state := cfg.CachedMap; state != nil {
		if cfg.MapSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" || state.Node.ID != cfg.NodeID || state.Network.ID != cfg.NetworkID || state.Network.Revision != cfg.MapRevision || state.Revision.Global != cfg.MapGlobalRevision || api.ValidateNetworkMap(*state) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*state, *cfg.MapSigningTrust) != nil || state.MapSignature == nil || !now.Before(state.MapSignature.ExpiresAt) {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		if policy := state.Network.ClientPolicy; policy != nil {
			for _, managed := range policy.Settings {
				if managed.Key != key {
					continue
				}
				var value ipc.LifecycleBehavior
				switch *managed.LifecycleValue {
				case api.ClientLifecycleKeepIntent:
					value = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
				case api.ClientLifecycleDisconnect:
					value = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT
				case api.ClientLifecycleConnect:
					if key != api.ClientSettingRuntimeStart {
						return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
					}
					value = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT
				default:
					// CONNECT requires a lifecycle connect executor. Do not execute
					// a local default in place of an unsupported managed behavior.
					return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
				}
				setting.Control.PolicyId = managed.PolicyID
				setting.Control.Locked = managed.Locked
				if managed.Locked || requested == nil {
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
	if setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT && (key != api.ClientSettingRuntimeStart || setting.Effective != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT) {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	return setting, nil
}
