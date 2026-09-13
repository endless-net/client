package client

import (
	"context"

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
		if len(keys) != 1 || keys[0] != ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		value := request.Patch.GetUiQuit()
		if value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		changed := profile.UIQuit == nil || *profile.UIQuit != value
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		profile.UIQuit = &value
		cfg.RPCState.Profiles[profile.ID] = profile
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: changed}}
		return nil
	}, true)
	return op, err
}

func (m *ClientRPCMutations) resetPreferencesAs(peer local.Peer, request *ipc.ResetPreferencesRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/ResetPreferences", request, func(cfg *Config, op *ipc.Operation) error {
		if err := rpcValidatePreferenceReset(request.Keys); err != nil {
			return err
		}
		if len(request.Keys) != 1 || request.Keys[0] != ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		changed := profile.UIQuit != nil
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		profile.UIQuit = nil
		cfg.RPCState.Profiles[profile.ID] = profile
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: changed}}
		return nil
	}, true)
	return op, err
}

func (m *ClientRPCMutations) notifyLifecycleAs(peer local.Peer, request *ipc.NotifyLifecycleRequest) (*ipc.Operation, error) {
	before := m.store.Read()
	behavior := ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT
	if before.RPCState != nil {
		behavior = rpcUIQuit(before.RPCState.Profiles[request.GetProfile().GetProfileId()])
	}
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/NotifyLifecycle", request, func(cfg *Config, op *ipc.Operation) error {
		if request.Event != ipc.LifecycleEvent_LIFECYCLE_EVENT_UI_QUIT {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID || rpcUIQuit(profile) != behavior {
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
	cfg := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetPreferences"), cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.Msg.Profile)
	if err != nil {
		return nil, err
	}
	value := rpcUIQuit(profile)
	if value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT && value != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	source := ipc.SettingSource_SETTING_SOURCE_DEFAULT
	var requested *ipc.LifecycleBehavior
	if profile.UIQuit != nil {
		source = ipc.SettingSource_SETTING_SOURCE_USER
		requested = value.Enum()
	}
	return connect.NewResponse(&ipc.GetPreferencesResponse{Preferences: &ipc.Preferences{ProfileId: profile.ID, Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}, Lifecycle: &ipc.RuntimeLifecycle{UiQuit: &ipc.LifecycleSetting{Effective: value, Requested: requested, AllowedValues: []ipc.LifecycleBehavior{ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT, ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT}, Control: &ipc.SettingControl{Source: source, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}}}}}}), nil
}

func (s *ClientRPCService) SetPreferences(ctx context.Context, request *connect.Request[ipc.SetPreferencesRequest]) (*connect.Response[ipc.SetPreferencesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
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
	return connect.NewResponse(&ipc.ListManagedSettingsResponse{Metadata: preferences.Msg.Preferences.Metadata, Settings: []*ipc.ManagedSetting{{Key: ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT, Control: proto.Clone(value.Control).(*ipc.SettingControl), EffectiveValue: &ipc.ManagedSetting_LifecycleValue{LifecycleValue: value.Effective}}}}), nil
}

func (s *ClientRPCService) ResetPreferences(ctx context.Context, request *connect.Request[ipc.ResetPreferencesRequest]) (*connect.Response[ipc.ResetPreferencesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
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
