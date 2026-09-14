package client

import (
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCResourcesPreserveExplicitSingleIPSubnet(t *testing.T) {
	for _, scenario := range []string{"host_only", "managed", "tampered", "hidden_peer", "undisclosed_prefix"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			opts, key := signedServiceDNSFixture(t)
			networkMap := opts.NetworkMap
			networkMap.Peers[0].AllowedIPs = []string{"100.64.0.2/32", "fd00::2/128", "0.0.0.0/0", "::/0"}
			if scenario != "host_only" {
				for _, cidr := range networkMap.Peers[0].AllowedIPs {
					if networkMap.Network.ClientPolicy == nil {
						networkMap.Network.ClientPolicy = &api.ClientPolicy{}
					}
					networkMap.Network.ClientPolicy.Resources = append(networkMap.Network.ClientPolicy.Resources, api.ManagedResourceSetting{
						Kind: api.ManagedResourceSubnet, ID: networkMap.Peers[0].ID, CIDR: cidr,
						Source: api.ClientPolicyAccount, PolicyID: "subnet-policy", Locked: true, Enabled: false,
					})
				}
			}
			resignApplicationMap(t, &networkMap, key)
			switch scenario {
			case "tampered":
				networkMap.Network.ClientPolicy.Resources[0].Enabled = true
			case "hidden_peer":
				networkMap.Network.ClientPolicy.Resources[0].ID = "hidden"
				resignApplicationMap(t, &networkMap, key)
			case "undisclosed_prefix":
				networkMap.Network.ClientPolicy.Resources[0].CIDR = "192.0.2.1/32"
				resignApplicationMap(t, &networkMap, key)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, opts.SigningTrust
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			s := NewClientRPCService(m, nil)
			response, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile})
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("projection changed policy or map")
			}
			if scenario != "host_only" && scenario != "managed" {
				if err == nil || response != nil {
					t.Fatal("invalid policy disclosed resources")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var subnets []string
			hosts := 0
			seen := map[string]bool{}
			for _, resource := range response.Resources {
				if seen[resource.Id] || resource.Enabled == nil {
					t.Fatal("duplicate identity or missing policy resolution")
				}
				seen[resource.Id] = true
				if host := resource.GetHost(); host != nil {
					hosts++
					if !slices.Equal(host.Addresses, []string{"100.64.0.2", "fd00::2"}) {
						t.Fatal("explicit subnet erased host identity", host)
					}
				}
				if subnet := resource.GetSubnet(); subnet != nil {
					if !netip.MustParsePrefix(subnet.Cidr).IsSingleIP() {
						t.Fatal("default route leaked through managed subnet policy")
					}
					subnets = append(subnets, subnet.Cidr)
				}
			}
			slices.Sort(subnets)
			if hosts != 1 || (scenario == "host_only" && len(subnets) != 0) || (scenario == "managed" && !slices.Equal(subnets, []string{"100.64.0.2/32", "fd00::2/128"})) {
				t.Fatal("single-IP resource classification lost", hosts, subnets)
			}
		})
	}
}

func TestRPCResourceServiceTargetsSearchAndIdentity(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	opts, key := signedServiceDNSFixture(t)
	networkMap := opts.NetworkMap
	networkMap.Network.Services[0].Ports = []api.ServicePort{
		{Protocol: "tcp", Port: 5432},
		{Protocol: "udp", Port: 5432},
		{Protocol: "tcp", Port: 6432},
	}
	install := func() {
		t.Helper()
		resignApplicationMap(t, &networkMap, key)
		if err := m.store.Update(func(cfg *Config) error {
			cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
			cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
			cfg.CachedMap, cfg.MapSigningTrust = &networkMap, opts.SigningTrust
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	install()
	s := NewClientRPCService(m, nil)
	before := m.store.Read()
	ids := map[string]string{}
	for _, tc := range []struct {
		search string
		want   []string
	}{
		{"", []string{"tcp:5432", "tcp:6432", "udp:5432"}},
		{"5432", []string{"tcp:5432", "udp:5432"}},
		{"  TCP  ", []string{"tcp:5432", "tcp:6432"}},
		{"db.account.endlessnet:6432", []string{"tcp:6432"}},
		{"UDP", []string{"udp:5432"}},
		{"db", []string{"tcp:5432", "tcp:6432", "udp:5432"}},
		{"9999", nil},
	} {
		t.Run(tc.search, func(t *testing.T) {
			response, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile, Search: tc.search, Kinds: []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_SERVICE}})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			seen := map[string]bool{}
			for _, resource := range response.Resources {
				target := resource.GetService()
				if target == nil || target.Hostname != "db.account.endlessnet" || resource.Id == "" || seen[resource.Id] || resource.Enabled == nil {
					t.Fatal("invalid, duplicated or falsely enabled service resource", resource)
				}
				seen[resource.Id] = true
				name := target.Protocol + ":" + strconv.FormatUint(uint64(target.Port), 10)
				if previous, ok := ids[name]; ok && previous != resource.Id {
					t.Fatal("search changed service identity")
				}
				ids[name] = resource.Id
				got = append(got, name)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("targets = %v, want %v", got, tc.want)
			}
		})
	}
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("service search changed signed state")
	}
	slices.Reverse(networkMap.Network.Services[0].Ports)
	networkMap.Network.Services[0].Name = "renamed"
	install()
	response, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile, Kinds: []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_SERVICE}})
	if err != nil || len(response.GetResources()) != 3 {
		t.Fatal("reordered service catalog unavailable", err)
	}
	for _, resource := range response.Resources {
		target := resource.GetService()
		name := target.Protocol + ":" + strconv.FormatUint(uint64(target.Port), 10)
		if resource.Id != ids[name] || resource.DisplayName != "renamed" {
			t.Fatal("service identity depends on ordering or display name")
		}
	}
}

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
					if resource.Id == "" || seen[resource.Id] || resource.Target == nil || resource.NetworkId != networkMap.Network.ID || resource.Enabled == nil || resource.Availability.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
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
