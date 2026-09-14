package client

import (
	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (s *ClientRPCService) acceptNetworkPreferenceOperation(accept func() (*ipc.Operation, error)) (*ipc.Operation, error) {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := accept()
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return op, nil
}

// Effective is the authenticated policy resolution of the committed config.
// It is not a claim that a particular resolver query or resource is reachable.
// A pending patch changes Requested only; the worker commits both keys together.
func (m *ClientRPCMutations) networkPreferenceSettings(cfg Config, profile clientRPCProfile) (*ipc.BooleanSetting, *ipc.BooleanSetting, error) {
	active := profile.ID == cfg.RPCState.ActiveProfileID
	plan := cfg.RPCState.NetworkPreferenceChange
	if !active {
		cfg = profile.Configuration
	}
	if cfg.CachedMap == nil {
		return nil, nil, nil
	}
	resolved, err := resolveNetworkAcceptance(cfg, *cfg.CachedMap, m.now())
	if err != nil || cfg.CachedMap.Network.Revision != cfg.MapRevision || cfg.CachedMap.Revision.Global != cfg.MapGlobalRevision {
		return nil, nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	committed := cloneNetworkPreferences(cfg.NetworkPreferences)
	if committed == nil {
		committed = &ClientNetworkPreferences{}
	}
	requested := committed
	pending := active && plan != nil && plan.ProfileID == profile.ID
	if pending {
		requested = cloneNetworkPreferences(plan.Requested)
		if requested == nil {
			requested = &ClientNetworkPreferences{}
		}
	}
	setting := func(key api.ClientSettingKey, effective bool, current, request *bool) *ipc.BooleanSetting {
		control := &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_DEFAULT, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}}
		if current != nil {
			control.Source = ipc.SettingSource_SETTING_SOURCE_USER
		}
		if !active || pending {
			control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_requires_idle_active_profile", ActionOwner: ipc.ActionOwner_ACTION_OWNER_USER}
		}
		if policy := cfg.CachedMap.Network.ClientPolicy; policy != nil {
			for _, managed := range policy.Settings {
				if managed.Key != key {
					continue
				}
				control.PolicyId, control.Locked = managed.PolicyID, managed.Locked
				if current == nil || managed.Locked {
					control.Source = ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
					actor := ipc.ActionOwner_ACTION_OWNER_ACCESS_ADMINISTRATOR
					if managed.Source == api.ClientPolicyDevice {
						control.Source, actor = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY, ipc.ActionOwner_ACTION_OWNER_DEVICE_ADMINISTRATOR
					}
					if managed.Locked {
						control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_POLICY_BLOCKED, ReasonKey: "preference_locked_by_policy", ActionOwner: actor}
					}
				}
			}
		}
		result := &ipc.BooleanSetting{Effective: effective, Control: control}
		if request != nil {
			result.Requested = proto.Bool(*request)
		}
		return result
	}
	return setting(api.ClientSettingAcceptDNS, resolved.dns, committed.AcceptDNS, requested.AcceptDNS), setting(api.ClientSettingAcceptRoutes, resolved.routes, committed.AcceptRoutes, requested.AcceptRoutes), nil
}
