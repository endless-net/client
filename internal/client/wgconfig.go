package client

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
)

const wireGuardACLFirewallChainPrefix = "ENACL-"
const wireGuardExitLANFirewallChainPrefix = "ENLAN-"

type WireGuardRenderOptions struct {
	ListenPort            int
	MTU                   int
	RouteTable            string
	Interfaces            []NetworkInterfaceStatus
	PeerEndpointOverrides map[string]string
	SubnetRouterSNAT      bool
	ExitBlockLAN          bool
}

// RenderWireGuardWithOptionsChecked validates the complete untrusted map
// before producing a WireGuard configuration for explicit manual export.
func RenderWireGuardWithOptionsChecked(privateKey string, response clientapi.RegisterNodeResponse, opts WireGuardRenderOptions) (string, error) {
	if strings.TrimSpace(privateKey) == "" {
		return "", fmt.Errorf("wireguard private key is missing")
	}
	if err := wgkeys.ValidateSafeText("wireguard private key", privateKey); err != nil {
		return "", err
	}
	if err := clientapi.ValidateNetworkMap(response); err != nil {
		return "", fmt.Errorf("invalid network map: %w", err)
	}
	if err := validateWireGuardRenderOptions(opts); err != nil {
		return "", err
	}
	return renderWireGuardValidated(privateKey, response, opts), nil
}

func renderWireGuardValidated(privateKey string, response clientapi.RegisterNodeResponse, opts WireGuardRenderOptions) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[Interface]")
	fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	addresses := []string{fmt.Sprintf("%s/32", response.Node.AssignedIP)}
	if strings.TrimSpace(response.Node.AssignedIPv6) != "" {
		addresses = append(addresses, fmt.Sprintf("%s/128", response.Node.AssignedIPv6))
	}
	fmt.Fprintf(&b, "Address = %s\n", strings.Join(addresses, ", "))
	if opts.ListenPort > 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", opts.ListenPort)
	}
	if opts.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", opts.MTU)
	}
	if routeTable := strings.TrimSpace(opts.RouteTable); routeTable != "" {
		fmt.Fprintf(&b, "Table = %s\n", routeTable)
	}
	if dns := wireGuardExportDNS(response.Network); len(dns) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(dns, ", "))
	}
	for _, hook := range renderSubnetRouterSNATHooks(response, opts.SubnetRouterSNAT) {
		fmt.Fprintln(&b, hook)
	}
	for _, hook := range renderExitLANFirewallHooks(response.Peers, opts.ExitBlockLAN) {
		fmt.Fprintln(&b, hook)
	}
	for _, hook := range renderACLFirewallHooks(response.Peers) {
		fmt.Fprintln(&b, hook)
	}
	for _, peer := range response.Peers {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "[Peer]\n")
		fmt.Fprintf(&b, "# %s (%s)\n", peer.Hostname, peer.ID)
		fmt.Fprintf(&b, "PublicKey = %s\n", peer.PublicKey)
		if len(peer.AllowedIPs) > 0 {
			fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", "))
		}
		peerID := strings.TrimSpace(peer.ID)
		endpoint := strings.TrimSpace(opts.PeerEndpointOverrides[peerID])
		if endpoint == "" {
			endpoint = preferredPeerEndpoint(peer, opts.Interfaces)
		}
		if endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", endpoint)
			if opts.PeerEndpointOverrides[peerID] != "" || shouldEmitPersistentKeepalive(peer, endpoint, opts.Interfaces) {
				fmt.Fprintln(&b, "PersistentKeepalive = 25")
			}
		}
	}
	return b.String()
}

// A static WireGuard export cannot run the EndlessNet proxy required for
// MagicDNS or split DNS. Only a complete global override is safe to represent.
func wireGuardExportDNS(network clientapi.Network) []string {
	if network.DNSConfig == nil {
		return append([]string(nil), network.DNS...)
	}
	dns := network.DNSConfig
	if !dns.OverrideLocalDNS || dns.MagicDNSEnabled {
		return nil
	}
	servers := make([]string, 0, len(dns.Nameservers))
	for _, nameserver := range dns.Nameservers {
		if nameserver.Scope == "split" {
			return nil
		}
		if nameserver.Scope == "global" {
			servers = append(servers, nameserver.Address)
		}
	}
	return servers
}

