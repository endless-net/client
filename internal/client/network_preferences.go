package client

import (
	"errors"
	"net/netip"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Local intent only. Absence restores the producer's unlocked baseline; a
// managed lock wins over a previously saved conflicting override. This state is
// part of each profile configuration and is never backend authorization.
type ClientNetworkPreferences struct {
	AllowInbound *bool `json:"allow_inbound,omitempty"`
	AcceptDNS    *bool `json:"accept_dns,omitempty"`
	AcceptRoutes *bool `json:"accept_routes,omitempty"`
}

type clientNetworkAcceptance struct {
	dns, routes, inbound bool
}

func resolveNetworkAcceptance(cfg Config, source api.RegisterNodeResponse, now time.Time) (clientNetworkAcceptance, error) {
	result := clientNetworkAcceptance{dns: true, routes: true, inbound: true}
	local := cfg.NetworkPreferences
	policy := source.Network.ClientPolicy
	if local == nil && policy == nil && source.MapSignature == nil && cfg.MapSigningTrust == nil {
		return result, nil // No local or managed override of the normal baseline.
	}
	if cfg.MapSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" || cfg.NodeID != source.Node.ID || cfg.NetworkID != source.Network.ID || api.ValidateNetworkMap(source) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(source, *cfg.MapSigningTrust) != nil || source.MapSignature == nil || !now.Before(source.MapSignature.ExpiresAt) {
		return result, errors.New("network preference map is not authenticated and current for this recipient")
	}
	resolve := func(key api.ClientSettingKey, requested *bool) bool {
		value, locked := true, false
		if policy != nil {
			for _, setting := range policy.Settings {
				if setting.Key == key {
					value, locked = *setting.BooleanValue, setting.Locked
				}
			}
		}
		if requested != nil && !locked {
			value = *requested
		}
		return value
	}
	var dns, routes, inbound *bool
	if local != nil {
		dns, routes = local.AcceptDNS, local.AcceptRoutes
		inbound = local.AllowInbound
	}
	result.dns, result.routes = resolve(api.ClientSettingAcceptDNS, dns), resolve(api.ClientSettingAcceptRoutes, routes)
	result.inbound = resolve(api.ClientSettingAllowInbound, inbound)
	return result, nil
}

// With route acceptance off, retain ordinary peer host routes. Subnet prefixes,
// explicitly managed single-IP subnets and application discovery routes are
// learned resource routes rather than host identity. Do not mutate signed data.
func restrictAcceptedResourceRoutes(routes []netip.Prefix, source api.RegisterNodeResponse) []netip.Prefix {
	resource := map[netip.Prefix]bool{}
	if policy := source.Network.ClientPolicy; policy != nil {
		for _, setting := range policy.Resources {
			if setting.Kind == api.ManagedResourceSubnet {
				if prefix, err := netip.ParsePrefix(setting.CIDR); err == nil {
					resource[prefix.Masked()] = true
				}
			}
		}
	}
	for _, app := range source.Network.Applications {
		for _, route := range app.Routes {
			for _, value := range route.CIDRs {
				if prefix, err := netip.ParsePrefix(value); err == nil {
					resource[prefix.Masked()] = true
				}
			}
		}
	}
	accepted := make([]netip.Prefix, 0, len(routes))
	for _, prefix := range routes {
		if prefix.IsSingleIP() && !resource[prefix.Masked()] {
			accepted = append(accepted, prefix)
		}
	}
	return accepted
}
