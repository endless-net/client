package connectivity

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	c "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/net/dns/dnsmessage"
)

func TestServiceDNSUsesCurrentHostGrantsAndNeverForwardsExpiredName(t *testing.T) {
	f := newMapFixture(t)
	f.config.ResolverDNS = true
	f.config.IndependentGrantExpiry = true
	p := &f.value.Peers[0]
	early, late := f.now.Add(time.Second), f.now.Add(5*time.Second)
	grant := p.ACLGrants[0]
	grant.ValidUntil = &early
	second := grant
	second.ValidUntil = &late
	second.AllowedPorts = []c.ACLPort{{Protocol: "tcp", Port: 8443}}
	ignored := grant
	ignored.ValidUntil = nil
	ignored.AllowedPorts = []c.ACLPort{{Protocol: "tcp", Port: 22}}
	p.ACLGrants = []c.ACLGrant{grant, second, ignored}
	f.value.Network.Services = []c.AdvertisedService{{ID: "docs", Name: "Docs", DNSName: "Docs.Dev.Example.", Hosts: []c.ServiceHost{{NodeID: p.ID, PublicKey: p.PublicKey}}, Ports: []c.ServicePort{{Protocol: "tcp", Port: 443}, {Protocol: "tcp", Port: 8443}}}}
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	verified, err := v.Verify(t.Context(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := verified.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	now := f.now
	current := func(end time.Time) bool { return now.Before(end) }
	r, err := CompileMapDNS(m, current)
	if err != nil {
		t.Fatal(err)
	}
	domains, err := r.ResolverDomains(nil)
	if err != nil || !slices.Equal(domains, []string{"~docs.dev.example"}) || !r.ExactServiceDomain("docs.dev.example") {
		t.Fatal("service-only assignment", domains, err)
	}
	query := func(name string, kind dnsmessage.Type) dnsmessage.Message {
		t.Helper()
		msg := dnsmessage.Message{Header: dnsmessage.Header{ID: 7}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, Type: kind}}}
		packet, err := msg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		dial := &dnsTestDialer{}
		raw, err := r.Exchange(ctx, packet, dial)
		if err != nil || len(dial.calls) != 0 {
			t.Fatal("service lookup escaped local resolver", err, dial.calls)
		}
		var answer dnsmessage.Message
		if err = answer.Unpack(raw); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	now = early
	answer := query("DOCS.dev.example.", dnsmessage.TypeA)
	if len(answer.Answers) != 1 || answer.Answers[0].Header.TTL != 0 || !answer.Authoritative {
		t.Fatal("independent port lost or cacheable", answer)
	}
	ip := answer.Answers[0].Body.(*dnsmessage.AResource).A
	if netip.AddrFrom4(ip).String()+"/32" != p.AllowedIPs[0] {
		t.Fatal("wrong service host", ip)
	}
	if answer = query("docs.dev.example.", dnsmessage.TypeAAAA); len(answer.Answers) != 0 || answer.RCode != dnsmessage.RCodeSuccess {
		t.Fatal(answer)
	}
	m.Network.DNSConfig = &c.DNSConfig{Zones: []c.DNSZone{{Name: "dev.example.", Version: 1, Records: []c.DNSRecord{{Name: "alias.dev.example.", Type: "CNAME", Value: "docs.dev.example."}}}}}
	r, err = CompileMapDNS(m, current)
	if err != nil {
		t.Fatal(err)
	}
	domains, err = r.ResolverDomains(nil)
	if err != nil || !slices.Equal(domains, []string{"~dev.example"}) || r.ExactServiceDomain("docs.dev.example") {
		t.Fatal(domains, err)
	}
	if answer = query("alias.dev.example.", dnsmessage.TypeA); len(answer.Answers) != 2 {
		t.Fatal("service alias", answer)
	}
	now = late
	if answer = query("docs.dev.example.", dnsmessage.TypeA); len(answer.Answers) != 0 || answer.RCode != dnsmessage.RCodeNameError {
		t.Fatal("expired service resolved", answer)
	}
	m.Network.DNSConfig.Zones[0].Records = append(m.Network.DNSConfig.Zones[0].Records, c.DNSRecord{Name: "docs.dev.example.", Type: "A", Value: "192.0.2.1"})
	if _, err := CompileMapDNS(m, current); !errors.Is(err, ErrDNSAssignment) {
		t.Fatal("conflicting explicit record", err)
	}
	m.Network.DNSConfig.Zones[0].Records = nil
	m.Network.Services = nil
	r, err = CompileMapDNS(m, current)
	if err != nil {
		t.Fatal(err)
	}
	now = f.now
	if answer = query("docs.dev.example.", dnsmessage.TypeA); answer.RCode != dnsmessage.RCodeNameError {
		t.Fatal("withdrawn service resolved", answer)
	}
}

func TestServiceDNSExpiresEachApprovedHostIndependently(t *testing.T) {
	f := newMapFixture(t)
	f.config.ResolverDNS = true
	f.config.IndependentGrantExpiry = true
	p := f.value.Peers[0]
	early := f.now.Add(time.Second)
	p.ACLGrants[0].ValidUntil = &early
	second := p
	second.ID = "other"
	second.PublicKey = newMapFixture(t).value.Node.PublicKey
	second.AllowedIPs = []string{"100.70.0.77/32"}
	second.ACLGrants = []c.ACLGrant{{DestinationCIDRs: second.AllowedIPs, AllowedPorts: []c.ACLPort{{Protocol: c.ProtocolAll}}}}
	f.value.Peers = []c.Peer{p, second}
	f.value.Network.Services = []c.AdvertisedService{{ID: "build", Name: "Build", DNSName: "build.internal", Ports: []c.ServicePort{{Protocol: "tcp", Port: 443}}, Hosts: []c.ServiceHost{{NodeID: p.ID, PublicKey: p.PublicKey}, {NodeID: second.ID, PublicKey: second.PublicKey}}}}
	now := f.now
	verifier, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	m, err := verifier.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := CompileMapDNS(snapshot, func(end time.Time) bool { return now.Before(end) })
	if err != nil {
		t.Fatal(err)
	}
	if rr, ok := routes.serviceRecords("build.internal"); !ok || len(rr) != 2 {
		t.Fatal(rr)
	}
	now = early
	rr, ok := routes.serviceRecords("build.internal")
	if !ok || len(rr) != 1 || rr[0].Body.(*dnsmessage.AResource).A != [4]byte{100, 70, 0, 77} {
		t.Fatal("independent host expiry", rr)
	}
	now = f.value.MapSignature.ExpiresAt
	if _, ok := routes.serviceRecords("build.internal"); ok {
		t.Fatal("all-protocol grant outlived map")
	}
}
