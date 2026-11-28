package dataplane

import (
	"slices"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	c "github.com/endless-net/coordinator/api/enrollment"
)

func TestWindowsServiceNameUsesOnlyExactNamespace(t *testing.T) {
	f := newCandidateFixture(t)
	f.resolverDNS = true
	p := f.snapshot.Peers[0]
	svc := c.AdvertisedService{ID: "wiki", Name: "Wiki", DNSName: "wiki.internal", Hosts: []c.ServiceHost{{NodeID: p.ID, PublicKey: p.PublicKey}}}
	port := p.ACLGrants[0].AllowedPorts[0]
	svc.Ports = []c.ServicePort{{Protocol: port.Protocol, Port: uint32(port.Port)}}
	f.snapshot.Network.Services = []c.AdvertisedService{svc}
	f.snapshot.Network.DNSConfig = &c.DNSConfig{Zones: []c.DNSZone{{Name: "office.internal.", Version: 1}}}
	f.verify(t, nil)
	m, err := f.verified.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := connectivity.CompileMapDNS(m, func(time.Time) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	ns, search, err := windowsResolverNamespaces(routes, nil)
	if err != nil || !slices.Equal(ns, []string{".office.internal", "office.internal", "wiki.internal"}) || len(search) != 0 {
		t.Fatal(ns, search, err)
	}
}