func validateWireGuardRenderSafety(privateKey string, response clientapi.RegisterNodeResponse, opts WireGuardRenderOptions) error {
	values := []struct {
		field string
		value string
	}{
		{"private key", privateKey},
		{"node assigned IP", response.Node.AssignedIP},
		{"node assigned IPv6", response.Node.AssignedIPv6},
		{"network CIDR", response.Network.CIDR},
		{"network IPv6 CIDR", response.Network.IPv6CIDR},
		{"route table", opts.RouteTable},
	}
	for i, value := range response.Network.DNS {
		values = append(values, struct{ field, value string }{fmt.Sprintf("DNS[%d]", i), value})
	}
	for i, value := range response.Node.AdvertisedIPs {
		values = append(values, struct{ field, value string }{fmt.Sprintf("advertised IP[%d]", i), value})
	}
	for i, peer := range response.Peers {
		values = append(values,
			struct{ field, value string }{fmt.Sprintf("peer[%d] id", i), peer.ID},
			struct{ field, value string }{fmt.Sprintf("peer[%d] hostname", i), peer.Hostname},
			struct{ field, value string }{fmt.Sprintf("peer[%d] public key", i), peer.PublicKey},
			struct{ field, value string }{fmt.Sprintf("peer[%d] endpoint", i), peer.Endpoint},
		)
		for j, value := range peer.EndpointCandidates {
			values = append(values, struct{ field, value string }{fmt.Sprintf("peer[%d] endpoint candidate[%d]", i, j), value})
		}
		for j, value := range peer.AllowedIPs {
			values = append(values, struct{ field, value string }{fmt.Sprintf("peer[%d] allowed IP[%d]", i, j), value})
		}
	}
	for peerID, endpoint := range opts.PeerEndpointOverrides {
		values = append(values,
			struct{ field, value string }{"peer endpoint override id", peerID},
			struct{ field, value string }{"peer endpoint override", endpoint},
		)
	}
	for _, value := range values {
		if err := wgkeys.ValidateSafeText(value.field, value.value); err != nil {
			return err
		}
	}
	return nil
}

func validateWireGuardRenderOptions(opts WireGuardRenderOptions) error {
	if err := validateWireGuardRenderSafety("valid-placeholder", clientapi.RegisterNodeResponse{}, opts); err != nil {
		return err
	}
	if opts.ListenPort < 0 || opts.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 0 and 65535")
	}
	if opts.MTU < 0 || opts.MTU > 65535 {
		return fmt.Errorf("MTU must be between 0 and 65535")
	}
	if table := strings.TrimSpace(opts.RouteTable); table != "" && table != "auto" && table != "off" {
		value, err := strconv.ParseUint(table, 10, 32)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != table {
			return fmt.Errorf("route table must be auto, off, or a positive decimal table number")
		}
	}
	for peerID, endpoint := range opts.PeerEndpointOverrides {
		if strings.TrimSpace(peerID) == "" {
			return fmt.Errorf("peer endpoint override id is missing")
		}
		if err := wgkeys.ValidateEndpoint(endpoint); err != nil {
			return fmt.Errorf("peer endpoint override %q: %w", peerID, err)
		}
	}
	return nil
}

