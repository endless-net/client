package client

import (
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitRouteProjectionRequiresExactLiveSelection(t *testing.T) {
	for _, scenario := range []string{"none", "ipv4", "ipv6", "dual", "expired", "tampered", "foreign_recipient", "replaced_peer", "forbidden_mode", "forbidden_lan"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0", "::/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			grant := api.ExitNodeGrant{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{grant}}
			selection := &ClientExitSelection{ID: grant.ID, NetworkID: cfg.NetworkID, NodeID: cfg.NodeID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
			switch scenario {
			case "none":
				selection = nil
			case "ipv6":
				selection.Family = api.ExitFamilyIPv6Only
			case "dual":
				selection.Family = api.ExitFamilyDualStack
			case "expired":
				networkMap.Network.ClientPolicy.ExitNodes[0].ExpiresAt = now.Add(-time.Second)
			case "foreign_recipient":
				selection.NodeID = "other"
			case "replaced_peer":
				selection.Host.PublicKey = "other"
			case "forbidden_mode":
				networkMap.Network.ClientPolicy.ExitNodes[0].AllowedFamilyModes = []api.ExitFamilyMode{api.ExitFamilyIPv6Only}
			case "forbidden_lan":
				selection.LAN = api.ExitLANAllow
			}
			resignApplicationMap(t, &networkMap, key)
			if scenario == "tampered" {
				networkMap.Network.ClientPolicy.ExitNodes[0].Name = "tampered"
			}
			before := cloneRegisterNodeResponse(networkMap)
			peers, err := exitRoutePeers(cfg, networkMap, selection, now)
			if scenario != "none" && scenario != "ipv4" && scenario != "ipv6" && scenario != "dual" {
				if err == nil || peers != nil {
					t.Fatal("rejected selection fell back to routing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, networkMap) {
				t.Fatal("projection mutated signed map")
			}
			for _, peer := range peers {
				want4 := peer.ID == host.NodeID && (scenario == "ipv4" || scenario == "dual")
				want6 := peer.ID == host.NodeID && (scenario == "ipv6" || scenario == "dual")
				if slices.Contains(peer.AllowedIPs, "0.0.0.0/0") != want4 || slices.Contains(peer.AllowedIPs, "::/0") != want6 {
					t.Fatal("route family selection was widened or downgraded")
				}
			}
			for _, original := range before.Peers[0].AllowedIPs {
				if original != "0.0.0.0/0" && original != "::/0" && !slices.Contains(peers[0].AllowedIPs, original) {
					t.Fatal("ordinary route removed")
				}
			}
		})
	}
}
