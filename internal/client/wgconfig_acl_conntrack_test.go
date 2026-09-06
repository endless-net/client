package client

import (
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestACLConntrackBypassIsLimitedToRepliesOnTunnel(t *testing.T) {
	hooks := renderACLFirewallHooks([]clientapi.Peer{{
		AllowedIPs:    []string{"100.90.0.2/32", "fd00::2/128"},
		ACLRestricted: true,
	}})
	for _, tool := range []string{"iptables", "ip6tables"} {
		found := false
		for _, hook := range hooks {
			if !strings.HasPrefix(hook, "PostUp = "+tool+" ") || !strings.Contains(hook, "--ctstate") {
				continue
			}
			found = true
			if !strings.Contains(hook, "-o %i ") || !strings.Contains(hook, "--ctdir REPLY -j RETURN") {
				t.Fatal("conntrack bypass permits original-direction or non-tunnel traffic")
			}
		}
		if !found {
			t.Fatalf("%s must preserve reply traffic", tool)
		}
	}
}
