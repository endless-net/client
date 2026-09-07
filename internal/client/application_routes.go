package client

import (
	"errors"
	"net/netip"
	"slices"
	"sort"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func applicationSelf(m clientapi.RegisterNodeResponse) clientapi.ServiceHost {
	return clientapi.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
}

func verifyApplicationMap(cfg Config, m clientapi.RegisterNodeResponse) error {
	trust, err := SigningTrustBundle(cfg)
	if err != nil {
		return err
	}
	if err := clientapi.ValidateNetworkMap(m); err != nil {
		return err
	}
	if err := clientapi.VerifyNetworkMapSignatureWithTrustBundle(m, trust); err != nil {
		return err
	}
	if cfg.NodeID != m.Node.ID || cfg.NetworkID != m.Network.ID {
		return errors.New("application map belongs to another node or network")
	}
	for _, app := range m.Network.Applications {
		for _, route := range app.Routes {
			for _, value := range route.CIDRs {
				prefix := netip.MustParsePrefix(value)
				for _, peer := range m.Peers {
					if peer.ID == route.Connector.NodeID {
						continue
					}
					for _, value := range peer.AllowedIPs {
						existing, err := netip.ParsePrefix(value)
						if err == nil && existing.Bits() >= prefix.Bits() && existing.Overlaps(prefix) {
							return errors.New("application destination conflicts with an existing peer route")
						}
					}
				}
			}
		}
	}
	return nil
}

// The original signed map remains intact. This derived peer list supplies only
// route and firewall configuration, never signature verification or discovery.
func applicationRoutePeers(m clientapi.RegisterNodeResponse, now time.Time) []clientapi.Peer {
	peers := cloneRegisterNodeResponse(m).Peers
	self := applicationSelf(m)
	owners := map[string]string{}
	for _, app := range m.Network.Applications {
		if !slices.Contains(app.Sources, self) || slices.Contains(app.Connectors, self) {
			continue
		}
		for _, route := range app.Routes {
			if !now.Before(route.ExpiresAt) {
				continue
			}
			for _, cidr := range route.CIDRs {
				if old := owners[cidr]; old == "" || route.Connector.NodeID < old {
					owners[cidr] = route.Connector.NodeID
				}
			}
		}
	}
	for i := range peers {
		peer := &peers[i]
		originalIPs := append([]string(nil), peer.AllowedIPs...)
		unrestricted := !peer.ACLRestricted && len(peer.AllowedPorts) == 0 && len(peer.ACLGrants) == 0
		for cidr, owner := range owners {
			if owner == peer.ID && !slices.Contains(peer.AllowedIPs, cidr) {
				peer.AllowedIPs = append(peer.AllowedIPs, cidr)
			}
		}
		for _, app := range m.Network.Applications {
			if !slices.Contains(app.Sources, self) || slices.Contains(app.Connectors, self) {
				continue
			}
			target, err := clientapi.ParseApplicationTarget(app.TargetType, app.Target)
			if err != nil {
				continue
			}
			for _, route := range app.Routes {
				if route.Connector.NodeID != peer.ID || !now.Before(route.ExpiresAt) {
					continue
				}
				for _, cidr := range route.CIDRs {
					if owners[cidr] != peer.ID {
						continue
					}
					grant := clientapi.ACLGrant{DestinationCIDRs: []string{cidr}}
					if target.TCPPort != 0 {
						grant.AllowedPorts = []clientapi.ACLPort{{Protocol: "tcp", Port: int(target.TCPPort)}}
					}
					peer.ACLGrants = append(peer.ACLGrants, grant)
				}
			}
		}
		sort.Strings(peer.AllowedIPs)
		if unrestricted && len(peer.ACLGrants) > 0 {
			peer.ACLGrants = append(peer.ACLGrants, clientapi.ACLGrant{DestinationCIDRs: originalIPs})
		}
	}
	return peers
}

func applicationDNSDomains(m clientapi.RegisterNodeResponse) []string {
	var domains []string
	for _, app := range m.Network.Applications {
		// Connector discovery must use its local DNS view, without recursing into
		// the signed answer it is responsible for discovering.
		if !app.DNSEnabled || slices.Contains(app.Connectors, applicationSelf(m)) {
			continue
		}
		target, err := clientapi.ParseApplicationTarget(app.TargetType, app.Target)
		if err == nil && target.Domain != "" {
			domains = append(domains, target.Domain)
		}
	}
	sort.Strings(domains)
	return slices.Compact(domains)
}

func applicationDNSResponse(request []byte, q dnsQuestion, opts DNSProxyOptions) ([]byte, bool) {
	if !slices.Contains(applicationDNSDomains(opts.NetworkMap), normalizeDNSName(q.Name)) {
		return nil, false
	}
	if opts.SigningTrust == nil || clientapi.ValidateNetworkMap(opts.NetworkMap) != nil || clientapi.VerifyNetworkMapSignatureWithTrustBundle(opts.NetworkMap, *opts.SigningTrust) != nil {
		return dnsErrorResponse(request, dnsRCodeFail), true
	}
	if q.Class != dnsClassIN || q.Type != dnsTypeA && q.Type != dnsTypeAAAA {
		return dnsResponseHeader(request, dnsRCodeNoErr, 0, nil), true
	}
	seen := map[netip.Addr]bool{}
	for _, app := range opts.NetworkMap.Network.Applications {
		target, _ := clientapi.ParseApplicationTarget(app.TargetType, app.Target)
		if target.Domain != normalizeDNSName(q.Name) || !slices.Contains(app.Sources, applicationSelf(opts.NetworkMap)) {
			continue
		}
		for _, route := range app.Routes {
			if !time.Now().Before(route.ExpiresAt) {
				continue
			}
			for _, cidr := range route.CIDRs {
				p, err := netip.ParsePrefix(cidr)
				if err == nil && (q.Type == dnsTypeA && p.Addr().Is4() || q.Type == dnsTypeAAAA && p.Addr().Is6()) {
					seen[p.Addr()] = true
				}
			}
		}
	}
	addresses := make([]netip.Addr, 0, len(seen))
	for addr := range seen {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i].Less(addresses[j]) })
	var answers []byte
	limit := opts.responseLimit
	if limit == 0 {
		limit = dnsMaxUDPBytes
	}
	for _, addr := range addresses {
		if q.QuestionEnd+len(answers)+len(addr.AsSlice())+12 > limit {
			response := dnsResponseHeader(request, dnsRCodeNoErr, 0, nil)
			response[2] |= 0x02
			return response, true
		}
		answers = appendServiceDNSAnswer(answers, q.Type, 0, addr.AsSlice())
	}
	if len(addresses) == 0 {
		return dnsErrorResponse(request, dnsRCodeNX), true
	}
	return dnsResponseHeader(request, dnsRCodeNoErr, uint16(len(addresses)), answers), true
}
