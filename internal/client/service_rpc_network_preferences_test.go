package client

import (
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNetworkPreferenceAdmissionIsDurableAndDoesNotApply(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	before := m.store.Read()
	request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), AcceptRoutes: proto.Bool(false), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}}
	op, err := m.setNetworkPreferencesAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	after := m.store.Read()
	plan := after.RPCState.NetworkPreferenceChange
	if op.State != ipc.OperationState_OPERATION_STATE_PENDING || plan == nil || !plan.Changed || plan.OperationID != op.Id || plan.ProfileID != profile.ProfileId || plan.OwnerID != owner.Identity || plan.MapHash != before.CachedMap.MapSignature.PayloadHash {
		t.Fatal("admission lost pending context")
	}
	if plan.Requested == nil || plan.Requested.AcceptDNS == nil || *plan.Requested.AcceptDNS || plan.Requested.AcceptRoutes == nil || *plan.Requested.AcceptRoutes || plan.RequestedUIQuit == nil || *plan.RequestedUIQuit != ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT {
		t.Fatal("atomic patch lost explicit values")
	}
	if !reflect.DeepEqual(before.NetworkPreferences, after.NetworkPreferences) || !reflect.DeepEqual(before.RPCState.Profiles, after.RPCState.Profiles) {
		t.Fatal("admission applied configuration before worker")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan, m.store.Read().RPCState.NetworkPreferenceChange) {
		t.Fatal("restart lost pending patch")
	}
	replay, err := m.setNetworkPreferencesAs(owner, request)
	if err != nil || !proto.Equal(op, replay) {
		t.Fatal("replay did not precede stale CAS and busy checks", err)
	}
	_, err = m.resetNetworkPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
}

func TestNetworkPreferenceAdmissionRejectsWholePatch(t *testing.T) {
	for _, scenario := range []string{"observer", "tampered", "stale", "locked", "unsupported", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			opts, key := signedServiceDNSFixture(t)
			if scenario == "locked" {
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingAcceptRoutes, Source: api.ClientPolicyDevice, PolicyID: "routes", Locked: true, BooleanValue: proto.Bool(true)}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
				if scenario == "tampered" {
					cfg.CachedMap.Network.Name = "tampered"
				}
				if scenario == "stale" {
					cfg.MapRevision++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			patch := &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), AcceptRoutes: proto.Bool(false), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}
			caller := owner
			want := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			switch scenario {
			case "observer":
				caller = local.Peer{Identity: "uid:other"}
				want = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			case "stale":
				want = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			case "locked":
				want = ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED
			case "unsupported":
				patch.Suspend = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT.Enum()
				want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "empty":
				patch = &ipc.PreferencesPatch{}
				want = ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
			}
			before := m.store.Read()
			_, err := m.setNetworkPreferencesAs(caller, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: patch})
			assertRPCFailure(t, err, want)
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("rejected patch partially changed durable state")
			}
		})
	}
}

func TestNetworkPreferenceResetPreservesUnselectedOverrides(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(false), AcceptRoutes: proto.Bool(false)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.resetNetworkPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	plan := cfg.RPCState.NetworkPreferenceChange
	if op.State != ipc.OperationState_OPERATION_STATE_PENDING || plan.Requested.AcceptDNS != nil || plan.Requested.AcceptRoutes == nil || *plan.Requested.AcceptRoutes || plan.Previous.AcceptDNS == nil || *plan.Previous.AcceptDNS || cfg.NetworkPreferences.AcceptDNS == nil {
		t.Fatal("reset lost selected-key isolation or applied before worker")
	}
}
