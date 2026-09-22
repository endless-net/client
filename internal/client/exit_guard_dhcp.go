package client

import (
	"fmt"
	"strings"
)

// DHCP is privileged host infrastructure, independent of LAN application
// authorization. Limit it to client/server UDP port pairs outside the TUN;
// DHCPv6 is additionally link-local. No application discovery ports are opened.
func exitGuardDHCPBatch(table, device string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add rule inet %s output oifname != %q meta nfproto ipv4 meta l4proto udp udp sport 68 udp dport 67 accept\n", table, device)
	for _, destination := range []string{"ff02::1:2", "fe80::/10"} {
		fmt.Fprintf(&b, "add rule inet %s output oifname != %q meta nfproto ipv6 meta l4proto udp ip6 saddr fe80::/10 ip6 daddr %s udp sport 546 udp dport 547 accept\n", table, device, destination)
	}
	return b.String()
}
