package client

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

type wireGuardEngineRouter interface {
	Configure(context.Context, wireGuardEngineRouterConfig) error
	Down(context.Context) error
}

func wireGuardEngineRouterConfigsEqual(a, b wireGuardEngineRouterConfig) bool {
	return a.Interface == b.Interface &&
		a.MTU == b.MTU &&
		a.RouteTable == b.RouteTable &&
		a.FirewallMark == b.FirewallMark &&
		slices.Equal(a.Addresses, b.Addresses) &&
		slices.Equal(a.Routes, b.Routes) &&
		slices.Equal(a.DNS, b.DNS) &&
		slices.Equal(a.PostUp, b.PostUp) &&
		slices.Equal(a.PreDown, b.PreDown)
}

type wireGuardEngineRouterConfig struct {
	Interface    string
	MTU          int
	Addresses    []netip.Prefix
	Routes       []netip.Prefix
	DNS          []netip.Addr
	RouteTable   string
	FirewallMark uint32
	PostUp       []string
	PreDown      []string
}

func buildWireGuardEngineRouterConfig(interfaceName string, mtu int, cfg Config, networkMap clientapi.RegisterNodeResponse) (wireGuardEngineRouterConfig, error) {
	out := wireGuardEngineRouterConfig{
		Interface:  strings.TrimSpace(interfaceName),
		MTU:        mtu,
		RouteTable: strings.TrimSpace(cfg.WireGuardRouteTable),
	}
	if !safeWireGuardInterfaceName(out.Interface) {
		return out, fmt.Errorf("wireguard-go interface %q is invalid", out.Interface)
	}
	assigned, err := netip.ParseAddr(strings.TrimSpace(networkMap.Node.AssignedIP))
	if err != nil {
		return out, fmt.Errorf("parse assigned IPv4 address: %w", err)
	}
	out.Addresses = append(out.Addresses, netip.PrefixFrom(assigned, assigned.BitLen()))
	if value := strings.TrimSpace(networkMap.Node.AssignedIPv6); value != "" {
		assignedIPv6, err := netip.ParseAddr(value)
		if err != nil {
			return out, fmt.Errorf("parse assigned IPv6 address: %w", err)
		}
		out.Addresses = append(out.Addresses, netip.PrefixFrom(assignedIPv6, assignedIPv6.BitLen()))
	}
	seenRoutes := map[string]bool{}
	if out.RouteTable != "off" {
		for _, peer := range networkMap.Peers {
			for _, value := range peer.AllowedIPs {
				prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
				if err != nil {
					return out, fmt.Errorf("parse peer %s allowed IP %q: %w", firstNonEmptyString(peer.Hostname, peer.ID), value, err)
				}
				prefix = prefix.Masked()
				if seenRoutes[prefix.String()] {
					continue
				}
				seenRoutes[prefix.String()] = true
				out.Routes = append(out.Routes, prefix)
			}
		}
	}
	for _, value := range networkMap.Network.DNS {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return out, fmt.Errorf("parse DNS address %q: %w", value, err)
		}
		out.DNS = append(out.DNS, addr)
	}
	blockLAN, err := ExitLANPolicyBlocksLocalLAN(cfg.ExitLANPolicy)
	if err != nil {
		return out, err
	}
	for _, hook := range append(append(
		renderSubnetRouterSNATHooks(networkMap, cfg.SubnetRouterSNAT),
		renderExitLANFirewallHooks(networkMap.Peers, blockLAN)...),
		renderACLFirewallHooks(networkMap.Peers)...,
	) {
		kind, command, ok := strings.Cut(hook, " = ")
		if !ok {
			return out, fmt.Errorf("invalid generated WireGuard hook %q", hook)
		}
		command = strings.ReplaceAll(command, "%i", out.Interface)
		switch kind {
		case "PostUp":
			out.PostUp = append(out.PostUp, command)
		case "PreDown":
			out.PreDown = append(out.PreDown, command)
		default:
			return out, fmt.Errorf("unsupported generated WireGuard hook %q", kind)
		}
	}
	sort.Slice(out.Routes, func(i, j int) bool {
		if out.Routes[i].Addr().Compare(out.Routes[j].Addr()) != 0 {
			return out.Routes[i].Addr().Compare(out.Routes[j].Addr()) < 0
		}
		return out.Routes[i].Bits() < out.Routes[j].Bits()
	})
	out.FirewallMark = platformWireGuardEngineFirewallMark(out.Routes, out.RouteTable)
	return out, nil
}
