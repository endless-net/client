//go:build !linux && !windows && !darwin

package client

import (
	"fmt"
	"net/netip"
	"runtime"
)

func newWireGuardEngineRouter(_ string, _ CommandRunner, _ commandInputRunner) (wireGuardEngineRouter, error) {
	return nil, fmt.Errorf("wireguard-go routing is not supported on %s", runtime.GOOS)
}

func platformWireGuardEngineFirewallMark(_ []netip.Prefix, _ string) uint32 { return 0 }

func platformPrepareDNSProxy(_ *wireGuardEngineRouterConfig) {}
