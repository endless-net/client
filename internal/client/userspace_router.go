package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

type wireGuardEngineRouter interface {
	Configure(context.Context, wireGuardEngineRouterConfig) error
	Down(context.Context) error
}

type dnsAwareWireGuardEngineRouter struct {
	base      wireGuardEngineRouter
	dnsCancel context.CancelFunc
	dnsDone   chan error
}

func newWireGuardEngineRouter(interfaceName string, runner CommandRunner, inputRunner commandInputRunner) (wireGuardEngineRouter, error) {
	base, err := newPlatformWireGuardEngineRouter(interfaceName, runner, inputRunner)
	if err != nil {
		return nil, err
	}
	return &dnsAwareWireGuardEngineRouter{base: base}, nil
}

func (r *dnsAwareWireGuardEngineRouter) Configure(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	r.stopDNSProxy()
	if cfg.DNSProxy != nil {
		if err := r.startDNSProxy(ctx, *cfg.DNSProxy); err != nil {
			return err
		}
	}
	if err := r.base.Configure(ctx, cfg); err != nil {
		r.stopDNSProxy()
		return err
	}
	return nil
}

func (r *dnsAwareWireGuardEngineRouter) Down(ctx context.Context) error {
	err := r.base.Down(ctx)
	r.stopDNSProxy()
	return err
}

func (r *dnsAwareWireGuardEngineRouter) startDNSProxy(ctx context.Context, opts DNSProxyOptions) error {
	proxyCtx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	opts.Ready = func(string) { ready <- struct{}{} }
	go func() { done <- ServeDNSProxy(proxyCtx, opts) }()
	select {
	case <-ready:
		r.dnsCancel = cancel
		r.dnsDone = done
		return nil
	case err := <-done:
		cancel()
		if err == nil {
			err = errors.New("DNS proxy stopped before becoming ready")
		}
		return fmt.Errorf("start DNS proxy: %w", err)
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case <-time.After(3 * time.Second):
		cancel()
		return errors.New("start DNS proxy: readiness timeout")
	}
}

func (r *dnsAwareWireGuardEngineRouter) stopDNSProxy() {
	if r.dnsCancel == nil {
		return
	}
	r.dnsCancel()
	select {
	case <-r.dnsDone:
	case <-time.After(3 * time.Second):
	}
	r.dnsCancel = nil
	r.dnsDone = nil
}

func wireGuardEngineRouterConfigsEqual(a, b wireGuardEngineRouterConfig) bool {
	return a.Interface == b.Interface &&
		a.MTU == b.MTU &&
		a.RouteTable == b.RouteTable &&
		a.FirewallMark == b.FirewallMark &&
		slices.Equal(a.Addresses, b.Addresses) &&
		slices.Equal(a.Routes, b.Routes) &&
		slices.Equal(a.DNS, b.DNS) &&
		slices.Equal(a.DNSDomains, b.DNSDomains) &&
		slices.Equal(a.SearchDomains, b.SearchDomains) &&
		a.DNSOverride == b.DNSOverride &&
		a.DNSConfigPresent == b.DNSConfigPresent &&
		dnsProxyOptionsEqual(a.DNSProxy, b.DNSProxy) &&
		slices.Equal(a.PostUp, b.PostUp) &&
		slices.Equal(a.PreDown, b.PreDown)
}

type wireGuardEngineRouterConfig struct {
	Interface        string
	MTU              int
	Addresses        []netip.Prefix
	Routes           []netip.Prefix
	DNS              []netip.Addr
	DNSDomains       []string
	SearchDomains    []string
	DNSOverride      bool
	DNSConfigPresent bool
	DNSProxy         *DNSProxyOptions
	RouteTable       string
	FirewallMark     uint32
	PostUp           []string
	PreDown          []string
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
	if dns := networkMap.Network.DNSConfig; dns != nil {
		out.DNSConfigPresent = true
		out.DNSOverride = dns.OverrideLocalDNS
		out.SearchDomains = append([]string(nil), dns.SearchDomains...)
		splitRulesByDomain := make(map[string][]string)
		globalUpstreams := make([]string, 0)
		globalDNS := make([]netip.Addr, 0)
		for _, nameserver := range dns.Nameservers {
			address, err := netip.ParseAddr(nameserver.Address)
			if err != nil {
				return out, fmt.Errorf("parse effective DNS server %q: %w", nameserver.Address, err)
			}
			upstream := net.JoinHostPort(address.String(), "53")
			if nameserver.Scope == "global" {
				if !slices.Contains(globalUpstreams, upstream) {
					globalUpstreams = append(globalUpstreams, upstream)
				}
				if !slices.Contains(globalDNS, address) {
					globalDNS = append(globalDNS, address)
				}
			}
			if nameserver.Scope == "split" {
				for _, domain := range nameserver.SplitDomains {
					domain = normalizeDNSName(domain)
					if !slices.Contains(out.DNSDomains, domain) {
						out.DNSDomains = append(out.DNSDomains, domain)
					}
					if !slices.Contains(splitRulesByDomain[domain], upstream) {
						splitRulesByDomain[domain] = append(splitRulesByDomain[domain], upstream)
					}
				}
			}
		}
		splitDomains := make([]string, 0, len(splitRulesByDomain))
		for domain := range splitRulesByDomain {
			splitDomains = append(splitDomains, domain)
		}
		sort.Strings(splitDomains)
		splitRules := make([]SplitDNSRule, 0, len(splitDomains))
		for _, domain := range splitDomains {
			splitRules = append(splitRules, SplitDNSRule{Domain: domain, Upstreams: splitRulesByDomain[domain]})
		}
		out.DNS = globalDNS
		searchDomain := ""
		if dns.MagicDNSEnabled {
			searchDomain = dns.Suffix
			if searchDomain == "" {
				searchDomain = DefaultDNSDomain(networkMap.Network.Name)
			}
			out.DNSDomains = append(out.DNSDomains, searchDomain)
		}
		if searchDomain != "" || len(splitRules) > 0 {
			out.DNS = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
			out.DNSProxy = &DNSProxyOptions{
				ListenAddr: "127.0.0.1:53", UpstreamAddrs: globalUpstreams,
				SplitRules: splitRules, NetworkMap: networkMap, ServePeerDNS: dns.MagicDNSEnabled, SearchDomain: searchDomain,
			}
		}
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

func dnsProxyOptionsEqual(a, b *DNSProxyOptions) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ListenAddr == b.ListenAddr && slices.Equal(a.UpstreamAddrs, b.UpstreamAddrs) &&
		a.ServePeerDNS == b.ServePeerDNS && a.SearchDomain == b.SearchDomain && reflect.DeepEqual(a.SplitRules, b.SplitRules) &&
		reflect.DeepEqual(a.NetworkMap, b.NetworkMap)
}
