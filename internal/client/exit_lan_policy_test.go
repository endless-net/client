package client

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitLANReservationRequiresSignedAuthorityAndPreservesResources(t *testing.T) {
	cfg, source, key := signedApplicationFixture(t, false)
	now := time.Now()
	source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "0.0.0.0/0", "::/0", "192.0.2.128/26")
	host := api.ServiceHost{NodeID: source.Peers[0].ID, PublicKey: source.Peers[0].PublicKey}
	source.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyDualStack}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANAllow}}}}
	resignApplicationMap(t, &source, key)
	cfg.ResourcePreferences = map[string]bool{rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, source.Peers[0].ID+"\x00"+"192.0.2.128/26"): false}
	selection := &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyDualStack, LAN: api.ExitLANAllow, RouteTable: cfg.WireGuardRouteTable}
	retained := []netip.Prefix{netip.MustParsePrefix("192.0.2.64/27")}
	policy, err := compileExitLANReservation(cfg, source, selection, retained, now)
	if err != nil {
		t.Fatal(err)
	}
	topology := &exitLANSource{Family: selection.Family, OwnInterface: "endlessnet", ValidUntil: now.Add(20 * time.Second), Links: []exitLANLink{{Index: 2, LinkIndex: 2, Name: "eth0", DevicePath: "/sys/devices/pci0000:00/0000:00:01.0", Driver: "igc", Subsystem: "pci", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24"), netip.MustParsePrefix("fd19::2/64")}, Routes: []exitLANDirectRoute{{Prefix: netip.MustParsePrefix("192.0.2.0/24")}, {Prefix: netip.MustParsePrefix("fd19::/64")}}}}}
	plan, err := compileExitLANPlan(cfg, source, selection, topology, retained, now)
	if err != nil || len(plan.bindings) != 2 || !plan.expires.Equal(topology.ValidUntil) || plan.bindings[0].connected != topology.Links[0].Routes[0].Prefix || !slices.Equal(plan.bindings[0].sources, []netip.Addr{netip.MustParseAddr("192.0.2.2")}) {
		t.Fatal("plan lost exact connected/source binding or address deadline", err)
	}
	if _, err := compileExitLANPlan(cfg, source, selection, topology, retained, topology.ValidUntil); err == nil {
		t.Fatal("expired address scope produced plan")
	}
	topology.Family = api.ExitFamilyIPv4Only
	if _, err := compileExitLANPlan(cfg, source, selection, topology, retained, now); err == nil {
		t.Fatal("partial-family observation became dual-stack authority")
	}
	topology.Family = selection.Family
	topology.Links[0].Routes = topology.Links[0].Routes[:1]
	if _, err := compileExitLANPlan(cfg, source, selection, topology, retained, now); err == nil {
		t.Fatal("empty selected IPv6 family became successful dual-stack ALLOW")
	}
	topology.Links[0].Addresses[0] = netip.MustParsePrefix("198.51.100.2/24")
	if plan.topology.Links[0].Addresses[0].String() != "192.0.2.2/24" || len(plan.topology.Links[0].Routes) != 2 {
		t.Fatal("plan aliases caller topology")
	}
	contains := func(prefixes []netip.Prefix, address string) bool {
		return slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(netip.MustParseAddr(address)) })
	}
	fragments, err := policy.destinations(netip.MustParsePrefix("192.0.2.0/24"), now)
	if err != nil || !contains(fragments, "192.0.2.1") || !contains(fragments, "192.0.2.254") || contains(fragments, "192.0.2.0") || contains(fragments, "192.0.2.255") || contains(fragments, "192.0.2.70") || contains(fragments, "192.0.2.150") {
		t.Fatal("public LAN scope expanded into reserved/broadcast destinations", fragments, err)
	}
	if !contains(policy.prefixes, netip.MustParsePrefix(source.Network.CIDR).Addr().String()) {
		t.Fatal("overlay address space was not reserved")
	}
	for _, value := range []string{"169.254.0.0/16", "fe80::/64", "224.0.0.0/4", "ff00::/8", "127.0.0.0/8"} {
		got, err := policy.destinations(netip.MustParsePrefix(value), now)
		if err != nil || len(got) != 0 {
			t.Fatal("discovery/link-local/loopback became application LAN", value, got, err)
		}
	}
	for _, value := range []string{"10.19.0.0/24", "fd19::/64"} {
		got, err := policy.destinations(netip.MustParsePrefix(value), now)
		if err != nil || len(got) == 0 {
			t.Fatal("private/ULA on-link candidates were lost", value, err)
		}
	}
	for _, app := range source.Network.Applications {
		for _, route := range app.Routes {
			for _, value := range route.CIDRs {
				if !contains(policy.prefixes, netip.MustParsePrefix(value).Addr().String()) {
					t.Fatal("application reservation lost")
				}
			}
		}
	}
	// The declared application CIDR remains reserved before any route lease,
	// including a restart with no sticky filter history.
	withoutLease := cloneRegisterNodeResponse(source)
	withoutLease.Network.Applications[0].TargetType = "cidr"
	withoutLease.Network.Applications[0].Target = "192.0.2.16/28"
	withoutLease.Network.Applications[0].Routes = nil
	withoutLease.Network.Applications[0].DNSEnabled = false
	resignApplicationMap(t, &withoutLease, key)
	reservedTarget, err := compileExitLANReservation(cfg, withoutLease, selection, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	withoutLeaseDestinations, err := reservedTarget.destinations(netip.MustParsePrefix("192.0.2.0/24"), now)
	if err != nil || contains(withoutLeaseDestinations, "192.0.2.20") || !contains(withoutLeaseDestinations, "192.0.2.40") {
		t.Fatal("application CIDR without lease became LAN fallback", err)
	}
	// Returned data owns its prefixes; caller changes cannot erase reservations.
	retained[0] = netip.MustParsePrefix("198.51.100.0/24")
	if !contains(policy.prefixes, "192.0.2.70") {
		t.Fatal("caller changed retained reservation")
	}
	if _, err := policy.destinations(netip.MustParsePrefix("192.0.2.0/24"), policy.expires); err == nil {
		t.Fatal("expired policy still supplies LAN destinations")
	}
	for _, scenario := range []string{"tampered", "expired", "recipient", "block", "ungranted", "stale_resource_choice"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := cloneRegisterNodeResponse(source)
			local := clonePersistentConfig(cfg)
			requested := *selection
			at := now
			switch scenario {
			case "tampered":
				candidate.Network.Name = "tampered"
			case "expired":
				at = candidate.MapSignature.ExpiresAt
			case "recipient":
				local.NodeID = "foreign"
			case "block":
				requested.LAN = api.ExitLANBlock
			case "ungranted":
				requested.ID = "foreign"
			case "stale_resource_choice":
				local.ResourcePreferences = map[string]bool{"missing": false}
			}
			if _, err := compileExitLANReservation(local, candidate, &requested, nil, at); err == nil {
				t.Fatal("unconfirmed authority produced LAN scope")
			}
		})
	}
}

