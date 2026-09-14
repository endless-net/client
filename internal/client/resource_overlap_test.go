package client

import (
	"net/netip"
	"slices"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceOverlapProtocolPortAndFamily(t *testing.T) {
	rule := func(cidr string, p byte, port uint16) resourceDenyRule {
		return resourceDenyRule{prefix: netip.MustParsePrefix(cidr), protocol: p, port: port}
	}
	base := rule("192.0.2.1/32", 6, 443)
	for _, tc := range []struct {
		other resourceDenyRule
		want  bool
	}{
		{rule("192.0.2.0/24", 0, 0), true},
		{rule("192.0.2.1/32", 6, 443), true},
		{rule("192.0.2.1/32", 6, 80), false},
		{rule("192.0.2.1/32", 17, 443), false},
		{rule("192.0.2.2/32", 6, 443), false},
		{rule("2001:db8::/32", 0, 0), false},
	} {
		if resourceRulesOverlap(base, tc.other) != tc.want || resourceRulesOverlap(tc.other, base) != tc.want {
			t.Fatal("incorrect symmetric overlap", tc)
		}
	}
}

func TestResourceCatalogOverlapSurvivesKindFilter(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	all, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	serviceOnly, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile, Kinds: []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_SERVICE}})
	if err != nil {
		t.Fatal(err)
	}
	overlaps := 0
	for _, service := range serviceOnly.Resources {
		for _, id := range service.OverlappingResourceIds {
			for _, other := range all.Resources {
				if other.Id == id && other.Kind == ipc.ResourceKind_RESOURCE_KIND_HOST {
					overlaps++
					if !slices.Contains(other.OverlappingResourceIds, service.Id) || service.OverlapReasonKey != "resource_packet_scope_overlap" {
						t.Fatal("asymmetric catalog overlap")
					}
				}
			}
		}
	}
	if overlaps == 0 {
		t.Fatal("service filtering hid overlapping host")
	}
}
