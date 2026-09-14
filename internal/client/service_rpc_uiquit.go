package client

import (
	"context"
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func rpcUIQuit(profile clientRPCProfile) ipc.LifecycleBehavior {
	if profile.UIQuit == nil {
		return ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
	}
	return *profile.UIQuit
}

func (m *ClientRPCMutations) setPreferencesAs(peer local.Peer, request *ipc.SetPreferencesRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/SetPreferences", request, func(cfg *Config, op *ipc.Operation) error {
		keys, err := rpcPreferencePatchKeys(request.Patch)
		if err != nil {
			return err
		}
		return m.prepareLifecyclePreferences(cfg, op, request.Profile, keys, request.Patch)
	}, true)
	return op, err
}

func (m *ClientRPCMutations) resetPreferencesAs(peer local.Peer, request *ipc.ResetPreferencesRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/ResetPreferences", request, func(cfg *Config, op *ipc.Operation) error {
		if err := rpcValidatePreferenceReset(request.Keys); err != nil {
			return err
		}
		return m.prepareLifecyclePreferences(cfg, op, request.Profile, request.Keys, nil)
	}, true)
	return op, err
}

func (m *ClientRPCMutations) prepareLifecyclePreferences(cfg *Config, op *ipc.Operation, ref *ipc.ProfileRef, keys []ipc.PreferenceKey, patch *ipc.PreferencesPatch) error {
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
	previous := profile
	for _, key := range keys {
		if err := m.patchLifecyclePreference(*cfg, &profile, key, patch); err != nil {
			return err
		}
	}
	cfg.RPCState.Profiles[profile.ID] = profile
	op.ProfileId = profile.ID
	op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
	op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: !reflect.DeepEqual(previous.UIQuit, profile.UIQuit) || !reflect.DeepEqual(previous.RuntimeStart, profile.RuntimeStart)}}
	return nil
}

// Mutates a candidate only. The caller commits all keys in one transaction.
func (m *ClientRPCMutations) patchLifecyclePreference(cfg Config, profile *clientRPCProfile, key ipc.PreferenceKey, patch *ipc.PreferencesPatch) error {
	var value *ipc.LifecycleBehavior
	var resolve func(Config, clientRPCProfile) (*ipc.LifecycleSetting, error)
	switch key {
	case ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT:
		if patch != nil {
			value = patch.GetUiQuit().Enum()
		}
		resolve = m.uiQuitSetting
	case ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START:
		if patch != nil {
			value = patch.GetRuntimeStart().Enum()
		}
		resolve = m.runtimeStartSetting
	default:
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if value != nil && *value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && *value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT && (key != ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START || *value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT) {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	if key == ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT {
		profile.UIQuit = value
	} else {
		profile.RuntimeStart = value
	}
	setting, err := resolve(cfg, *profile)
	if err != nil {
		return err
	}
	if setting.Effective == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED {
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if value != nil && setting.Control.Locked && setting.Effective != *value {
		return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
	}
	return nil
}

func (m *ClientRPCMutations) notifyLifecycleAs(peer local.Peer, request *ipc.NotifyLifecycleRequest) (*ipc.Operation, error) {
	before := m.store.Read()
	behavior := ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
	if before.RPCState != nil {
		if setting, err := m.uiQuitSetting(before, before.RPCState.Profiles[request.GetProfile().GetProfileId()]); err == nil {
			behavior = setting.Effective
		}
	}
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/NotifyLifecycle", request, func(cfg *Config, op *ipc.Operation) error {
		if request.Event != ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		setting, err := m.uiQuitSetting(*cfg, profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID || setting.Effective != behavior {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		switch behavior {
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT:
			op.ProfileId = profile.ID
			op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
			op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: false}}
			return nil
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT:
			return m.prepareDisconnect(cfg, op, request.Profile)
		default:
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
	}, behavior == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT)
	return op, err
}

func (s *ClientRPCService) GetPreferences(ctx context.Context, request *connect.Request[ipc.GetPreferencesRequest]) (*connect.Response[ipc.GetPreferencesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	return s.preferencesAs(peer, request.Msg)
}

func (s *ClientRPCService) preferencesAs(peer local.Peer, request *ipc.GetPreferencesRequest) (*connect.Response[ipc.GetPreferencesResponse], error) {
	cfg := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetPreferences"), cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.Profile)
	if err != nil {
		return nil, err
	}
	setting, err := s.mutations.uiQuitSetting(cfg, profile)
	if err != nil {
		return nil, err
	}
	startup, err := s.mutations.runtimeStartSetting(cfg, profile)
	if err != nil {
		return nil, err
	}
	dns, routes, inbound, err := s.mutations.networkPreferenceSettings(cfg, profile)
	if err != nil {
		return nil, err
	}
	s.profileMu.Lock()
	ready := s.profileWorker != nil && s.profileWorker.ctx.Err() == nil
	s.profileMu.Unlock()
	if !ready {
		for _, value := range []*ipc.BooleanSetting{dns, routes, inbound} {
			if value != nil && !value.Control.Locked {
				value.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_worker_unavailable"}
			}
		}
	}
	if plan := cfg.RPCState.NetworkPreferenceChange; plan != nil && plan.ProfileID == profile.ID {
		startup.Requested = cloneLifecycleBehavior(plan.RequestedRuntimeStart)
		if !startup.Control.Locked {
			startup.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_patch_pending"}
		}
		setting.Requested = cloneLifecycleBehavior(plan.RequestedUIQuit)
		if !setting.Control.Locked {
			setting.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "preference_patch_pending"}
		}
	}
	return connect.NewResponse(&ipc.GetPreferencesResponse{Preferences: &ipc.Preferences{ProfileId: profile.ID, Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}, AcceptDns: dns, AcceptRoutes: routes, AllowInbound: inbound, Lifecycle: &ipc.RuntimeLifecycle{UiQuit: setting, RuntimeStart: startup}}}), nil
}

func (s *ClientRPCService) SetPreferences(ctx context.Context, request *connect.Request[ipc.SetPreferencesRequest]) (*connect.Response[ipc.SetPreferencesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	if patch := request.Msg.GetPatch(); patch != nil && (patch.AllowInbound != nil || patch.AcceptDns != nil || patch.AcceptRoutes != nil) {
		op, err := s.acceptNetworkPreferenceOperation(func() (*ipc.Operation, error) { return s.mutations.setNetworkPreferencesAs(peer, request.Msg) })
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&ipc.SetPreferencesResponse{Operation: op}), nil
	}
	op, err := s.mutations.setPreferencesAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.SetPreferencesResponse{Operation: op}), nil
}

