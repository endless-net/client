package client

import (
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Property signatures and JSON envelopes follow systemd resolve1's Manager /
// Link APIs and busctl's sd_bus_message_dump_json. DNSEx preserves explicit
// port/SNI configuration; reading DNS alone could silently downgrade it.
func underlayDNSCaptureLinks(owner, ownInterface string, interfaces []underlayDNSInterface, manager map[string]any, readLink func(int) (map[string]any, error)) (*underlayDNSSource, error) {
	servers, err := underlayDNSIndexGroups(manager["DNSEx"], "a(iiayqs)", 5)
	if err != nil {
		return nil, err
	}
	domains, err := underlayDNSIndexGroups(manager["Domains"], "a(isb)", 3)
	if err != nil {
		return nil, err
	}
	byIndex := map[int]underlayDNSInterface{}
	local := map[netip.Addr]bool{}
	for _, link := range interfaces {
		byIndex[link.Index] = link
		for _, address := range link.Addresses {
			local[address] = true
		}
	}
	for index := range domains {
		if index != 0 {
			if _, exists := byIndex[index]; !exists {
				return nil, errUnderlayDNSSource
			}
		}
	}
	indices := make([]int, 0, len(servers))
	// resolved's dns_scope_good_domain excludes scopes without a DNS server
	// before considering their search/routing domains.
	for index := range servers {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	source := &underlayDNSSource{Owner: owner}
	for _, iface := range interfaces {
		if iface.Name != ownInterface {
			copy := iface
			copy.Addresses = slices.Clone(iface.Addresses)
			source.Interfaces = append(source.Interfaces, copy)
		}
	}
	for _, index := range indices {
		link := underlayDNSLink{Index: index}
		properties := manager
		if index != 0 {
			iface, exists := byIndex[index]
			if !exists {
				return nil, errUnderlayDNSSource
			}
			if iface.Name == ownInterface || iface.Loopback || !iface.Up {
				continue
			}
			link.Name = iface.Name
			properties, err = readLink(index)
			if err != nil {
				return nil, err
			}
			// Recheck the manager's aggregate against the corresponding link.
			// A mixed observation must not borrow a new link's security/defaults.
			dns, ok := underlayDNSVariant(properties["DNSEx"], "a(iayqs)")
			if !ok || !reflect.DeepEqual(dns, servers[index]) {
				return nil, errUnderlayDNSSource
			}
			routed, ok := underlayDNSVariant(properties["Domains"], "a(sb)")
			if !ok {
				return nil, errUnderlayDNSSource
			}
			list, ok := routed.([]any)
			if !ok || !slices.EqualFunc(list, domains[index], reflect.DeepEqual) {
				return nil, errUnderlayDNSSource
			}
			mask, ok := underlayDNSVariant(properties["ScopesMask"], "t")
			scopes, valid := underlayDNSInt(mask, 0, 31)
			if !ok || !valid {
				return nil, errUnderlayDNSSource
			}
			if scopes&1 == 0 {
				continue
			}
			value, ok := underlayDNSVariant(properties["DefaultRoute"], "b")
			defaultRoute, valid := value.(bool)
			if !ok || !valid {
				return nil, errUnderlayDNSSource
			}
			link.DefaultRoute = defaultRoute
		} else {
			// Configured global DNS participates when no routing domain wins.
			// FallbackDNS/CurrentDNSServer are deliberately never read as source.
			link.DefaultRoute = true
		}
		security, securityOK := underlayDNSVariant(properties["DNSSEC"], "s")
		tls, tlsOK := underlayDNSVariant(properties["DNSOverTLS"], "s")
		if !securityOK || !tlsOK || security != "no" || tls != "no" {
			return nil, errUnderlayDNSSource
		}
		link.DNSSEC, link.DNSOverTLS = "no", "no"
		link.Servers, err = underlayDNSServers(servers[index], link.Name, local)
		if err != nil {
			return nil, err
		}
		link.Domains, err = underlayDNSDomains(domains[index])
		if err != nil {
			return nil, err
		}
		if len(link.Servers) == 0 {
			return nil, errUnderlayDNSSource
		}
		source.Links = append(source.Links, link)
	}
	if len(source.Links) == 0 {
		return nil, errUnderlayDNSSource
	}
	return source, nil
}

func underlayDNSIndexGroups(value any, signature string, tupleSize int) (map[int][]any, error) {
	data, ok := underlayDNSVariant(value, signature)
	array, valid := data.([]any)
	if !ok || !valid || len(array) > 512 {
		return nil, errUnderlayDNSSource
	}
	groups := map[int][]any{}
	for _, value := range array {
		tuple, ok := value.([]any)
		if !ok || len(tuple) != tupleSize {
			return nil, errUnderlayDNSSource
		}
		index, valid := underlayDNSInt(tuple[0], 0, 1<<31-1)
		if !valid {
			return nil, errUnderlayDNSSource
		}
		groups[index] = append(groups[index], tuple[1:])
		if len(groups[index]) > 64 || len(groups) > 128 {
			return nil, errUnderlayDNSSource
		}
	}
	return groups, nil
}

func underlayDNSServers(values []any, interfaceName string, local map[netip.Addr]bool) ([]netip.AddrPort, error) {
	servers := make([]netip.AddrPort, 0, len(values))
	for _, value := range values {
		tuple, ok := value.([]any)
		if !ok || len(tuple) != 4 {
			return nil, errUnderlayDNSSource
		}
		family, validFamily := underlayDNSInt(tuple[0], 2, 10)
		bytes, validBytes := tuple[1].([]any)
		port, validPort := underlayDNSInt(tuple[2], 0, 65535)
		if !validFamily || !validBytes || !validPort || tuple[3] != "" || (family != 2 && family != 10) || (family == 2 && len(bytes) != 4) || (family == 10 && len(bytes) != 16) {
			return nil, errUnderlayDNSSource
		}
		address := make([]byte, len(bytes))
		for i, value := range bytes {
			number, ok := underlayDNSInt(value, 0, 255)
			if !ok {
				return nil, errUnderlayDNSSource
			}
			address[i] = byte(number)
		}
		ip, valid := netip.AddrFromSlice(address)
		if !valid || ip.Is4In6() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || local[ip] || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
			return nil, errUnderlayDNSSource
		}
		if ip.Is6() && ip.IsLinkLocalUnicast() {
			if interfaceName == "" {
				return nil, errUnderlayDNSSource
			}
			ip = ip.WithZone(interfaceName)
		}
		if port == 0 {
			port = 53
		}
		server := netip.AddrPortFrom(ip, uint16(port))
		if slices.Contains(servers, server) {
			return nil, errUnderlayDNSSource
		}
		servers = append(servers, server)
	}
	// Preserve per-link server preference order, which is meaningful source data.
	return servers, nil
}

func underlayDNSDomains(values []any) ([]underlayDNSDomain, error) {
	domains := make([]underlayDNSDomain, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		tuple, ok := value.([]any)
		if !ok || len(tuple) != 2 {
			return nil, errUnderlayDNSSource
		}
		name, validName := tuple[0].(string)
		routeOnly, validFlag := tuple[1].(bool)
		if !validName || !validFlag {
			return nil, errUnderlayDNSSource
		}
		name = strings.ToLower(name)
		if name != "." {
			name = strings.TrimSuffix(name, ".")
		}
		if !underlayDNSDomainValid(name) || seen[name] || name == "." && !routeOnly {
			return nil, errUnderlayDNSSource
		}
		seen[name] = true
		domains = append(domains, underlayDNSDomain{Name: name, RouteOnly: routeOnly})
	}
	// Preserve search-domain order even though underlay lookups never expand it.
	return domains, nil
}

func underlayDNSDomainValid(name string) bool {
	if name == "." {
		return true
	}
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}
