package client

import (
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestEngineMapClonePreservesSignedPolicyAfterCallerMutation(t *testing.T) {
	opts, key := signedServiceDNSFixture(t)
	enabled := false
	behavior := api.ClientLifecycleDisconnect
	opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{
		Settings: []api.ManagedClientSetting{
			{Key: api.ClientSettingAcceptDNS, Source: api.ClientPolicyDevice, PolicyID: "dns", BooleanValue: &enabled},
			{Key: api.ClientSettingUIQuit, Source: api.ClientPolicyAccount, PolicyID: "quit", LifecycleValue: &behavior},
		},
		Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceHost, ID: opts.NetworkMap.Peers[0].ID, Source: api.ClientPolicyDevice, PolicyID: "host", Locked: true, Enabled: false}},
	}
	resignApplicationMap(t, &opts.NetworkMap, key)
	snapshot := cloneRegisterNodeResponse(opts.NetworkMap)
	if !reflect.DeepEqual(snapshot.Network.ClientPolicy, opts.NetworkMap.Network.ClientPolicy) {
		t.Fatal("clone changed policy")
	}
	*opts.NetworkMap.Network.ClientPolicy.Settings[0].BooleanValue = true
	*opts.NetworkMap.Network.ClientPolicy.Settings[1].LifecycleValue = api.ClientLifecycleKeepIntent
	opts.NetworkMap.Network.ClientPolicy.Resources[0].Enabled = true
	if err := api.VerifyNetworkMapSignatureWithTrustBundle(snapshot, *opts.SigningTrust); err != nil {
		t.Fatal("caller mutation corrupted signed runtime snapshot", err)
	}
	if err := api.VerifyNetworkMapSignatureWithTrustBundle(opts.NetworkMap, *opts.SigningTrust); err == nil {
		t.Fatal("test did not mutate signed policy")
	}
	// Mutation in a rollback copy must not change the applied snapshot either.
	rollback := cloneRegisterNodeResponse(snapshot)
	rollback.Network.ClientPolicy.Settings[0].PolicyID = "changed"
	*rollback.Network.ClientPolicy.Settings[0].BooleanValue = true
	rollback.Network.ClientPolicy.Resources[0].Locked = false
	if err := api.VerifyNetworkMapSignatureWithTrustBundle(snapshot, *opts.SigningTrust); err != nil {
		t.Fatal("rollback clone aliased applied policy", err)
	}
}

func TestClientPolicyCloneOwnsExitConstraints(t *testing.T) {
	source := &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
	copy := cloneClientPolicy(source)
	copy.ExitNodes[0].AllowedFamilyModes[0] = api.ExitFamilyDualStack
	copy.ExitNodes[0].AllowedLANAccess[0] = api.ExitLANAllow
	copy.ExitNodes[0].ID = "other"
	if source.ExitNodes[0].ID != "exit" || source.ExitNodes[0].AllowedFamilyModes[0] != api.ExitFamilyIPv4Only || source.ExitNodes[0].AllowedLANAccess[0] != api.ExitLANBlock {
		t.Fatal("exit constraints shared with copy")
	}
	if cloneClientPolicy(nil) != nil || !reflect.DeepEqual(cloneClientPolicy(&api.ClientPolicy{}), &api.ClientPolicy{}) {
		t.Fatal("clone changed absent policy fields")
	}
}
