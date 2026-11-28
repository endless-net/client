package connectivity

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestSignedDomainDiscoveryBindsOnlyLocalRouter(t *testing.T) {
	f := newMapFixture(t)
	config := f.config
	config.UnavailableResourceGuards = true
	v, err := NewVerifier(f.profile, config)
	if err != nil {
		t.Fatal(err)
	}
	local := coordinator.ServiceHost{NodeID: f.value.Node.ID, PublicKey: f.value.Node.PublicKey}
	app := coordinator.Application{ID: "office", TargetType: "domain", Target: "printer.corp.example", PolicyHash: strings.Repeat("ab", 32), Connectors: []coordinator.ServiceHost{local}}
	for _, tc := range []string{"assigned", "wrong-key", "unassigned", "invalid-name", "invalid-hash", "traffic-source"} {
		a := app
		switch tc {
		case "wrong-key":
			a.Connectors = []coordinator.ServiceHost{{NodeID: local.NodeID, PublicKey: f.value.Peers[0].PublicKey}}
		case "unassigned":
			a.Connectors = nil
		case "invalid-name":
			a.Target = "a..example"
		case "invalid-hash":
			a.PolicyHash = strings.Repeat("x", 64)
		case "traffic-source":
			a.Sources = []coordinator.ServiceHost{local}
		}
		f.value.Network.Applications = []coordinator.Application{a}
		raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
		got, err := v.Verify(t.Context(), raw, nil)
		if tc != "assigned" {
			if !errors.Is(err, ErrMap) {
				t.Fatalf("%s: %v", tc, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		m, err := got.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		prefixes, err := UnavailableResourcePrefixes(m, 1)
		if err != nil || len(prefixes) != 0 {
			t.Fatal(prefixes, err)
		}
	}
}

func TestVerifiedUnavailableResourceRequiresNativeGuard(t *testing.T) {
	f := newMapFixture(t)
	f.value.Network.Applications = []coordinator.Application{{ID: "printer", Name: "Printer", TargetType: "cidr", Target: "192.168.23.0/24"}}
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	for _, enabled := range []bool{false, true} {
		config := f.config
		config.UnavailableResourceGuards = enabled
		v, err := NewVerifier(f.profile, config)
		if err != nil {
			t.Fatal(err)
		}
		m, err := v.Verify(t.Context(), raw, nil)
		if !enabled {
			if !errors.Is(err, ErrMap) {
				t.Fatal("map needs native resource guard", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := m.Snapshot()
		if err != nil || len(got.Network.Applications) != 1 || got.Network.Applications[0].Target != "192.168.23.0/24" {
			t.Fatal("lost protected prefix", err)
		}
	}
}

func TestUnavailableResourcePrefixes(t *testing.T) {
	a := coordinator.Application{ID: "printer", TargetType: "ip", Target: "10.20.0.17"}
	n := coordinator.Network{CIDR: "100.96.0.0/24", Applications: []coordinator.Application{a}}
	prefixes, err := UnavailableResourcePrefixes(coordinator.NetworkMapSnapshot{Network: n}, 1)
	if err != nil || len(prefixes) != 1 || prefixes[0] != netip.MustParsePrefix("10.20.0.17/32") {
		t.Fatal(prefixes, err)
	}
	for _, change := range []func(*coordinator.Application){
		func(a *coordinator.Application) { a.Connectors = []coordinator.ServiceHost{{NodeID: "router"}} },
		func(a *coordinator.Application) { a.Routes = []coordinator.ApplicationRoute{{}} },
		func(a *coordinator.Application) { a.Target = "100.96.0.1" },
		func(a *coordinator.Application) { a.TargetType, a.Target = "cidr", "0.0.0.0/0" },
		func(a *coordinator.Application) { a.TargetType, a.Target = "cidr", "10.20.0.17/24" },
	} {
		n.Applications[0] = a
		change(&n.Applications[0])
		if _, err := UnavailableResourcePrefixes(coordinator.NetworkMapSnapshot{Network: n}, 1); err == nil {
			t.Fatal("unexpected resource admitted", n.Applications[0])
		}
	}
}

func TestDomainRecipientVerifiedMapAnswersUntilLeaseEnds(t *testing.T) {
	f := newMapFixture(t)
	end := f.now.Add(time.Minute)
	p := &f.value.Peers[0]
	p.EndpointExpiresAt = &end
	host := coordinator.ServiceHost{NodeID: p.ID, PublicKey: p.PublicKey}
	app := coordinator.Application{ID: "domain", TargetType: "domain", Target: "printer.corp.example", DNSEnabled: true, PolicyHash: strings.Repeat("ab", 32), AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Connectors: []coordinator.ServiceHost{host}, Routes: []coordinator.ApplicationRoute{{Connector: host, CIDRs: []string{"10.72.0.1/32", "10.72.0.2/32"}, Metric: 1, ExpiresAt: end}}}
	c := f.config
	c.ResolverDNS, c.UnavailableResourceGuards, c.IPv4ResourceRoutes = true, true, true
	v, err := NewVerifier(f.profile, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, online := range []bool{true, false} {
		if !online {
			app.Routes = nil
			app.Connectors = nil
		}
		f.value.Network.Applications = []coordinator.Application{app}
		m, err := v.Verify(t.Context(), f.sign(t, nil, f.now, end), nil)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := m.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		at := f.now
		r, err := CompileMapDNS(snapshot, func(exp time.Time) bool { return at.Before(exp) })
		if err != nil || !r.hasService(app.Target) {
			t.Fatal("resolver lost authoritative name", err)
		}
		records, ok := r.serviceRecords(app.Target)
		if online && (len(records) != 2 || !ok) || !online && (len(records) != 0 || ok) {
			t.Fatal(online, records)
		}
		at = end
		if _, ok := r.serviceRecords(app.Target); ok {
			t.Fatal("expired records")
		}
	}
}

func TestUnavailableRouterAssignmentBindsSignedLocalIdentity(t *testing.T) {
	f := newMapFixture(t)
	config := f.config
	config.UnavailableResourceGuards = true
	v, err := NewVerifier(f.profile, config)
	if err != nil {
		t.Fatal(err)
	}
	local := coordinator.ServiceHost{NodeID: f.value.Node.ID, PublicKey: f.value.Node.PublicKey}
	a := coordinator.Application{ID: "lan", TargetType: "cidr", Target: "10.72.0.0/24", Connectors: []coordinator.ServiceHost{local}}
	b := coordinator.Application{ID: "remote", TargetType: "ip", Target: "192.0.2.123"}
	for _, tc := range []string{"local", "rotated key", "other node", "extra connector", "source", "route"} {
		f.value.Network.Applications = []coordinator.Application{a, b}
		own := &f.value.Network.Applications[0]
		switch tc {
		case "rotated key":
			own.Connectors = []coordinator.ServiceHost{{NodeID: local.NodeID, PublicKey: f.value.Peers[0].PublicKey}}
		case "other node":
			own.Connectors = []coordinator.ServiceHost{{NodeID: "another", PublicKey: local.PublicKey}}
		case "extra connector":
			own.Connectors = []coordinator.ServiceHost{local, local}
		case "source":
			own.Sources = []coordinator.ServiceHost{local}
		case "route":
			own.Routes = []coordinator.ApplicationRoute{{Connector: local}}
		}
		raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
		m, err := v.Verify(t.Context(), raw, nil)
		if tc != "local" {
			if !errors.Is(err, ErrMap) {
				t.Fatalf("%s accepted: %v", tc, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := m.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		prefixes, err := UnavailableResourcePrefixes(snapshot, 2)
		if err != nil || len(prefixes) != 1 || prefixes[0].String() != "192.0.2.123/32" {
			t.Fatal("incorrect recipient/router guards", prefixes, err)
		}
	}
}

func TestVerifierAdmitsActiveIPv4RoutesOnlyWithScopedNativeSupport(t *testing.T) {
	f := newMapFixture(t)
	end := f.now.Add(time.Minute)
	local := coordinator.ServiceHost{NodeID: f.value.Node.ID, PublicKey: f.value.Node.PublicKey}
	peer := coordinator.ServiceHost{NodeID: f.value.Peers[0].ID, PublicKey: f.value.Peers[0].PublicKey}
	f.value.Node.EndpointExpiresAt = &end
	f.value.Peers[0].EndpointExpiresAt = &end
	for _, tc := range []string{"recipient", "router", "exit-router", "exit-recipient", "exit-native", "exit-down", "exit-forwarding-disabled", "recipient-only-router", "unsupported", "short-path", "wrong-source-key", "foreign-source"} {
		target := "10.37.0.0/24"
		if tc == "exit-router" || tc == "exit-recipient" || tc == "exit-native" || tc == "exit-down" || tc == "exit-forwarding-disabled" {
			target = "0.0.0.0/0"
		}
		router := peer
		var sources []coordinator.ServiceHost
		exp := end
		switch tc {
		case "router", "exit-router", "exit-forwarding-disabled", "wrong-source-key", "recipient-only-router":
			router = local
			sources = []coordinator.ServiceHost{peer}
			if tc == "wrong-source-key" {
				sources[0].PublicKey = local.PublicKey
			}
		case "short-path":
			exp = end.Add(-time.Second)
		case "foreign-source":
			sources = []coordinator.ServiceHost{peer}
		}
		f.value.Network.Applications = []coordinator.Application{{ID: "r", Target: target, TargetType: "cidr", Connectors: []coordinator.ServiceHost{router}, Sources: sources, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Routes: []coordinator.ApplicationRoute{{Connector: router, Metric: 1, CIDRs: []string{target}, ExpiresAt: exp}}}}
		c := f.config
		if tc == "exit-down" {
			f.value.Network.Applications[0].Routes = nil
			f.value.Network.Applications[0].Connectors = nil
		}
		c.IPv4ExitRouting = tc == "exit-native" || tc == "exit-down"
		c.UnavailableResourceGuards = true
		c.IPv4ResourceRoutes = tc != "unsupported"
		c.IPv4ResourceForwarding = tc != "recipient" && tc != "recipient-only-router" && tc != "exit-forwarding-disabled"
		v, err := NewVerifier(f.profile, c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = v.Verify(t.Context(), f.sign(t, nil, f.now, end), nil)
		if tc == "recipient" || tc == "router" || tc == "exit-router" || c.IPv4ExitRouting {
			if err != nil {
				t.Fatal(tc, err)
			}
		} else if !errors.Is(err, ErrMap) {
			t.Fatal(tc, err)
		}
	}
}