func renderSubnetRouterSNATHooks(response clientapi.RegisterNodeResponse, enabled bool) []string {
	if !enabled || strings.TrimSpace(response.Network.CIDR) == "" || len(response.Node.AdvertisedIPs) == 0 {
		return nil
	}
	overlay, err := netip.ParsePrefix(strings.TrimSpace(response.Network.CIDR))
	if err != nil || overlay.Addr().Is6() {
		return nil
	}
	hooks := []string{}
	seen := map[string]bool{}
	for _, advertised := range response.Node.AdvertisedIPs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(advertised))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if prefix.Addr().Is6() || seen[prefix.String()] {
			continue
		}
		seen[prefix.String()] = true
		probe := subnetRouterSNATProbeAddress(prefix)
		if !probe.IsValid() {
			continue
		}
		hooks = append(hooks, fmt.Sprintf("PostUp = sysctl -w net.ipv4.ip_forward=1 >/dev/null; lan_if=\"$(ip route get %s | awk '{for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}')\"; test -n \"$lan_if\"; iptables -C FORWARD -i %%i -o \"$lan_if\" -s %s -d %s -j ACCEPT 2>/dev/null || iptables -A FORWARD -i %%i -o \"$lan_if\" -s %s -d %s -j ACCEPT; iptables -C FORWARD -i \"$lan_if\" -o %%i -s %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || iptables -A FORWARD -i \"$lan_if\" -o %%i -s %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT; iptables -t nat -C POSTROUTING -s %s -d %s -o \"$lan_if\" -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -d %s -o \"$lan_if\" -j MASQUERADE", probe, overlay.String(), prefix.String(), overlay.String(), prefix.String(), prefix.String(), overlay.String(), prefix.String(), overlay.String(), overlay.String(), prefix.String(), overlay.String(), prefix.String()))
		hooks = append(hooks, fmt.Sprintf("PreDown = lan_if=\"$(ip route get %s | awk '{for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}' 2>/dev/null || true)\"; if [ -n \"$lan_if\" ]; then iptables -D FORWARD -i %%i -o \"$lan_if\" -s %s -d %s -j ACCEPT 2>/dev/null || true; iptables -D FORWARD -i \"$lan_if\" -o %%i -s %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || true; iptables -t nat -D POSTROUTING -s %s -d %s -o \"$lan_if\" -j MASQUERADE 2>/dev/null || true; fi", probe, overlay.String(), prefix.String(), prefix.String(), overlay.String(), overlay.String(), prefix.String()))
	}
	return hooks
}

func subnetRouterSNATProbeAddress(prefix netip.Prefix) netip.Addr {
	prefix = prefix.Masked()
	addr := prefix.Addr()
	if prefix.Bits() < addr.BitLen() {
		next := addr.Next()
		if next.IsValid() && prefix.Contains(next) {
			return next
		}
	}
	return addr
}

func renderExitLANFirewallHooks(peers []clientapi.Peer, enabled bool) []string {
	if !enabled {
		return nil
	}
	blockV4, blockV6 := false, false
	for _, peer := range peers {
		for _, allowed := range peer.AllowedIPs {
			prefix, ok := normalizeFirewallTarget(allowed)
			if !ok || prefix.Bits() != 0 {
				continue
			}
			if prefix.Addr().Is4() {
				blockV4 = true
			} else {
				blockV6 = true
			}
		}
	}
	if !blockV4 && !blockV6 {
		return nil
	}
	endpoints := exitLANPeerEndpointExclusions(peers)
	hooks := make([]string, 0)
	if blockV4 {
		hooks = append(hooks, renderExitLANFirewallHooksForTool("iptables", exitLANLocalIPv4Prefixes(), endpoints)...)
	}
	if blockV6 {
		hooks = append(hooks, renderExitLANFirewallHooksForTool("ip6tables", exitLANLocalIPv6Prefixes(), endpoints)...)
	}
	return hooks
}

func renderExitLANFirewallHooksForTool(tool string, prefixes []netip.Prefix, endpoints []netip.Addr) []string {
	chain := wireGuardExitLANFirewallChainPrefix + "%i"
	hooks := []string{
		fmt.Sprintf("PostUp = %s -N %s 2>/dev/null || true; %s -F %s; %s -C OUTPUT -j %s 2>/dev/null || %s -I OUTPUT -j %s", tool, chain, tool, chain, tool, chain, tool, chain),
	}
	for _, endpoint := range endpoints {
		if (tool == "iptables" && !endpoint.Is4()) || (tool == "ip6tables" && !endpoint.Is6()) {
			continue
		}
		hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s -d %s -j RETURN", tool, chain, netip.PrefixFrom(endpoint, endpoint.BitLen()).String()))
	}
	for _, prefix := range prefixes {
		hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s ! -o %%i -d %s -j REJECT", tool, chain, prefix.String()))
	}
	hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s -j RETURN", tool, chain))
	hooks = append(hooks, fmt.Sprintf("PreDown = %s -D OUTPUT -j %s 2>/dev/null || true; %s -F %s 2>/dev/null || true; %s -X %s 2>/dev/null || true", tool, chain, tool, chain, tool, chain))
	return hooks
}

