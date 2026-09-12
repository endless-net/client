package client

import (
	"net/netip"
	"slices"
)

// splitDefaultRoutes preserves the host's /0 and avoids competing default-route
// metrics. Darwin and Windows install two more specific /1 routes instead.
// Reachability of remote tunnel/control endpoints still requires separate
// underlay routing: preserving /0 does not bypass these more specific routes.
func splitDefaultRoutes(routes []netip.Prefix) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(routes)+2)
	for _, route := range routes {
		if route.Bits() != 0 {
			result = append(result, route)
			continue
		}
		if route.Addr().Is4() {
			result = append(result, netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1"))
		} else {
			result = append(result, netip.MustParsePrefix("::/1"), netip.MustParsePrefix("8000::/1"))
		}
	}
	unique := result[:0]
	for _, route := range result {
		if !slices.Contains(unique, route) {
			unique = append(unique, route)
		}
	}
	return unique
}
