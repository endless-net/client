package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var errUnderlayDNSRoute = errors.New("marked pre-exit DNS route is unconfirmed")

type underlayDNSRouteObservation struct {
	Server         netip.AddrPort
	Network        string
	Mark           uint32
	InterfaceIndex int
	InterfaceName  string
	Source         netip.Addr
}

// Observe the kernel's resolved route with the same mark, transport and bound
// interface as the DNS socket. This is a point-in-time observation, not a lease,
// socket attestation, firewall proof or evidence of upstream reachability.
func observeUnderlayDNSRoute(ctx context.Context, ownInterface string, mark uint32, network string, server netip.AddrPort, link underlayDNSLink, source *underlayDNSSource, runner CommandRunner) (underlayDNSRouteObservation, error) {
	var empty underlayDNSRouteObservation
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if strings.TrimSpace(ownInterface) != ownInterface || !safeWireGuardInterfaceName(ownInterface) || mark == 0 || source == nil || len(source.Interfaces) == 0 || len(source.Interfaces) > 128 || len(source.Links) > 128 || !server.IsValid() || server.Port() == 0 {
		return empty, errUnderlayDNSRoute
	}
	protocol := strings.TrimSuffix(strings.TrimSuffix(network, "4"), "6")
	ip := server.Addr()
	if (protocol != "tcp" && protocol != "udp") || (strings.HasSuffix(network, "4") && !ip.Is4()) || (strings.HasSuffix(network, "6") && !ip.Is6()) || ip.Is4In6() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return empty, errUnderlayDNSRoute
	}
	member := false
	for _, candidate := range source.Links {
		if candidate.Index == link.Index && candidate.Name == link.Name && candidate.DNSSEC == "no" && candidate.DNSOverTLS == "no" && slices.Contains(candidate.Servers, server) {
			member = true
		}
	}
	if !member || link.Index < 0 || link.Name == ownInterface || (link.Index == 0 && link.Name != "") || (link.Index != 0 && (!safeWireGuardInterfaceName(link.Name) || strings.TrimSpace(link.Name) != link.Name)) || (ip.Zone() != "" && ip.Zone() != link.Name) || (ip.IsLinkLocalUnicast() && (link.Index == 0 || ip.Zone() != link.Name)) {
		return empty, errUnderlayDNSRoute
	}
	if runner == nil {
		if runtime.GOOS != "linux" {
			return empty, errUnderlayDNSRoute
		}
		runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return runExitCommand(ctx, "", name, args...)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	family := "-6"
	if ip.Is4() {
		family = "-4"
	}
	args := []string{"-j", "-N", family, "route", "get", ip.WithZone("").String(), "mark", strconv.FormatUint(uint64(mark), 10), "ipproto", protocol, "dport", strconv.Itoa(int(server.Port()))}
	if link.Index != 0 {
		args = append(args, "oif", link.Name)
	}
	raw, err := runner(ctx, "ip", args...)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil {
		return empty, errUnderlayDNSRoute
	}
	value, err := underlayDNSJSON(raw)
	if err != nil {
		return empty, errUnderlayDNSRoute
	}
	rows, ok := value.([]any)
	if !ok || len(rows) != 1 {
		return empty, errUnderlayDNSRoute
	}
	route, ok := rows[0].(map[string]any)
	if !ok {
		return empty, errUnderlayDNSRoute
	}
	destination, _ := route["dst"].(string)
	dst, err := netip.ParseAddr(destination)
	if err != nil || dst != ip.WithZone("") {
		return empty, errUnderlayDNSRoute
	}
	device, _ := route["dev"].(string)
	if device == ownInterface || device == "" || device == "lo" || (link.Index != 0 && device != link.Name) {
		return empty, errUnderlayDNSRoute
	}
	var iface *underlayDNSInterface
	for i := range source.Interfaces {
		candidate := &source.Interfaces[i]
		if candidate.Name == device {
			if iface != nil || candidate.Index <= 0 || !candidate.Up || candidate.Loopback || (link.Index != 0 && candidate.Index != link.Index) {
				return empty, errUnderlayDNSRoute
			}
			iface = candidate
		}
	}
	if iface == nil {
		return empty, errUnderlayDNSRoute
	}
	preferred, _ := route["prefsrc"].(string)
	local, err := netip.ParseAddr(preferred)
	if err != nil || local.Zone() != "" || local.Is4() != ip.Is4() || local.IsUnspecified() || local.IsLoopback() || !slices.Contains(iface.Addresses, local) {
		return empty, errUnderlayDNSRoute
	}
	for key, value := range route {
		switch key {
		case "dst", "dev", "prefsrc":
		case "type":
			if value != "unicast" {
				return empty, errUnderlayDNSRoute
			}
		case "mark":
			var text string
			switch number := value.(type) {
			case json.Number:
				text = number.String()
			case string:
				text = number
			default:
				return empty, errUnderlayDNSRoute
			}
			base := 10
			if strings.HasPrefix(text, "0x") {
				base = 16
				text = strings.TrimPrefix(text, "0x")
			}
			number, err := strconv.ParseUint(text, base, 32)
			if err != nil || uint32(number) != mark {
				return empty, errUnderlayDNSRoute
			}
		case "flags", "cache":
			values, ok := value.([]any)
			if !ok || len(values) != 0 {
				return empty, errUnderlayDNSRoute
			}
		case "gateway":
			text, ok := value.(string)
			gateway, err := netip.ParseAddr(text)
			if !ok || err != nil || gateway.Is4() != ip.Is4() || gateway.IsUnspecified() || gateway.IsLoopback() || gateway.IsMulticast() {
				return empty, errUnderlayDNSRoute
			}
		case "src", "from":
			text, ok := value.(string)
			selector, err := netip.ParseAddr(text)
			if !ok || err != nil || selector.Is4() != ip.Is4() || (!selector.IsUnspecified() && selector != local) {
				return empty, errUnderlayDNSRoute
			}
		case "table", "protocol", "scope", "metric", "pref", "uid", "expires": // Routing metadata; does not establish interface or source identity.
		default:
			return empty, errUnderlayDNSRoute
		}
	}
	return underlayDNSRouteObservation{Server: server, Network: network, Mark: mark, InterfaceIndex: iface.Index, InterfaceName: device, Source: local}, nil
}