func exitLANLocalIPv4Prefixes() []netip.Prefix {
	return mustPrefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16")
}

func exitLANLocalIPv6Prefixes() []netip.Prefix {
	return mustPrefixes("fc00::/7", "fe80::/10")
}

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		out = append(out, prefix.Masked())
	}
	return out
}

func exitLANPeerEndpointExclusions(peers []clientapi.Peer) []netip.Addr {
	seen := map[netip.Addr]bool{}
	out := []netip.Addr{}
	for _, peer := range peers {
		endpoints := append([]string{peer.Endpoint}, peer.EndpointCandidates...)
		for _, endpoint := range endpoints {
			host, _, err := net.SplitHostPort(strings.TrimSpace(endpoint))
			if err != nil {
				continue
			}
			addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
			if err != nil || !addr.IsValid() || seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, addr)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Compare(out[j]) < 0
	})
	return out
}

type aclFirewallTarget struct {
	tool   string
	target string
	ports  []clientapi.ACLPort
	grants []clientapi.ACLGrant
	deny   bool
}

func renderACLFirewallHooks(peers []clientapi.Peer) []string {
	targets := aclFirewallTargets(peers)
	if len(targets) == 0 {
		return nil
	}
	byTool := map[string][]aclFirewallTarget{}
	for _, target := range targets {
		byTool[target.tool] = append(byTool[target.tool], target)
	}
	tools := make([]string, 0, len(byTool))
	for tool := range byTool {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	hooks := []string{}
	for _, tool := range tools {
		hooks = append(hooks, fmt.Sprintf("PostUp = %s -N %s%%i 2>/dev/null || true; %s -F %s%%i; %s -C OUTPUT -j %s%%i 2>/dev/null || %s -I OUTPUT -j %s%%i", tool, wireGuardACLFirewallChainPrefix, tool, wireGuardACLFirewallChainPrefix, tool, wireGuardACLFirewallChainPrefix, tool, wireGuardACLFirewallChainPrefix))
		// Only replies bypass destination-port ACLs: their destination is the
		// caller's ephemeral port. Locally initiated established traffic must
		// still match the current grants after a policy revocation.
		hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -m conntrack --ctstate ESTABLISHED,RELATED --ctdir REPLY -j RETURN", tool, wireGuardACLFirewallChainPrefix))
		for _, target := range byTool[tool] {
			for _, grant := range target.grants {
				for _, destination := range grant.DestinationCIDRs {
					if len(grant.AllowedPorts) == 0 {
						hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -j RETURN", tool, wireGuardACLFirewallChainPrefix, destination))
					}
					for _, port := range grant.AllowedPorts {
						command := fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -p %s", tool, wireGuardACLFirewallChainPrefix, destination, aclFirewallProtocolForTool(tool, port.Protocol))
						if port.Protocol != "icmp" {
							command += fmt.Sprintf(" --dport %d", port.Port)
						}
						hooks = append(hooks, command+" -j RETURN")
					}
				}
			}
			for _, port := range target.ports {
				if port.Protocol == "icmp" {
					hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -p %s -j RETURN", tool, wireGuardACLFirewallChainPrefix, target.target, aclFirewallProtocolForTool(tool, port.Protocol)))
					continue
				}
				hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -p %s --dport %d -j RETURN", tool, wireGuardACLFirewallChainPrefix, target.target, port.Protocol, port.Port))
			}
			if !target.deny {
				hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -j RETURN", tool, wireGuardACLFirewallChainPrefix, target.target))
				continue
			}
			hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -o %%i -d %s -j REJECT", tool, wireGuardACLFirewallChainPrefix, target.target))
		}
		hooks = append(hooks, fmt.Sprintf("PostUp = %s -A %s%%i -j RETURN", tool, wireGuardACLFirewallChainPrefix))
		hooks = append(hooks, fmt.Sprintf("PreDown = %s -D OUTPUT -j %s%%i 2>/dev/null || true; %s -F %s%%i 2>/dev/null || true; %s -X %s%%i 2>/dev/null || true", tool, wireGuardACLFirewallChainPrefix, tool, wireGuardACLFirewallChainPrefix, tool, wireGuardACLFirewallChainPrefix))
	}
	return hooks
}

func aclFirewallTargets(peers []clientapi.Peer) []aclFirewallTarget {
	anyRestricted := false
	for _, peer := range peers {
		if peer.ACLRestricted || len(peer.AllowedPorts) > 0 || len(peer.ACLGrants) > 0 {
			anyRestricted = true
			break
		}
	}
	if !anyRestricted {
		return nil
	}
	byTarget := map[string]aclFirewallTarget{}
	for _, peer := range peers {
		ports := normalizeACLPorts(peer.AllowedPorts)
		deny := peer.ACLRestricted || len(ports) > 0 || len(peer.ACLGrants) > 0
		for _, allowed := range peer.AllowedIPs {
			prefix, ok := normalizeFirewallTarget(allowed)
			if !ok {
				continue
			}
			tool := "iptables"
			if prefix.Addr().Is6() {
				tool = "ip6tables"
			}
			key := tool + "\x00" + prefix.String()
			target := byTarget[key]
			target.tool = tool
			target.target = prefix.String()
			target.deny = target.deny || deny
			target.ports = mergeACLPorts(target.ports, ports)
			for _, grant := range peer.ACLGrants {
				for _, destination := range grant.DestinationCIDRs {
					grantPrefix, ok := normalizeFirewallTarget(destination)
					if !ok || grantPrefix.Addr().BitLen() != prefix.Addr().BitLen() {
						continue
					}
					intersection := grantPrefix
					if grantPrefix.Bits() <= prefix.Bits() && grantPrefix.Contains(prefix.Addr()) {
						intersection = prefix
					} else if !prefix.Contains(grantPrefix.Addr()) {
						continue
					}
					grantPorts := normalizeACLPorts(grant.AllowedPorts)
					if len(grant.AllowedPorts) > 0 && len(grantPorts) == 0 {
						continue
					}
					target.grants = append(target.grants, clientapi.ACLGrant{DestinationCIDRs: []string{intersection.String()}, AllowedPorts: grantPorts})
				}
			}
			byTarget[key] = target
		}
	}
	targets := make([]aclFirewallTarget, 0, len(byTarget))
	for _, target := range byTarget {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].tool != targets[j].tool {
			return targets[i].tool < targets[j].tool
		}
		// First-match firewall evaluation must follow route specificity: a
		// parent subnet's RETURN must not bypass a restricted child subnet.
		left := netip.MustParsePrefix(targets[i].target)
		right := netip.MustParsePrefix(targets[j].target)
		if left.Bits() != right.Bits() {
			return left.Bits() > right.Bits()
		}
		return targets[i].target < targets[j].target
	})
	return targets
}

