package client

import (
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestACLFirewallRestrictedSubnetPrecedesParentAllowance(t *testing.T) {
	for _, family := range []struct{ tool, parent, child string }{
		{"iptables", "10.0.0.0/8", "10.1.0.0/16"},
		{"ip6tables", "fd00::/8", "fd01::/16"},
	} {
		for _, reverse := range []bool{false, true} {
			peers := []clientapi.Peer{
				{AllowedIPs: []string{family.parent}, ACLRestricted: true, AllowedPorts: []clientapi.ACLPort{{Protocol: "tcp", Port: 443}}},
				{AllowedIPs: []string{family.child}, ACLRestricted: true},
			}
			if reverse {
				peers[0], peers[1] = peers[1], peers[0]
			}
			hooks := strings.Join(renderACLFirewallHooks(peers), "\n")
			deny := strings.Index(hooks, family.tool+" -A ENACL-%i -o %i -d "+family.child+" -j REJECT")
			allow := strings.Index(hooks, family.tool+" -A ENACL-%i -o %i -d "+family.parent+" -p tcp --dport 443 -j RETURN")
			if deny < 0 || allow < 0 || deny >= allow {
				t.Fatalf("specific restriction bypassed (reverse=%v): %s", reverse, hooks)
			}
		}
	}
}