func (s *ClientRPCService) ListManagedSettings(ctx context.Context, request *connect.Request[ipc.ListManagedSettingsRequest]) (*connect.Response[ipc.ListManagedSettingsResponse], error) {
	// Reuse the native effective projection, not an independently maintained
	// policy/default table. Unsupported settings have no fabricated value.
	preferences, err := s.GetPreferences(ctx, connect.NewRequest(&ipc.GetPreferencesRequest{Profile: request.Msg.Profile}))
	if err != nil {
		return nil, err
	}
	value := preferences.Msg.Preferences.Lifecycle.UiQuit
	settings := []*ipc.ManagedSetting{{Key: ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT, Control: proto.Clone(value.Control).(*ipc.SettingControl), EffectiveValue: &ipc.ManagedSetting_LifecycleValue{LifecycleValue: value.Effective}}}
	startup := preferences.Msg.Preferences.Lifecycle.RuntimeStart
	for _, entry := range []struct {
		key   ipc.PreferenceKey
		value *ipc.BooleanSetting
	}{{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS, preferences.Msg.Preferences.AcceptDns}, {ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES, preferences.Msg.Preferences.AcceptRoutes}, {ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND, preferences.Msg.Preferences.AllowInbound}} {
		if entry.value != nil {
			settings = append(settings, &ipc.ManagedSetting{Key: entry.key, Control: proto.Clone(entry.value.Control).(*ipc.SettingControl), EffectiveValue: &ipc.ManagedSetting_BooleanValue{BooleanValue: entry.value.Effective}})
		}
	}
	settings = append(settings, &ipc.ManagedSetting{Key: ipc.PreferenceKey_PREFERENCE_KEY_RUNTIME_START, Control: proto.Clone(startup.Control).(*ipc.SettingControl), EffectiveValue: &ipc.ManagedSetting_LifecycleValue{LifecycleValue: startup.Effective}})
	return connect.NewResponse(&ipc.ListManagedSettingsResponse{Metadata: preferences.Msg.Preferences.Metadata, Settings: settings}), nil
}

func (s *ClientRPCService) ResetPreferences(ctx context.Context, request *connect.Request[ipc.ResetPreferencesRequest]) (*connect.Response[ipc.ResetPreferencesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	for _, key := range request.Msg.GetKeys() {
		if key == ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND || key == ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS || key == ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES {
			op, err := s.acceptNetworkPreferenceOperation(func() (*ipc.Operation, error) { return s.mutations.resetNetworkPreferencesAs(peer, request.Msg) })
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(&ipc.ResetPreferencesResponse{Operation: op}), nil
		}
	}
	op, err := s.mutations.resetPreferencesAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.ResetPreferencesResponse{Operation: op}), nil
}

func (s *ClientRPCService) NotifyLifecycle(ctx context.Context, request *connect.Request[ipc.NotifyLifecycleRequest]) (*connect.Response[ipc.NotifyLifecycleResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.notifyLifecycleAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.NotifyLifecycleResponse{Operation: op}), nil
}