func aclFirewallProtocolForTool(tool, protocol string) string {
	if protocol == "icmp" && tool == "ip6tables" {
		return "ipv6-icmp"
	}
	return protocol
}

func normalizeFirewallTarget(value string) (netip.Prefix, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ","))
	if value == "" {
		return netip.Prefix{}, false
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), true
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	return netip.Prefix{}, false
}

func normalizeACLPorts(ports []clientapi.ACLPort) []clientapi.ACLPort {
	seen := map[clientapi.ACLPort]bool{}
	out := make([]clientapi.ACLPort, 0, len(ports))
	for _, port := range ports {
		port.Protocol = strings.ToLower(strings.TrimSpace(port.Protocol))
		if port.Protocol == "icmp" {
			port.Port = 0
		} else if port.Protocol != "tcp" && port.Protocol != "udp" {
			continue
		}
		if port.Protocol != "icmp" && (port.Port <= 0 || port.Port > 65535) {
			continue
		}
		if seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		return out[i].Port < out[j].Port
	})
	return out
}

func mergeACLPorts(existing, next []clientapi.ACLPort) []clientapi.ACLPort {
	merged := append([]clientapi.ACLPort(nil), existing...)
	merged = append(merged, next...)
	return normalizeACLPorts(merged)
}

func RelayDataplaneEndpointOverrides(peers []clientapi.Peer, baseEndpoint string) (map[string]string, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(baseEndpoint))
	if err != nil {
		return nil, fmt.Errorf("relay dataplane endpoint must be host:base-port: %w", err)
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return nil, fmt.Errorf("relay dataplane endpoint host is required")
	}
	basePort, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || basePort <= 0 || basePort > 65535 {
		return nil, fmt.Errorf("relay dataplane endpoint base port must be between 1 and 65535")
	}
	ordered := append([]clientapi.Peer(nil), peers...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Hostname == ordered[j].Hostname {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Hostname < ordered[j].Hostname
	})
	if len(ordered) > 0 && basePort+len(ordered)-1 > 65535 {
		return nil, fmt.Errorf("relay dataplane endpoint range %d..%d exceeds port 65535", basePort, basePort+len(ordered)-1)
	}
	overrides := make(map[string]string, len(ordered))
	for i, peer := range ordered {
		peerID := strings.TrimSpace(peer.ID)
		if peerID == "" {
			return nil, fmt.Errorf("relay dataplane peer id is missing")
		}
		overrides[peerID] = net.JoinHostPort(host, strconv.Itoa(basePort+i))
	}
	return overrides, nil
}

