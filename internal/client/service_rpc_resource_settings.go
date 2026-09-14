package client

import (
	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Effective describes the committed policy resolution, not reachability.
// Availability on Resource carries the separate runtime observation.
func rpcResourceSetting(cfg Config, id string, identity clientResourceIdentity, ready bool) *ipc.BooleanSetting {
	resolved := resourcePreferenceForIdentity(cfg, id, identity)
	control := &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_DEFAULT, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}}
	if resolved.Requested != nil {
		control.Source = ipc.SettingSource_SETTING_SOURCE_USER
	}
	result := &ipc.BooleanSetting{Effective: resolved.Enabled, Requested: resolved.Requested, Control: control}
	if !ready {
		control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "resource_worker_unavailable"}
	}
	if plan := cfg.RPCState.NetworkPreferenceChange; plan != nil {
		control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "resource_change_pending"}
		if value, exists := plan.RequestedResources[id]; exists {
			result.Requested = proto.Bool(value)
		}
	}
	if managed := resolved.Managed; managed != nil {
		control.PolicyId, control.Locked = managed.PolicyID, managed.Locked
		if resolved.Requested == nil || managed.Locked {
			control.Source = ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
			actor := ipc.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
			if managed.Source == api.ClientPolicyDevice {
				control.Source = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY
				actor = ipc.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
			}
			if managed.Locked {
				control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_POLICY_BLOCKED, ReasonKey: "resource_locked_by_policy", ActionOwner: actor}
			}
		}
	}
	return result
}
