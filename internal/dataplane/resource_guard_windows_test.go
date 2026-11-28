//go:build windows

package dataplane

import (
	"errors"
	"net/netip"
	"testing"
	"time"
	"unsafe"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wf"
)

func TestWindowsExitFilterPublicScopeAndRecovery(t *testing.T) {
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID, m.Peers[0].ID = "recipient", "router"
	m.Peers[0].EndpointExpiresAt = &end
	router := coordinator.ServiceHost{NodeID: "router", PublicKey: m.Peers[0].PublicKey}
	m.Network.Applications = []coordinator.Application{
		{ID: "exit", TargetType: "cidr", Target: "0.0.0.0/0", AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}},
			Connectors: []coordinator.ServiceHost{router}, Routes: []coordinator.ApplicationRoute{{Connector: router, Metric: 1, ExpiresAt: end, CIDRs: []string{"0.0.0.0/0"}}}},
		{ID: "withdrawn", TargetType: "cidr", Target: "8.8.8.0/24"},
	}
	plan, err := filterPlan(m, policySession, 12345, 7, 32, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := plan.withExitTransport(`\device\harddiskvolume1\endlessnet\client.exe`)
	if err != nil {
		t.Fatal(err)
	}
	var protocols = map[wf.IPProto]int{}
	for _, rule := range transport.rules {
		if !exitTransportRule(rule) {
			continue
		}
		if !rule.Conditions[3].Value.(netip.Prefix).Contains(netip.MustParseAddr("8.8.4.4")) {
			continue
		}
		protocols[rule.Conditions[4].Value.(wf.IPProto)]++
		if rule.Conditions[2].Value != uint64(12345) || rule.Conditions[2].Op != wf.MatchTypeNotEqual {
			t.Fatal("tunnel exception")
		}
		var arena filterArena
		defer arena.close()
		a, err := marshalNativeFilter(&arena, rule)
		if err != nil {
			t.Fatal(err)
		}
		b, err := marshalNativeFilter(&arena, rule)
		if err != nil || !sameNativeFilter(a, b) {
			t.Fatal("service permit identity", err)
		}
		conditions := unsafe.Slice(b.Conditions, b.Count)
		for _, index := range []int{0, 1} {
			blob := (*nativeBlob)(nativePointer(conditions[index].Value.Value))
			*blob.Data ^= 1
			if sameNativeFilter(a, b) {
				t.Fatal("changed service identity accepted", index)
			}
			*blob.Data ^= 1
		}
	}
	if protocols[6] != 1 || protocols[17] != 1 {
		t.Fatal(protocols)
	}
	_, _, _, journal := ipFixture(t)
	journal.Filters = plan.Records()
	const needed = 4 // local address, peer, exit and independently withdrawn subnet
	recovered, _, err := recoverGuardPlan(journal, needed)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := recoverGuardPlan(journal, needed-1); !errors.Is(err, ErrPolicyLimit) {
		t.Fatal("recovery failed to charge independent scope", err)
	}
	for i := range journal.Filters {
		r := &journal.Filters[i]
		previous := r.ExitOverlay
		if previous != "" {
			r.ExitOverlay = "100.65.0.0/24"
		} else if r.GuardAddress == "8.8.8.0/24" {
			r.ExitOverlay = m.Network.CIDR
		} else {
			continue
		}
		if _, _, err := recoverGuardPlan(journal, needed); !errors.Is(err, ErrPolicyOwnership) {
			t.Fatal("unverified exit scope", err)
		}
		r.ExitOverlay = previous
	}
	ip, err := ipPlan(m, policySession, 12345, 10, 8)
	if err != nil {
		t.Fatal(err)
	}
	_, journal.Routes = ip.Prepared()
	withdrawn := m
	withdrawn.Network.Applications = withdrawn.Network.Applications[1:]
	withoutExit, err := filterPlan(withdrawn, policySession, 12345, 7, 32, needed, false)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := withoutExit.RetainIPGuards(journal)
	if err != nil || retained.resourceCount != 2 {
		t.Fatal("guarding unfinished routes after withdrawal", retained.resourceCount, err)
	}
	for _, sample := range []struct {
		address      string
		block, allow bool
	}{
		{"8.8.4.4", true, true}, {"8.8.8.8", true, false},
		{"10.0.0.1", false, false}, {"127.0.0.1", false, false},
		{"100.64.0.9", false, false}, {"224.0.0.1", false, false},
	} {
		ip := netip.MustParseAddr(sample.address)
		check := func(p FilterPlan, wantAllow bool) {
			t.Helper()
			var block, allow bool
			for _, r := range p.rules {
				if !supportedNativeRule(r) {
					t.Fatal("unsupported rule", r)
				}
				if prefix, ok := resourceGuardPrefix(r); ok && prefix.Contains(ip) {
					block = true
				}
				if r.Layer == wf.LayerALEAuthConnectV4 && r.Action == wf.ActionPermit {
					for _, c := range r.Conditions {
						if prefix, ok := c.Value.(netip.Prefix); ok && prefix.Contains(ip) {
							allow = true
						}
					}
				}
			}
			if block != sample.block || allow != wantAllow {
				t.Fatal(sample.address, block, allow)
			}
		}
		check(plan, sample.allow)
		check(plan.currentGrants(end), false)
		check(recovered, false)
	}
}

