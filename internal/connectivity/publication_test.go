package connectivity

import (
	"testing"
	"time"

	contract "github.com/endless-net/coordinator/api/enrollment"
	api "github.com/endless-net/management/api/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPublicationRequiresLiveLeaseAndSignedTargetPort(t *testing.T) {
	f, _, clock, lease, cancel := leaseFixture(t, time.Minute)
	defer cancel()
	f.config.ResolverDNS = true
	f.config.IndependentGrantExpiry = true
	peer := f.value.Peers[0]
	cutoff := f.now.Add(8 * time.Second)
	g := peer.ACLGrants[0]
	g.ValidUntil = &cutoff
	long := peer.ACLGrants[0]
	long.AllowedPorts = []contract.ACLPort{{Protocol: "tcp", Port: 8443}}
	f.value.Peers[0].ACLGrants = []contract.ACLGrant{g, long}
	f.value.Network.Services = []contract.AdvertisedService{{ID: "docs", Name: "Docs", DNSName: "docs.internal", Hosts: []contract.ServiceHost{{NodeID: peer.ID, PublicKey: peer.PublicKey}}, Ports: []contract.ServicePort{{Protocol: "tcp", Port: 443}, {Protocol: "tcp", Port: 8443}}}}
	second := peer
	second.ID = "other"
	second.PublicKey = newMapFixture(t).value.Node.PublicKey
	second.AllowedIPs = []string{"100.70.0.77/32"}
	second.ACLGrants = []contract.ACLGrant{{DestinationCIDRs: second.AllowedIPs, AllowedPorts: []contract.ACLPort{{Protocol: "tcp", Port: 443}}}}
	f.value.Peers = append(f.value.Peers, second)
	f.value.Network.Services[0].Hosts = append(f.value.Network.Services[0].Hosts, contract.ServiceHost{NodeID: second.ID, PublicKey: second.PublicKey})
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Confirm(m); err != nil {
		t.Fatal(err)
	}
	exe := &api.PublicationExecutor{AccountId: f.value.Network.AccountID, NetworkId: f.value.Network.ID, ProxyId: "proxy", NodeId: f.value.Node.ID}
	now := f.now
	routes, err := NewPublicationRoutes(exe, lease, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	const host = "docs.example.org"
	if _, err := routes.Resolve(host, nil); err != ErrPublication {
		t.Fatal("empty start", err)
	}
	who := &api.PublicationPeer{NodeId: peer.ID, PublicKey: peer.PublicKey}
	r := &api.PublicationRoute{ServiceId: "docs", Version: 1, Hostname: host, TargetPort: 443, TargetHttps: true, Targets: []*api.PublicationPeer{who}, AllowedSources: []*api.PublicationPeer{who}, Access: api.PublicationAccess_PUBLICATION_ACCESS_NETWORK_GROUPS, DomainValidUntil: timestamppb.New(now.Add(20 * time.Second))}
	packet := &api.GetPublicationRoutesResponse{Executor: exe, Generation: 1, LeaseId: "first", IssuedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(30 * time.Second)), Routes: []*api.PublicationRoute{r}}
	install := func() {
		t.Helper()
		if err := routes.Install(packet); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func(src *api.PublicationPeer, want bool) PublicationTarget {
		t.Helper()
		b, e := routes.Resolve(host, src)
		if (e == nil) != want {
			t.Fatal(b, e, want)
		}
		return b
	}
	install()
	resolve(nil, false)
	spoof := proto.CloneOf(who)
	spoof.PublicKey = f.value.Node.PublicKey
	resolve(spoof, false)
	target := resolve(who, true)
	if target.Address.String() != "100.70.0.2:443" || target.TLSName != "docs.internal" || target.Until != cutoff || !target.HTTPS {
		t.Fatal(target)
	}
	r.Targets = append(r.Targets, &api.PublicationPeer{NodeId: second.ID, PublicKey: second.PublicKey})
	packet.Generation++
	install()
	if routes.AdvanceProbe(host, target) {
		t.Fatal("old generation changed target preference")
	}
	if !routes.AdvanceProbe(host, resolve(who, true)) || resolve(who, true).Address.String() != "100.70.0.77:443" {
		t.Fatal("second approved target unavailable")
	}
	resolve(nil, false)
	packet.Generation++
	install()
	if resolve(who, true).Peer.NodeID != second.ID {
		t.Fatal("lease renewal lost available target")
	}
	r.Targets = r.Targets[:1]
	packet.Generation++
	install()
	target = resolve(who, true)
	r.TargetPort = 22
	if err := routes.Install(packet); err != ErrPublication {
		t.Fatal("same generation replaced", err)
	}
	resolve(who, true)
	packet.Generation++
	install()
	if _, err := routes.Resolve(host, nil); err != ErrPublication {
		t.Fatal("unauthorized source learned target availability", err)
	}
	if _, err := routes.Resolve(host, who); err != ErrPublicationTarget {
		t.Fatal("missing approved target port", err)
	}
	if target.Generation.Err() == nil {
		t.Fatal("old target survived replacement")
	}
	r.TargetPort = 443
	packet.Generation++
	install()
	resolve(who, true)
	now = cutoff
	clock.advance(8*time.Second, 8*time.Second)
	resolve(who, false)
	r.TargetPort = 8443
	r.Access = api.PublicationAccess_PUBLICATION_ACCESS_PUBLIC
	r.AllowedSources = nil
	packet.Generation++
	install()
	resolve(nil, true)
	stale := proto.CloneOf(packet)
	r.Targets = []*api.PublicationPeer{spoof}
	packet.Generation++
	install()
	resolve(nil, false)
	r.Targets = []*api.PublicationPeer{who}
	packet.Generation++
	install()
	now = now.Add(12 * time.Second)
	clock.advance(12*time.Second, 12*time.Second)
	if _, err := routes.Resolve(host, nil); err != ErrPublication {
		t.Fatal("expired domain remained identifiable", err)
	}
	packet.Generation++
	packet.Routes = nil
	install()
	if err := routes.Install(stale); err != ErrPublication {
		t.Fatal("stale restored", err)
	}
	resolve(nil, false)
}