func TestExitLANDestinationSubtractionIsExactAcrossFamilies(t *testing.T) {
	now := time.Now()
	for _, scenario := range []struct {
		prefix string
		deny   []string
	}{
		{"192.0.2.0/24", []string{"192.0.2.7/32", "192.0.2.64/26", "192.0.2.96/27", "2001:db8::/32"}},
		{"10.0.0.0/24", []string{"10.0.0.0/26", "10.0.0.200/32"}},
		{"fd00::/120", []string{"fd00::7/128", "fd00::40/122", "192.0.2.0/24"}},
		{"2001:db8::/120", []string{"2001:db8::a0/123"}},
		{"192.0.2.0/31", nil}, {"192.0.2.0/32", nil},
		{"192.0.2.0/24", []string{"192.0.0.0/16"}},
	} {
		t.Run(scenario.prefix, func(t *testing.T) {
			original := netip.MustParsePrefix(scenario.prefix)
			policy := &exitLANReservation{expires: now.Add(time.Minute)}
			for _, value := range scenario.deny {
				policy.prefixes = append(policy.prefixes, netip.MustParsePrefix(value))
			}
			got, err := policy.destinations(original, now)
			if err != nil {
				t.Fatal(err)
			}
			for i, fragment := range got {
				if fragment != fragment.Masked() || fragment.Bits() < original.Bits() || !original.Contains(fragment.Addr()) {
					t.Fatal("subtraction widened original prefix", fragment)
				}
				for _, other := range got[i+1:] {
					if fragment.Overlaps(other) {
						t.Fatal("duplicate/overlapping fragments")
					}
				}
			}
			// Enumerate every address in these bounded prefixes independently of
			// the splitting algorithm, including /31 and /32 endpoint behavior.
			for address := original.Addr(); original.Contains(address); address = address.Next() {
				want := !slices.ContainsFunc(policy.prefixes, func(p netip.Prefix) bool { return p.Contains(address) })
				if original.Addr().Is4() && original.Bits() <= 30 && (address == original.Addr() || !original.Contains(address.Next())) {
					want = false
				}
				allowed := slices.ContainsFunc(got, func(p netip.Prefix) bool { return p.Contains(address) })
				if allowed != want {
					t.Fatal("incorrect subtraction at", address, "allowed", allowed)
				}
			}
		})
	}
}

func TestExitLANRetainsWithdrawnPacketFilterReservations(t *testing.T) {
	e := &WireGuardEngine{applicationFilter: newApplicationPacketFilter(), sharingFilter: newSharingPacketFilter()}
	prefix := netip.MustParsePrefix("192.0.2.0/24")
	address := netip.MustParseAddr("2001:db8::9")
	e.applicationFilter.protected[prefix] = true
	e.sharingFilter.protected[address] = true
	e.applicationFilter.withdraw()
	e.sharingFilter.withdraw()
	e.mu.Lock()
	got, err := e.retainedExitLANDestinationsLocked()
	e.mu.Unlock()
	if err != nil || !slices.Contains(got, prefix) || !slices.Contains(got, netip.PrefixFrom(address, 128)) {
		t.Fatal("withdrawal erased LAN exclusions", got, err)
	}
	for _, saturated := range []string{"application", "sharing"} {
		e.applicationFilter.saturated = saturated == "application"
		e.sharingFilter.saturated = saturated == "sharing"
		e.mu.Lock()
		_, err := e.retainedExitLANDestinationsLocked()
		e.mu.Unlock()
		if err == nil {
			t.Fatal("saturated sticky reservations permitted partial LAN scope")
		}
	}
}

func TestExitLANDestinationWorkBudgetSpansRepeatedPrefixes(t *testing.T) {
	now := time.Now()
	policy := &exitLANReservation{expires: now.Add(time.Minute), prefixes: []netip.Prefix{netip.MustParsePrefix("fd00::/64")}}
	work := exitLANWorkLimit - 1
	if got, err := policy.destinationsWithinBudget(netip.MustParsePrefix("fd00::/64"), now, &work); err != nil || len(got) != 0 {
		t.Fatal("fully subtracted prefix failed before budget exhausted", err)
	}
	if _, err := policy.destinationsWithinBudget(netip.MustParsePrefix("fd00::/64"), now, &work); err == nil {
		t.Fatal("empty result reset shared work budget")
	}
}