func TestWindowsResourcePermitsRespectNestedTargetsAndExpiry(t *testing.T) {
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID, m.Peers[0].ID = "source", "router"
	m.Peers[0].EndpointExpiresAt = &end
	router := coordinator.ServiceHost{NodeID: "router", PublicKey: m.Peers[0].PublicKey}
	active := func(id, prefix string, port uint32) coordinator.Application {
		return coordinator.Application{ID: id, TargetType: "cidr", Target: prefix,
			AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: port}},
			Connectors:   []coordinator.ServiceHost{router},
			Routes:       []coordinator.ApplicationRoute{{Connector: router, Metric: 1, ExpiresAt: end, CIDRs: []string{prefix}}}}
	}
	m.Network.Applications = []coordinator.Application{
		active("broad", "10.27.4.0/24", 22), active("narrow", "10.27.4.0/25", 443),
		{ID: "unavailable", TargetType: "cidr", Target: "10.27.4.32/27"},
	}
	for _, offline := range []bool{false, true} {
		p, err := filterPlan(m, policySession, 12345, 7, 32, 16, offline)
		if err != nil {
			t.Fatal(err)
		}
		if p.resourceCount != 3 {
			t.Fatal("missing persistent blocks", p.resourceCount)
		}
		for _, sample := range []struct {
			address string
			port    uint16
		}{
			{"10.27.4.128", 22}, {"10.27.4.31", 443}, {"10.27.4.32", 0}, {"10.27.5.1", 0},
		} {
			for _, at := range []time.Time{end.Add(-time.Nanosecond), end} {
				var packets, connects int
				for _, r := range p.currentGrants(at).rules {
					if !supportedNativeRule(r) {
						t.Fatalf("unsupported filter %v", r)
					}
					if r.Action != wf.ActionPermit {
						continue
					}
					fields := map[wf.FieldID]any{}
					for _, c := range r.Conditions {
						fields[c.Field] = c.Value
					}
					prefix, ok := fields[wf.FieldIPRemoteAddress].(netip.Prefix)
					if !ok || !prefix.Contains(netip.MustParseAddr(sample.address)) {
						continue
					}
					if fields[wf.FieldIPLocalInterface] != uint64(12345) || fields[wf.FieldIPLocalAddress] != netip.MustParseAddr(m.Node.AssignedIP) {
						t.Fatal("unscoped permit", r)
					}
					switch r.Layer {
					case wf.LayerOutboundIPPacketV4:
						if fields[wf.FieldInterfaceIndex] != uint32(7) {
							t.Fatal("wrong interface", r)
						}
						packets++
					case wf.LayerALEAuthConnectV4:
						if fields[wf.FieldIPRemotePort] != sample.port || fields[wf.FieldIPProtocol] != wf.IPProto(6) || (fields[wf.FieldFlags] == wf.ConditionFlagIsReauthorize) != offline {
							t.Fatal("wrong ALE conditions", r)
						}
						connects++
					default:
						t.Fatal("unexpected resource permit", r)
					}
				}
				want := 0
				if sample.port != 0 && at.Before(end) {
					want = 1
				}
				if packets != want || connects != want {
					t.Fatal(sample, at, packets, connects)
				}
			}
		}
	}
}

