package client

import (
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceCatalogCommittedPendingAndPolicy(t *testing.T) {
	for _, scenario := range []string{"default", "local", "pending", "account", "device_locked"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			opts, key := signedServiceDNSFixture(t)
			peerID := opts.NetworkMap.Peers[0].ID
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, peerID)
			if scenario == "account" || scenario == "device_locked" {
				source := api.ClientPolicyAccount
				if scenario == "device_locked" {
					source = api.ClientPolicyDevice
				}
				opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceHost, ID: peerID, Source: source, PolicyID: "host-rule", Locked: scenario == "device_locked", Enabled: false}}}
				resignApplicationMap(t, &opts.NetworkMap, key)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
				if scenario == "local" {
					cfg.ResourcePreferences = map[string]bool{id: false}
				}
				if scenario == "device_locked" {
					cfg.ResourcePreferences = map[string]bool{id: true}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if scenario == "pending" {
				if _, err := m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id}); err != nil {
					t.Fatal(err)
				}
			}
			response, err := NewClientRPCService(m, nil).resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			var value *ipc.BooleanSetting
			for _, row := range response.Resources {
				if row.Id == id {
					value = row.Enabled
					if row.Availability.ReasonKey != "resource_runtime_observation_unavailable" {
						t.Fatal("policy fabricated reachability")
					}
				}
			}
			if value == nil {
				t.Fatal("missing setting")
			}
			wantEffective := scenario == "default" || scenario == "pending"
			wantSource := ipc.SettingSource_SETTING_SOURCE_DEFAULT
			if scenario == "local" {
				wantSource = ipc.SettingSource_SETTING_SOURCE_USER
			}
			if scenario == "account" {
				wantSource = ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY
			}
			if scenario == "device_locked" {
				wantSource = ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY
			}
			if value.Effective != wantEffective || value.Control.Source != wantSource {
				t.Fatal("incorrect committed resolution", value)
			}
			if scenario == "pending" && (value.Requested == nil || *value.Requested || value.Control.Mutation.ReasonKey != "resource_change_pending") {
				t.Fatal("pending overwrote committed choice", value)
			}
			if scenario == "device_locked" && (!value.Control.Locked || value.Control.PolicyId != "host-rule" || value.Requested == nil || !*value.Requested || value.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_POLICY_BLOCKED) {
				t.Fatal("lock lost original choice or provenance", value)
			}
			if scenario == "default" && (value.Requested != nil || value.Control.Mutation.ReasonKey != "resource_worker_unavailable") {
				t.Fatal("missing worker or absent choice hidden")
			}
		})
	}
}
