package client

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"google.golang.org/protobuf/proto"
)

func TestNetworkAcceptancePolicyAndRouterEffects(t *testing.T) {
	for _, mode := range []string{"default", "local_false", "baseline_false", "unlocked_override", "locked_false", "locked_true", "reset"} {
		t.Run(mode, func(t *testing.T) {
			cfg, source, key := signedApplicationFixture(t, false)
			source.Network.DNS = []string{"100.64.0.53"}
			source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "198.18.0.0/15", "192.0.2.1/32", "fd00::2/128")
			source.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceSubnet, ID: source.Peers[0].ID, CIDR: "192.0.2.1/32", Source: api.ClientPolicyAccount, PolicyID: "subnet", Enabled: true}}}
			want := true
			if mode != "default" && mode != "local_false" {
				baseline := mode == "locked_true"
				for _, setting := range []api.ClientSettingKey{api.ClientSettingAcceptDNS, api.ClientSettingAcceptRoutes} {
					source.Network.ClientPolicy.Settings = append(source.Network.ClientPolicy.Settings, api.ManagedClientSetting{Key: setting, Source: api.ClientPolicyDevice, PolicyID: "network", BooleanValue: proto.Bool(baseline), Locked: mode == "locked_false" || mode == "locked_true"})
				}
			}
			switch mode {
			case "local_false", "locked_true":
				cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(false), AcceptRoutes: proto.Bool(false)}
			case "unlocked_override", "locked_false":
				cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(true), AcceptRoutes: proto.Bool(true)}
			case "reset":
				cfg.NetworkPreferences = &ClientNetworkPreferences{}
			}
			if mode == "local_false" || mode == "baseline_false" || mode == "locked_false" || mode == "reset" {
				want = false
			}
			resignApplicationMap(t, &source, key)
			before := cloneRegisterNodeResponse(source)
			acceptance, err := resolveNetworkAcceptance(cfg, source, time.Now())
			if err != nil || acceptance.dns != want || acceptance.routes != want {
				t.Fatal("incorrect managed/user intersection", acceptance, err)
			}
			router, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(router.Routes, netip.MustParsePrefix("100.64.0.2/32")) || !slices.Contains(router.Routes, netip.MustParsePrefix("fd00::2/128")) {
				t.Fatal("ordinary peer host routes were removed")
			}
			for _, value := range []string{"198.18.0.0/15", "192.0.2.1/32", "10.1.2.3/32"} {
				if slices.Contains(router.Routes, netip.MustParsePrefix(value)) != want {
					t.Fatal("resource route acceptance not applied", value)
				}
			}
			if want {
				if router.DNSProxy == nil || len(router.DNS) == 0 || len(router.DNSDomains) == 0 {
					t.Fatal("accepted service/application DNS missing")
				}
			} else if router.DNSProxy != nil || len(router.DNS) != 0 || len(router.DNSDomains) != 0 || len(router.SearchDomains) != 0 || router.DNSOverride || !router.DNSConfigPresent {
				t.Fatal("declined DNS left resolver/proxy settings active")
			}
			if !reflect.DeepEqual(before, source) {
				t.Fatal("preference mutated signed source")
			}
		})
	}
}

func TestNetworkAcceptanceRejectsUntrustedContext(t *testing.T) {
	for _, mode := range []string{"tampered", "removed_policy", "expired", "recipient", "network", "trust", "missing_signature"} {
		t.Run(mode, func(t *testing.T) {
			cfg, source, key := signedApplicationFixture(t, false)
			source.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingAcceptDNS, BooleanValue: proto.Bool(false), Source: api.ClientPolicyAccount, PolicyID: "dns", Locked: true}}}
			resignApplicationMap(t, &source, key)
			now := time.Now()
			switch mode {
			case "tampered":
				source.Network.ClientPolicy.Settings[0].BooleanValue = proto.Bool(true)
			case "removed_policy":
				source.Network.ClientPolicy = nil
			case "expired":
				now = source.MapSignature.ExpiresAt
			case "recipient":
				cfg.NodeID = "foreign"
			case "network":
				cfg.NetworkID = "foreign"
			case "trust":
				cfg.MapSigningTrust = nil
			case "missing_signature":
				source.MapSignature = nil
			}
			if _, err := resolveNetworkAcceptance(cfg, source, now); err == nil {
				t.Fatal("untrusted policy supplied an effective preference")
			}
		})
	}
}

func TestNetworkPreferencePresenceSurvivesStore(t *testing.T) {
	m := newRPCStoreTest(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(false)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stored := reopenRPCStoreFromDisk(t, m.store).Read().NetworkPreferences
	if stored == nil || stored.AcceptDNS == nil || *stored.AcceptDNS || stored.AcceptRoutes != nil {
		t.Fatal("false and absence collapsed across restart")
	}
	*stored.AcceptDNS = true
	if *m.store.Read().NetworkPreferences.AcceptDNS {
		t.Fatal("caller mutated the protected preference store")
	}
}