func TestWindowsRetainsWithdrawnHostResourceUntilRouteCleanup(t *testing.T) {
	m, _ := engineMap(t)
	m.Peers = nil
	plan, err := filterPlan(m, policySession, 12345, 17, 8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, journal := ipFixture(t)
	const target = "10.27.4.5/32"
	journal.Routes = []state.RouteRecord{{Destination: target, NextHop: "0.0.0.0", Resource: true, EffectStamp: state.EffectStamp{Phase: state.EffectRemoving}}}
	retained, err := plan.RetainIPGuards(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range retained.rules {
		if prefix, ok := resourceGuardPrefix(r); ok && prefix.String() == target && r.Persistent && r.Action == wf.ActionBlock {
			return
		}
	}
	t.Fatal("withdrawn /32 resource lost its durable outbound block")
}

func TestWindowsUnavailableSubnetGuardOwnershipAndWithdrawal(t *testing.T) {
	m, _ := engineMap(t)
	m.Network.CIDR = "100.64.0.0/24"
	m.Network.Applications = []coordinator.Application{{ID: "subnet", TargetType: "cidr", Target: "192.0.2.0/25"}}
	f, _ := filterController(t)
	plan := func(offline bool) FilterPlan {
		t.Helper()
		p, err := filterPlan(m, policySession, 12345, 17, 8, 8, offline)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := plan(false)
	var block *wf.Rule
	for _, r := range p.rules {
		if prefix, ok := resourceGuardPrefix(r); ok {
			if block != nil || prefix.String() != "192.0.2.0/25" {
				t.Fatal("wrong resource block")
			}
			block = r
		}
	}
	if block == nil {
		t.Fatal("missing outbound block")
	}
	var arena filterArena
	defer arena.close()
	wire, err := marshalNativeFilter(&arena, block)
	if err != nil {
		t.Fatal(err)
	}
	v := wire.Conditions.Value
	if v.Type != fwpV4AddrMask || v.Value == 0 {
		t.Fatal("expected WFP IPv4 mask")
	}
	mask := (*nativeV4AddrMask)(nativePointer(v.Value))
	if mask.Address != 0xc0000200 || mask.Mask != 0xffffff80 {
		t.Fatal("unexpected native address or mask", mask)
	}
	copy, err := marshalNativeFilter(&arena, block)
	if err != nil || !sameNativeFilter(wire, copy) {
		t.Fatal("owned native rule unrecognized", err)
	}
	(*nativeV4AddrMask)(nativePointer(copy.Conditions.Value.Value)).Mask = 0xffffff00
	if sameNativeFilter(wire, copy) {
		t.Fatal("a broader native filter passed ownership validation")
	}
	records, err := f.Replace(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	recovered, ids := guardRecoveryFixture(t, records)
	if ids[block.ID] == 0 || recovered.resourceCount != 1 {
		t.Fatal("persistent block lost its recovery receipt")
	}
	if _, err = f.Replace(t.Context(), plan(true)); err != nil || f.guards.ids[block.ID] != ids[block.ID] {
		t.Fatal("offline changed block identity", err)
	}
	m.Network.Applications = nil
	if _, err = f.Replace(t.Context(), plan(false)); err != nil {
		t.Fatal(err)
	}
	if _, exists := f.guards.rules[block.ID]; exists {
		t.Fatal("withdrawn subnet still blocked")
	}
	for _, r := range records {
		if r.GuardAddress == "192.0.2.0/25" {
			r.GuardAddress = "192.0.2.0/24"
			j := state.ResourceJournal{ID: policySession, Adapter: &state.AdapterRecord{ID: policySession, LUID: 12345}, Filters: []state.FilterRecord{r}}
			if _, _, err := recoverGuardPlan(j, 8); !errors.Is(err, ErrPolicyOwnership) {
				t.Fatal("altered journal prefix accepted")
			}
		}
	}
}

func TestWindowsSubnetGuardReplacementChecksOldIdentityAndKeepsRecoveryIDs(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		m, _ := engineMap(t)
		m.Network.CIDR = "100.64.0.0/24"
		apply := func(target string) FilterPlan {
			m.Network.Applications = []coordinator.Application{{ID: "app", TargetType: "cidr", Target: target}}
			p, err := filterPlan(m, policySession, 12345, 17, 8, 3, false)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
		f, _ := filterController(t)
		old, err := f.Replace(t.Context(), apply("10.81.0.0/24"))
		if err != nil {
			t.Fatal(err)
		}
		guards := f.guards.api.(*mockFilterAPI)
		for id, rule := range f.guards.rules {
			if _, resource := resourceGuardPrefix(rule); resource && foreign {
				guards.live[id]++
			}
		}
		if !foreign {
			guards.fail = "commit"
		}
		expected := ErrPolicyUncertain
		if foreign {
			expected = ErrPolicyOwnership
		}
		newPlan := apply("10.82.0.0/24")
		if _, err := f.Replace(t.Context(), newPlan); !errors.Is(err, expected) {
			t.Fatal("wrong replacement outcome", err)
		}
		if foreign {
			if guards.removes != 0 {
				t.Fatal("foreign block deleted")
			}
			continue
		}
		var journal []state.FilterRecord
		for _, r := range old {
			if r.GuardAddress == "10.81.0.0/24" {
				r.Phase = state.EffectRemoving
				journal = append(journal, r)
			}
		}
		journal = append(journal, guardRecords(newPlan, f.guards.ids)...)
		p, ids, err := recoverGuardPlan(state.ResourceJournal{ID: policySession, Adapter: &state.AdapterRecord{ID: policySession, LUID: 12345}, Filters: journal}, 3)
		if err != nil || p.resourceCount != 2 || len(ids) != len(f.guards.ids) {
			t.Fatal("uncertain replacement lost one generation", err)
		}
	}
}
