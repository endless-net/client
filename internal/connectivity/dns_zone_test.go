package connectivity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/net/dns/dnsmessage"
)

func TestZoneAddressSets(t *testing.T) {
	records := []coordinator.DNSRecord{
		{Name: "db.dev.example.", Type: "AAAA", Value: "2001:db8::1", TTL: 0},
		{Name: "db.dev.example.", Type: "A", Value: "192.0.2.1", TTL: 30},
		{Name: "other.dev.example.", Type: "AAAA", Value: "2001:db8::1", TTL: 20},
		{Name: "DB.dev.example.", Type: "AAAA", Value: "2001:db8::2", TTL: 0},
	}
	cfg := &coordinator.DNSConfig{Zones: []coordinator.DNSZone{{Name: "dev.example.", Version: 1, Records: records}}}
	if _, err := CompileDNSRoutes(cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		value string
		ttl   uint32
	}{
		{"different TTL", "2001:db8::3", 1},
		{"same IPv6 address", "2001:0db8:0:0::1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Zones[0].Records = append(slices.Clone(records), coordinator.DNSRecord{
				Name: "DB.DEV.EXAMPLE.", Type: "AAAA", Value: tc.value, TTL: tc.ttl,
			})
			if _, err := CompileDNSRoutes(cfg); !errors.Is(err, ErrDNSAssignment) {
				t.Fatalf("CompileDNSRoutes() error = %v, want ErrDNSAssignment", err)
			}
		})
	}
}

func TestAssignedZoneReturnsLocalDNSAnswers(t *testing.T) {
	cfg := &coordinator.DNSConfig{Zones: []coordinator.DNSZone{{Name: "dev.example.", Version: 7, Records: []coordinator.DNSRecord{
		{Name: "db.team.dev.example.", Type: "AAAA", Value: "2001:db8::4", TTL: 27},
		{Name: "alias.dev.example.", Type: "CNAME", Value: "db.team.dev.example.", TTL: 13},
	}}}}
	cfg.OverrideLocalDNS = true
	cfg.Nameservers = []coordinator.DNSNameserver{
		{ID: "public", Scope: "primary", Address: "192.0.2.1"},
		{ID: "private", Scope: "split", Address: "192.0.2.2", SplitDomains: []string{"dev.example"}},
	}
	routes, err := CompileDNSRoutes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := routes.ResolverDomains([]string{"dev.example"})
	if err != nil || !slices.Equal(domains, []string{"dev.example", "~."}) {
		t.Fatal(domains, err)
	}
	cases := []struct {
		name   string
		kind   dnsmessage.Type
		code   dnsmessage.RCode
		answer dnsmessage.Type
		ttl    uint32
	}{
		{"DB.team.dev.example.", dnsmessage.TypeAAAA, 0, dnsmessage.TypeAAAA, 27},
		{"db.team.dev.example.", dnsmessage.TypeA, 0, 0, 0},
		{"alias.dev.example.", dnsmessage.TypeA, 0, dnsmessage.TypeCNAME, 13},
		{"missing.dev.example.", dnsmessage.TypeA, dnsmessage.RCodeNameError, 0, 0},
		{"dev.example.", dnsmessage.TypeAAAA, 0, 0, 0},
		{"team.dev.example.", dnsmessage.TypeAAAA, 0, 0, 0},
	}
	for _, tc := range cases {
		q := dnsmessage.Question{Name: dnsmessage.MustNewName(tc.name), Type: tc.kind, Class: dnsmessage.ClassINET}
		query := dnsmessage.Message{Header: dnsmessage.Header{ID: 341, RecursionDesired: true}, Questions: []dnsmessage.Question{q}}
		packet, err := query.Pack()
		if err != nil {
			t.Fatal(err)
		}
		dial := &dnsTestDialer{}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		raw, err := routes.Exchange(ctx, packet, dial)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if len(dial.calls) != 0 {
			t.Fatalf("%s escaped the assigned zone: %v", tc.name, dial.calls)
		}
		var response dnsmessage.Message
		if err = response.Unpack(raw); err != nil {
			t.Fatal(err)
		}
		if !response.Authoritative || !response.Response || response.RCode != tc.code || response.ID != 341 || len(response.Questions) != 1 || response.Questions[0] != q {
			t.Fatal(tc, response)
		}
		if tc.answer == 0 {
			if len(response.Answers) != 0 {
				t.Fatal(tc, response.Answers)
			}
			continue
		}
		if len(response.Answers) != 1 || response.Answers[0].Header.Type != tc.answer || response.Answers[0].Header.TTL != tc.ttl {
			t.Fatal(tc, response.Answers)
		}
	}
}

func TestLocalCNAMEFollowsAssignedZones(t *testing.T) {
	cfg := &coordinator.DNSConfig{
		Zones: []coordinator.DNSZone{
			{Name: "dev.test.", Version: 1, Records: []coordinator.DNSRecord{
				{Name: "app.dev.test.", Type: "CNAME", Value: "alias.child.dev.test.", TTL: 10},
				{Name: "alias.child.dev.test.", Type: "A", Value: "192.0.2.9", TTL: 90},
				{Name: "loop.dev.test.", Type: "CNAME", Value: "loop.dev.test.", TTL: 5},
				{Name: "outside.dev.test.", Type: "CNAME", Value: "www.public.test.", TTL: 5},
				{Name: "absent.dev.test.", Type: "CNAME", Value: "missing.dev.test.", TTL: 5},
			}},
			{Name: "child.dev.test.", Version: 2, Records: []coordinator.DNSRecord{
				{Name: "alias.child.dev.test.", Type: "CNAME", Value: "real.child.dev.test.", TTL: 20},
				{Name: "real.child.dev.test.", Type: "A", Value: "192.0.2.8", TTL: 30},
			}},
		},
	}
	routes, err := CompileDNSRoutes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		qtype dnsmessage.Type
		code  dnsmessage.RCode
		ttls  []uint32
	}{
		{"app.dev.test.", dnsmessage.TypeA, 0, []uint32{10, 20, 30}},
		{"app.dev.test.", dnsmessage.TypeCNAME, 0, []uint32{10}},
		{"app.dev.test.", dnsmessage.TypeAAAA, 0, []uint32{10, 20}},
		{"outside.dev.test.", dnsmessage.TypeA, 0, []uint32{5}},
		{"absent.dev.test.", dnsmessage.TypeA, dnsmessage.RCodeNameError, []uint32{5}},
		{"loop.dev.test.", dnsmessage.TypeA, dnsmessage.RCodeServerFailure, nil},
	} {
		query := dnsmessage.Message{Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(tc.name), Type: tc.qtype, Class: dnsmessage.ClassINET}}}
		raw, err := query.Pack()
		if err != nil {
			t.Fatal(err)
		}
		raw, err = routes.Exchange(t.Context(), raw, nil)
		var reply dnsmessage.Message
		if err != nil || reply.Unpack(raw) != nil || reply.RCode != tc.code || len(reply.Answers) != len(tc.ttls) {
			t.Fatalf("%s %v: %v %v", tc.name, tc.qtype, reply, err)
		}
		for i, ttl := range tc.ttls {
			if reply.Answers[i].Header.TTL != ttl {
				t.Fatal("wrong TTL", reply)
			}
		}
		if len(tc.ttls) == 3 && reply.Answers[2].Body.(*dnsmessage.AResource).A != [4]byte{192, 0, 2, 8} {
			t.Fatal("parent shadowed child", reply)
		}
	}
}
