//go:build linux

package client

import "net/netip"

func newPlatformExitGuard(name, routeTable string) (*linuxExitGuard, error) {
	mark := platformWireGuardEngineFirewallMark([]netip.Prefix{netip.PrefixFrom(netip.IPv4Unspecified(), 0)}, routeTable)
	return newLinuxExitGuard(name, mark, nil)
}
