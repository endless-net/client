package client

import (
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceAppliedDenialProjectionRequiresObservation(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, m.store.Read().CachedMap.Peers[0].ID)
	if err := m.store.Update(func(cfg *Config) error { cfg.ResourcePreferences = map[string]bool{id: false}; return nil }); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	confirmed := false
	calls := 0
	s.ResourceEnforcementProvider = func(cfg Config, now time.Time) bool {
		calls++
		if cfg.ResourcePreferences[id] || cfg.CachedMap == nil || now.IsZero() {
			t.Fatal("provider lost bound configuration")
		}
		return confirmed
	}
	for _, observed := range []bool{false, true, false} {
		confirmed = observed
		rows, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		found, overlap := false, false
		for _, row := range rows.Resources {
			want := "resource_runtime_observation_unavailable"
			if observed && (row.Id == id || row.Kind == ipc.ResourceKind_RESOURCE_KIND_SERVICE) {
				want = "resource_packet_restriction_applied"
			}
			if row.Id == id {
				found = true
			}
			if row.Kind == ipc.ResourceKind_RESOURCE_KIND_SERVICE && row.Availability.ReasonKey == "resource_packet_restriction_applied" {
				overlap = true
			}
			if row.Availability.ReasonKey != want || row.Availability.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
				t.Fatal("unconfirmed reachability or denial", row)
			}
		}
		if !found || observed && !overlap {
			t.Fatal("host denial did not restrict overlapping service")
		}
	}
	if calls != 3 {
		t.Fatal("observation was cached or repeated per resource")
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Name = "tampered"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.resourcesAs(t.Context(), owner, &ipc.ListResourcesRequest{Profile: profile}); err == nil || calls != 3 {
		t.Fatal("unauthenticated map reached provider")
	}
}
