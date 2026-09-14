package client

import (
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExplicitExitProjectsMatchingRouterAndUAPI(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{"", api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		t.Run(string(family), func(t *testing.T) {
			cfg, source, key := signedApplicationFixture(t, false)
			cfg.PrivateKey = testWireGuardEngineKey(1)
			source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "0.0.0.0/0", "::/0")
			host := api.ServiceHost{NodeID: source.Peers[0].ID, PublicKey: source.Peers[0].PublicKey}
			now := time.Now()
			source.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &source, key)
			selection := &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: family, LAN: api.ExitLANBlock}
			if family == "" {
				selection = nil
			}
			before := cloneRegisterNodeResponse(source)
			router, err := buildWireGuardEngineRouterConfigForExit("endlessnet", 1420, cfg, source, selection, now)
			if err != nil {
				t.Fatal(err)
			}
			for _, emit := range []bool{false, true} {
				wire, err := wireGuardEngineUAPIForExit(cfg.PrivateKey, source, 0, false, router.FirewallMark, nil, emit, cfg, selection, now)
				if err != nil {
					t.Fatal(err)
				}
				for prefix, want := range map[string]bool{"0.0.0.0/0": family == api.ExitFamilyIPv4Only || family == api.ExitFamilyDualStack, "::/0": family == api.ExitFamilyIPv6Only || family == api.ExitFamilyDualStack, "10.1.2.3/32": true} {
					if slices.Contains(router.Routes, netip.MustParsePrefix(prefix)) != want || strings.Contains(wire, "allowed_ip="+prefix+"\n") != want {
						t.Fatalf("router/UAPI disagreed on permitted route %s", prefix)
					}
				}
			}
			if !reflect.DeepEqual(before, source) {
				t.Fatal("projection changed signed authority")
			}
			policy, err := compileExitPacketPolicy(cfg, source, selection, now)
			if err != nil || !exitPacketAllowed(policy, netip.MustParseAddr("10.1.2.3"), now) {
				t.Fatal("TUN exit gate disagreed with authorized application route", err)
			}
			// Persisted intent cannot bypass the explicit adapter-only entry point.
			cfg.ExitSelection = selection
			ordinary, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range ordinary.Routes {
				if route.Bits() == 0 {
					t.Fatal("ordinary configure inferred exit authority")
				}
			}
		})
	}
}

func TestExplicitExitProjectionRejectsAuthorityAndRouteSuppression(t *testing.T) {
	for _, scenario := range []string{"tampered", "expired", "wrong_recipient", "routing_off", "accept_routes_false"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, source, key := signedApplicationFixture(t, false)
			cfg.PrivateKey = testWireGuardEngineKey(1)
			source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: source.Peers[0].ID, PublicKey: source.Peers[0].PublicKey}
			now := time.Now()
			source.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &source, key)
			selection := &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
			switch scenario {
			case "tampered":
				source.Network.Applications[0].Name = "changed-after-signing"
			case "expired":
				now = now.Add(2 * time.Hour)
			case "wrong_recipient":
				selection.NodeID = "other"
			case "routing_off":
				cfg.WireGuardRouteTable = "off"
			case "accept_routes_false":
				no := false
				cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptRoutes: &no}
			}
			if router, err := buildWireGuardEngineRouterConfigForExit("endlessnet", 1420, cfg, source, selection, now); err == nil || len(router.Routes) != 0 {
				t.Fatal("invalid selection produced OS route input")
			}
			if scenario != "routing_off" && scenario != "accept_routes_false" {
				if wire, err := wireGuardEngineUAPIForExit(cfg.PrivateKey, source, 0, false, 51820, nil, false, cfg, selection, now); err == nil || wire != "" {
					t.Fatal("invalid authority produced device configuration")
				}
			}
		})
	}
}
