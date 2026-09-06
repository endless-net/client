package client

import (
	"net/netip"
	"strconv"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestACLGrantSnapshotIsDeepCopied(t *testing.T) {
	original := clientapi.RegisterNodeResponse{Peers: []clientapi.Peer{{ACLGrants: []clientapi.ACLGrant{{DestinationCIDRs: []string{"10.0.0.0/8"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 443}}}}}}}
	copy := cloneRegisterNodeResponse(original)
	copy.Peers[0].ACLGrants[0].DestinationCIDRs[0] = "192.0.2.0/24"
	copy.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 22
	if original.Peers[0].ACLGrants[0].DestinationCIDRs[0] != "10.0.0.0/8" || original.Peers[0].ACLGrants[0].AllowedPorts[0].Port != 443 {
		t.Fatal("ACL snapshot aliases input")
	}
}

func TestCorrelatedACLHooksDoNotCreateCrossGrantPermissions(t *testing.T) {
	peers := []clientapi.Peer{{AllowedIPs: []string{"10.0.0.0/8", "2001:db8::/32"}, ACLRestricted: true, ACLGrants: []clientapi.ACLGrant{
		{DestinationCIDRs: []string{"10.1.0.0/16"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 443}}},
		{DestinationCIDRs: []string{"10.2.0.0/16"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 22}}},
		{DestinationCIDRs: []string{"2001:db8::/48"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "icmp"}}},
	}}}
	hooks := renderACLFirewallHooks(peers)
	for _, test := range []struct {
		ip, protocol string
		port         int
		allow        bool
	}{
		{"10.1.0.1", "tcp", 443, true}, {"10.1.0.1", "tcp", 22, false},
		{"10.2.0.1", "tcp", 22, true}, {"10.2.0.1", "tcp", 443, false},
		{"10.1.0.1", "udp", 443, false}, {"10.3.0.1", "tcp", 443, false},
		{"2001:db8::1", "ipv6-icmp", 0, true}, {"2001:db8:1::1", "ipv6-icmp", 0, false},
	} {
		if got := newPacketAllowed(hooks, test.ip, test.protocol, test.port); got != test.allow {
			t.Fatalf("%+v: got %v", test, got)
		}
	}
}

func TestACLGrantUnionAndUnrestrictedMoreSpecificRoute(t *testing.T) {
	peers := []clientapi.Peer{{AllowedIPs: []string{"10.0.0.0/8"}, ACLRestricted: true, ACLGrants: []clientapi.ACLGrant{
		{DestinationCIDRs: []string{"10.0.0.0/8"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 443}}},
		{DestinationCIDRs: []string{"10.1.0.0/16"}, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 22}}},
	}}, {AllowedIPs: []string{"10.2.0.0/16"}}}
	hooks := renderACLFirewallHooks(peers)
	for _, test := range []struct {
		ip    string
		port  int
		allow bool
	}{
		{"10.1.0.1", 443, true}, {"10.1.0.1", 22, true}, {"10.3.0.1", 22, false}, {"10.2.0.1", 1234, true},
	} {
		if got := newPacketAllowed(hooks, test.ip, "tcp", test.port); got != test.allow {
			t.Fatalf("%+v: got %v", test, got)
		}
	}
}

// Model first-match rules for NEW outgoing packets without executing firewall
// commands. Runtime/conntrack acceptance belongs to privileged Linux CI.
func newPacketAllowed(hooks []string, ip, protocol string, port int) bool {
	address := netip.MustParseAddr(ip)
	for _, hook := range hooks {
		if !strings.HasPrefix(hook, "PostUp = ") || !strings.Contains(hook, " -o %i -d ") {
			continue
		}
		fields := strings.Fields(hook)
		options := map[string]string{}
		for i := 0; i+1 < len(fields); i++ {
			if strings.HasPrefix(fields[i], "-") {
				options[fields[i]] = fields[i+1]
			}
		}
		prefix, err := netip.ParsePrefix(options["-d"])
		if err != nil || !prefix.Contains(address) {
			continue
		}
		if options["-p"] != "" && options["-p"] != protocol {
			continue
		}
		if options["--dport"] != "" && options["--dport"] != strconv.Itoa(port) {
			continue
		}
		return options["-j"] == "RETURN"
	}
	return true
}
