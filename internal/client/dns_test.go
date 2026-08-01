package client

import (
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestResolvePeerDNSNameFQDNAndShortName(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "prod"},
		Node:    clientapi.Node{ID: "node-a", Hostname: "node-a", AssignedIP: "100.64.0.2"},
		Peers: []clientapi.Peer{{
			ID:         "node-b",
			Hostname:   "node-b",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	fqdn, err := ResolvePeerDNSName(networkMap, "node-b.prod.endlessnet.", "", DNSAddressIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if fqdn.Address != "100.64.0.3" || fqdn.FQDN != "node-b.prod.endlessnet" || fqdn.SearchDomain != "prod.endlessnet" {
		t.Fatalf("fqdn resolution = %#v", fqdn)
	}
	short, err := ResolvePeerDNSName(networkMap, "node-b", "", DNSAddressIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if short.Address != fqdn.Address || short.FQDN != fqdn.FQDN {
		t.Fatalf("short resolution = %#v, want same address/fqdn as %#v", short, fqdn)
	}
}

func TestResolvePeerDNSNameConflictUsesLowestNodeID(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "default"},
		Peers: []clientapi.Peer{
			{ID: "node-z", Hostname: "app", AllowedIPs: []string{"100.64.0.9/32"}},
			{ID: "node-a", Hostname: "app", AllowedIPs: []string{"100.64.0.3/32"}},
		},
	}
	resolution, err := ResolvePeerDNSName(networkMap, "app", "", DNSAddressIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Address != "100.64.0.3" || resolution.NodeID != "node-a" || !resolution.Conflict {
		t.Fatalf("conflict resolution = %#v", resolution)
	}
	if resolution.ConflictPolicy != "lowest-node-id-wins" {
		t.Fatalf("conflict policy = %q", resolution.ConflictPolicy)
	}
	if got := resolution.ConflictingNodeIDs; len(got) != 2 || got[0] != "node-a" || got[1] != "node-z" {
		t.Fatalf("conflicting ids = %#v", got)
	}
}

func TestResolvePeerDNSNameIPv6AndSubnetFiltering(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "v6"},
		Peers: []clientapi.Peer{{
			ID:         "node-b",
			Hostname:   "node-b",
			AllowedIPs: []string{"10.2.0.0/24", "fd7a:115c:a1e0::3/128"},
		}},
	}
	if _, err := ResolvePeerDNSName(networkMap, "node-b", "", DNSAddressIPv4); err == nil {
		t.Fatal("IPv4 resolution unexpectedly used an advertised subnet prefix")
	}
	resolution, err := ResolvePeerDNSName(networkMap, "node-b.v6.endlessnet", "", DNSAddressIPv6)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Address != "fd7a:115c:a1e0::3" || resolution.Family != "AAAA" {
		t.Fatalf("IPv6 resolution = %#v", resolution)
	}
}

func TestResolvePeerDNSNameRejectsOutsideDomain(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{Name: "default"},
		Peers: []clientapi.Peer{{
			ID:         "node-b",
			Hostname:   "node-b",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	if _, err := ResolvePeerDNSName(networkMap, "node-b.example.com", "", DNSAddressIPv4); err == nil {
		t.Fatal("outside-domain DNS name unexpectedly resolved")
	}
}
