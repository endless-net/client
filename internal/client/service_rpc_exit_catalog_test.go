package client

import (
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCExitCatalogUsesSignedBoundGrants(t *testing.T) {
	for _, scenario := range []string{"valid", "tampered", "foreign_node", "expired", "absent_policy", "wrong_host", "observer", "expired_page"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			m.now = func() time.Time { return now }
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			grant := api.ExitNodeGrant{ID: "exit-a", Name: "Exit A", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{grant}}
			switch scenario {
			case "expired":
				networkMap.Network.ClientPolicy.ExitNodes[0].ExpiresAt = now.Add(-time.Minute)
			case "absent_policy":
				networkMap.Network.ClientPolicy = nil
			case "wrong_host":
				networkMap.Network.ClientPolicy.ExitNodes[0].Host.PublicKey = "wrong-key"
			case "expired_page":
				other := grant
				other.ID = "exit-b"
				networkMap.Network.ClientPolicy.ExitNodes = append(networkMap.Network.ClientPolicy.ExitNodes, other)
			}
			resignApplicationMap(t, &networkMap, key)
			if scenario == "tampered" {
				networkMap.Network.ClientPolicy.ExitNodes[0].Name = "Changed after signing"
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				if scenario == "foreign_node" {
					cfg.NodeID = "foreign"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			caller := owner
			if scenario == "observer" {
				caller = local.Peer{Identity: "uid:other"}
			}
			response, err := s.exitNodesAs(t.Context(), caller, &ipc.ListExitNodesRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}})
			if scenario == "tampered" || scenario == "foreign_node" || scenario == "wrong_host" || scenario == "observer" {
				if err == nil {
					t.Fatal("invalid or unauthorized catalog accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" || scenario == "absent_policy" {
				if len(response.ExitNodes) != 0 {
					t.Fatal("default route inferred an exit without a live grant")
				}
				return
			}
			if len(response.ExitNodes) != 1 || response.ExitNodes[0].Id != grant.ID || response.ExitNodes[0].PeerId != host.NodeID || response.ExitNodes[0].Selection.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE || len(response.ExitNodes[0].AllowedFamilyModes) != 0 {
				t.Fatal("invalid signed grant projection or false apply claim")
			}
			if scenario == "expired_page" {
				if response.Page.NextPageToken == "" {
					t.Fatal("missing page token")
				}
				now = now.Add(time.Minute)
				_, err := s.exitNodesAs(t.Context(), owner, &ipc.ListExitNodesRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1, PageToken: response.Page.NextPageToken}})
				if err == nil {
					t.Fatal("expired grants retained old page binding")
				}
			}
		})
	}
}