func LocalEndpointCandidates(listenPort int, interfaces []NetworkInterfaceStatus) []string {
	if listenPort <= 0 || listenPort > 65535 {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, iface := range interfaces {
		if iface.Error != "" || !interfaceFlagPresent(iface.Flags, "up") || interfaceFlagPresent(iface.Flags, "loopback") || interfaceFlagPresent(iface.Flags, "point_to_point") {
			continue
		}
		for _, value := range iface.Addresses {
			addr, err := netip.ParseAddr(strings.TrimSpace(value))
			if err != nil || !usableDirectEndpointAddress(addr) {
				continue
			}
			endpoint := net.JoinHostPort(addr.String(), strconv.Itoa(listenPort))
			if seen[endpoint] {
				continue
			}
			seen[endpoint] = true
			out = append(out, endpoint)
		}
	}
	sort.Strings(out)
	return out
}

func preferredPeerEndpoint(peer clientapi.Peer, interfaces []NetworkInterfaceStatus) string {
	fallback := strings.TrimSpace(peer.Endpoint)
	prefixes := usableLANPrefixes(interfaces)
	if len(prefixes) > 0 {
		for _, candidate := range orderedPeerEndpointCandidates(peer) {
			addr, ok := endpointAddr(candidate)
			if !ok || !usableDirectEndpointAddress(addr) {
				continue
			}
			for _, prefix := range prefixes {
				if prefix.Addr().Is6() == addr.Is6() && prefix.Contains(addr) {
					return candidate
				}
			}
		}
	}
	families := usableEndpointFamilies(interfaces)
	if families.v4 || families.v6 {
		for _, candidate := range orderedPeerEndpointCandidates(peer) {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || !endpointMatchesFamilies(candidate, families) {
				continue
			}
			return candidate
		}
	}
	if fallbackAddr, ok := endpointAddr(fallback); ok && fallbackAddr.Is6() {
		for _, candidate := range orderedPeerEndpointCandidates(peer) {
			addr, ok := endpointAddr(candidate)
			if ok && addr.Is4() {
				return strings.TrimSpace(candidate)
			}
		}
	}
	if fallback == "" {
		for _, candidate := range orderedPeerEndpointCandidates(peer) {
			if strings.TrimSpace(candidate) != "" {
				return candidate
			}
		}
	}
	return fallback
}

func shouldEmitPersistentKeepalive(peer clientapi.Peer, endpoint string, interfaces []NetworkInterfaceStatus) bool {
	if !peerEndpointCandidateContains(peer, endpoint) {
		return true
	}
	addr, ok := endpointAddr(endpoint)
	if !ok || !usableDirectEndpointAddress(addr) {
		return true
	}
	for _, prefix := range usableLANPrefixes(interfaces) {
		if prefix.Addr().Is6() == addr.Is6() && prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func peerEndpointCandidateContains(peer clientapi.Peer, endpoint string) bool {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false
	}
	for _, candidate := range peer.EndpointCandidates {
		if strings.TrimSpace(candidate) == endpoint {
			return true
		}
	}
	return false
}

func orderedPeerEndpointCandidates(peer clientapi.Peer) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(peer.EndpointCandidates)+1)
	for _, candidate := range peer.EndpointCandidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	endpoint := strings.TrimSpace(peer.Endpoint)
	if endpoint != "" && !seen[endpoint] {
		out = append(out, endpoint)
	}
	return out
}

