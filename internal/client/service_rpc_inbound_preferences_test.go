package client

import (
	"context"
	"reflect"
	"sync"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestInboundPreferenceAtomicPatchRestartAndSelectiveReset(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AllowInbound: proto.Bool(false), AcceptDns: proto.Bool(false), AcceptRoutes: proto.Bool(false)}}
	op, err := m.setNetworkPreferencesAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	read := func() *ipc.Preferences {
		t.Helper()
		r, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		return r.Msg.Preferences
	}
	value := read().AllowInbound
	if !value.Effective || value.Requested == nil || *value.Requested || m.store.Read().NetworkPreferences != nil {
		t.Fatal("pending inbound applied early")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := m.setNetworkPreferencesAs(owner, request)
	if err != nil || !proto.Equal(op, replay) {
		t.Fatal("inbound restart replay changed", err)
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { t.Fatal("offline preference started tunnel"); return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	settings := read()
	if settings.AllowInbound.Effective || settings.AcceptDns.Effective || settings.AcceptRoutes.Effective || settings.AllowInbound.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER {
		t.Fatal("mixed patch not committed atomically")
	}
	_, err = m.resetNetworkPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ALLOW_INBOUND}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	settings = read()
	if !settings.AllowInbound.Effective || settings.AllowInbound.Requested != nil || settings.AcceptDns.Effective || settings.AcceptRoutes.Effective {
		t.Fatal("selective inbound reset changed DNS/routes")
	}
}

func TestInboundPolicyLockRejectsWholeMixedPreferencePatch(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	opts, key := signedServiceDNSFixture(t)
	opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingAllowInbound, Source: api.ClientPolicyDevice, PolicyID: "inbound-device", BooleanValue: proto.Bool(false), Locked: true}}}
	resignApplicationMap(t, &opts.NetworkMap, key)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	_, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AllowInbound: proto.Bool(true), AcceptDns: proto.Bool(false)}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED)
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("inbound lock permitted partial DNS mutation")
	}
	r, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	value := r.Msg.Preferences.AllowInbound
	if value.Effective || !value.Control.Locked || value.Control.PolicyId != "inbound-device" || value.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY || value.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_POLICY_BLOCKED {
		t.Fatal("inbound policy projection lost provenance", value)
	}
}
