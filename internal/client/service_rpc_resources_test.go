package client

import (
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCResourcesAuthenticateFilterAndBindPages(t *testing.T) {
	for _, scenario := range []string{"valid", "observer", "tampered", "expired", "foreign", "bad_kind", "duplicate_kind", "long_search", "page_query", "page_map"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			m.now = func() time.Time { return now }
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "198.18.0.0/15", "0.0.0.0/0", "::/0")
			resignApplicationMap(t, &networkMap, key)
			if scenario == "tampered" {
				networkMap.Peers[0].Hostname = "unsigned change"
			}
			if scenario == "expired" {
				now = networkMap.MapSignature.ExpiresAt.Add(time.Second)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				if scenario == "foreign" {
					cfg.NodeID = "other"
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
			request := &ipc.ListResourcesRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
			switch scenario {
			case "bad_kind":
				request.Kinds = []ipc.ResourceKind{99}
			case "duplicate_kind":
				request.Kinds = []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_HOST, ipc.ResourceKind_RESOURCE_KIND_HOST}
			case "long_search":
				request.Search = string(make([]byte, 257))
			}
			before := m.store.Read()
			response, err := s.resourcesAs(t.Context(), caller, request)
			if scenario != "valid" && scenario != "page_query" && scenario != "page_map" {
				if err == nil || response != nil {
					t.Fatal("invalid resource request disclosed data")
				}
				if !reflect.DeepEqual(before, m.store.Read()) {
					t.Fatal("rejected resource read changed state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Resources) != 1 || response.Page.NextPageToken == "" {
				t.Fatal("resource catalog not paginated")
			}
			if scenario == "page_query" || scenario == "page_map" {
				request.Page.PageToken = response.Page.NextPageToken
				if scenario == "page_query" {
					request.Search = "different"
				} else {
					networkMap.Peers[0].Hostname = "new signed name"
					resignApplicationMap(t, &networkMap, key)
					if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap = &networkMap; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := s.resourcesAs(t.Context(), caller, request); err == nil {
					t.Fatal("page token crossed query/map snapshot")
				}
				return
			}
			seen := map[string]bool{}
			kinds := map[ipc.ResourceKind]bool{}
			for {
				for _, resource := range response.Resources {
					if resource.Id == "" || seen[resource.Id] || resource.Target == nil || resource.NetworkId != networkMap.Network.ID || resource.Enabled != nil || resource.Availability.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
						t.Fatal("resource identity duplicated or unobserved effect claimed")
					}
					if resource.GetSubnet().GetCidr() == "0.0.0.0/0" || resource.GetSubnet().GetCidr() == "::/0" {
						t.Fatal("exit default disclosed as ordinary subnet")
					}
					seen[resource.Id], kinds[resource.Kind] = true, true
				}
				if response.Page.NextPageToken == "" {
					break
				}
				request.Page.PageToken = response.Page.NextPageToken
				response, err = s.resourcesAs(t.Context(), caller, request)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !kinds[ipc.ResourceKind_RESOURCE_KIND_HOST] || !kinds[ipc.ResourceKind_RESOURCE_KIND_SUBNET] || !kinds[ipc.ResourceKind_RESOURCE_KIND_APPLICATION] {
				t.Fatal("authorized target kinds missing")
			}
			filtered, err := s.resourcesAs(t.Context(), caller, &ipc.ListResourcesRequest{Profile: profile, Search: "198.18", Kinds: []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_SUBNET}})
			if err != nil || len(filtered.GetResources()) != 1 || filtered.Resources[0].GetSubnet().GetCidr() != "198.18.0.0/15" {
				t.Fatal("resource search/kind intersection failed", err)
			}
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("resource projection changed signed source")
			}
		})
	}
}