type endpointFamilies struct {
	v4 bool
	v6 bool
}

func usableEndpointFamilies(interfaces []NetworkInterfaceStatus) endpointFamilies {
	var families endpointFamilies
	for _, iface := range interfaces {
		if iface.Error != "" || !interfaceFlagPresent(iface.Flags, "up") || interfaceFlagPresent(iface.Flags, "loopback") || interfaceFlagPresent(iface.Flags, "point_to_point") {
			continue
		}
		for _, value := range iface.Addresses {
			addr, err := netip.ParseAddr(strings.TrimSpace(value))
			if err != nil || !usableDirectEndpointAddress(addr) {
				continue
			}
			if addr.Is4() {
				families.v4 = true
			} else {
				families.v6 = true
			}
		}
		for _, value := range iface.Prefixes {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil || !usableDirectEndpointAddress(prefix.Addr()) {
				continue
			}
			if prefix.Addr().Is4() {
				families.v4 = true
			} else {
				families.v6 = true
			}
		}
	}
	return families
}

func endpointMatchesFamilies(endpoint string, families endpointFamilies) bool {
	addr, ok := endpointAddr(endpoint)
	if !ok {
		return true
	}
	if addr.Is4() {
		return families.v4
	}
	return families.v6
}

func usableLANPrefixes(interfaces []NetworkInterfaceStatus) []netip.Prefix {
	var out []netip.Prefix
	for _, iface := range interfaces {
		if iface.Error != "" || !interfaceFlagPresent(iface.Flags, "up") || interfaceFlagPresent(iface.Flags, "loopback") || interfaceFlagPresent(iface.Flags, "point_to_point") {
			continue
		}
		for _, value := range iface.Prefixes {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			prefix = prefix.Masked()
			if prefix.Addr().Is4() {
				if prefix.Bits() >= 31 || !usableLANIPv4Address(prefix.Addr()) {
					continue
				}
			} else {
				if prefix.Bits() >= 127 || !usableDirectEndpointAddress(prefix.Addr()) {
					continue
				}
			}
			out = append(out, prefix)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bits() != out[j].Bits() {
			return out[i].Bits() > out[j].Bits()
		}
		return out[i].String() < out[j].String()
	})
	return out
}

func endpointAddr(endpoint string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(endpoint))
	if err != nil {
		return netip.Addr{}, false
	}
	host = strings.Trim(host, "[]")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}

func usableLANIPv4Address(addr netip.Addr) bool {
	return addr.Is4() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsUnspecified()
}

func usableDirectEndpointAddress(addr netip.Addr) bool {
	return addr.IsGlobalUnicast() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && !addr.IsUnspecified()
}
