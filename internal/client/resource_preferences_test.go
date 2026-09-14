package client

import (
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourcePreferenceServicePolicyCoversPortsAndPreservesLocalChoice(t *testing.T) {
	for _, locked := range []bool{false, true} {
		m, _, _ := rpcPreferenceFixture(t)
		opts, key := signedServiceDNSFixture(t)
		service := &opts.NetworkMap.Network.Services[0]
		service.Ports = append(service.Ports, api.ServicePort{Protocol: "udp", Port: 9999})
		opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceService, ID: service.ID, Source: api.ClientPolicyDevice, PolicyID: "service-policy", Locked: locked, Enabled: false}}}
		resignApplicationMap(t, &opts.NetworkMap, key)
		first := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, service.ID+"\x00"+service.Ports[0].Protocol+"\x00"+strconv.Itoa(int(service.Ports[0].Port)))
		second := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, service.ID+"\x00udp\x009999")
		if err := m.store.Update(func(cfg *Config) error {
			cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
			cfg.ResourcePreferences = map[string]bool{first: true}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cfg := reopenRPCStoreFromDisk(t, m.store).Read()
		for _, id := range []string{first, second} {
			value, err := resolveResourcePreference(cfg, id, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if value.Managed == nil || value.Managed.PolicyID != "service-policy" || value.Managed.Source != api.ClientPolicyDevice || value.Managed.Locked != locked {
				t.Fatal("service policy not shared by every port", value)
			}
			if value.Enabled != (id == first && !locked) || (value.Requested != nil) != (id == first) {
				t.Fatal("lock or port-local override lost", value)
			}
		}
		delete(cfg.ResourcePreferences, first)
		value, err := resolveResourcePreference(cfg, first, time.Now())
		if err != nil || value.Enabled || value.Requested != nil {
			t.Fatal("absence did not restore service baseline", err)
		}
		if !m.store.Read().ResourcePreferences[first] {
			t.Fatal("read alias modified persistent override")
		}
	}
}

func TestResourcePreferenceFalsePresenceAndAuthenticatedSource(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	cfg := m.store.Read()
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)
	if err := m.store.Update(func(cfg *Config) error { cfg.ResourcePreferences = map[string]bool{id: false}; return nil }); err != nil {
		t.Fatal(err)
	}
	cfg = reopenRPCStoreFromDisk(t, m.store).Read()
	value, err := resolveResourcePreference(cfg, id, time.Now())
	if err != nil || value.Enabled || value.Requested == nil || *value.Requested || value.Managed != nil {
		t.Fatal("false override collapsed into absent/default", value, err)
	}
	cfg.CachedMap.Network.Name = "tampered"
	if _, err := resolveResourcePreference(cfg, id, time.Now()); err == nil {
		t.Fatal("local override bypassed signature verification")
	}
}
