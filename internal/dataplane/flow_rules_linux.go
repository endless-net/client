package dataplane

import (
	"fmt"
	"strings"
	"time"
)

// linuxFlowRules arms or removes metadata counters on a session-owned table.
// The interface element expires in the kernel even if the process stalls.
// The caller must bound the lifetime by the remaining map and consent grants.
func linuxFlowRules(id linuxFilterIdentity, enable bool, capacity int, lifetime time.Duration) (string, error) {
	if !linuxObjectName.MatchString(id.table) || !linuxObjectName.MatchString(id.tunnel) ||
		enable && (capacity < 1 || lifetime < time.Millisecond) {
		return "", ErrConfiguration
	}
	table := "inet " + id.table
	var rules strings.Builder
	if enable {
		fmt.Fprintf(&rules, "create set %s flow_interfaces { type ifname; flags timeout; }\n", table)
		fmt.Fprintf(&rules, "add element %s flow_interfaces { %q timeout %dms }\n", table, id.tunnel, lifetime.Milliseconds())
	}
	for _, chain := range []string{"input", "output"} {
		iface := "iifname"
		if chain == "output" {
			iface = "oifname"
		}
		for _, verdict := range []string{"accept", "drop"} {
			name := chain + "_" + verdict
			fmt.Fprintf(&rules, "flush chain %s %s\n", table, name)
			if enable {
				fmt.Fprintf(&rules, "create set %s seen_%s { type ipv4_addr . ipv4_addr . inet_proto . inet_service; flags dynamic; size %d; }\n", table, name, capacity)
				fmt.Fprintf(&rules, "add rule %s %s %s @flow_interfaces meta nfproto ipv4 counter\n", table, name, iface)
				fmt.Fprintf(&rules, "add rule %s %s %s @flow_interfaces meta nfproto ipv4 meta l4proto { tcp, udp } update @seen_%s { ip saddr . ip daddr . meta l4proto . th dport counter }\n", table, name, iface, name)
			} else {
				fmt.Fprintf(&rules, "destroy set %s seen_%s\n", table, name)
			}
			fmt.Fprintf(&rules, "add rule %s %s %s\n", table, name, verdict)
		}
	}
	if !enable {
		fmt.Fprintf(&rules, "destroy set %s flow_interfaces\n", table)
	}
	return rules.String(), nil
}
