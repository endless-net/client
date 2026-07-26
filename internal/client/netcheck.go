package client

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/unng-lab/endlessnet-client/internal/stunclient"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

type STUNCheckResult struct {
	ID            string        `json:"id"`
	Addr          string        `json:"addr"`
	Reachable     bool          `json:"reachable"`
	MappedAddress string        `json:"mapped_address,omitempty"`
	Duration      time.Duration `json:"duration"`
	Error         string        `json:"error,omitempty"`
}

type STUNMappingSummary struct {
	Classification     string   `json:"classification"`
	TotalEndpoints     int      `json:"total_endpoints"`
	ReachableEndpoints int      `json:"reachable_endpoints"`
	MappedAddresses    []string `json:"mapped_addresses,omitempty"`
	Error              string   `json:"error,omitempty"`
}

type NetworkInterfaceStatus struct {
	Name         string   `json:"name"`
	Index        int      `json:"index"`
	MTU          int      `json:"mtu"`
	Flags        []string `json:"flags,omitempty"`
	AddressCount int      `json:"address_count"`
	Addresses    []string `json:"addresses,omitempty"`
	Prefixes     []string `json:"prefixes,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type OverlayCIDRConflict struct {
	OverlayCIDR   string `json:"overlay_cidr"`
	LocalPrefix   string `json:"local_prefix"`
	Interface     string `json:"interface"`
	AddressFamily string `json:"address_family"`
	Reason        string `json:"reason"`
}

func CheckSTUN(ctx context.Context, endpoints []clientapi.STUNEndpoint, timeout time.Duration) []STUNCheckResult {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	results := make([]STUNCheckResult, 0, len(endpoints))
	if len(endpoints) == 0 {
		return results
	}
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		for _, endpoint := range endpoints {
			endpoint.ID = strings.TrimSpace(endpoint.ID)
			endpoint.Addr = strings.TrimSpace(endpoint.Addr)
			if endpoint.Addr == "" {
				continue
			}
			results = append(results, STUNCheckResult{
				ID:       endpoint.ID,
				Addr:     endpoint.Addr,
				Duration: 0,
				Error:    err.Error(),
			})
		}
		return results
	}
	defer func() { _ = conn.Close() }()
	for _, endpoint := range endpoints {
		endpoint.ID = strings.TrimSpace(endpoint.ID)
		endpoint.Addr = strings.TrimSpace(endpoint.Addr)
		if endpoint.Addr == "" {
			continue
		}
		started := time.Now()
		result := STUNCheckResult{
			ID:       endpoint.ID,
			Addr:     endpoint.Addr,
			Duration: time.Since(started),
		}
		mapped, err := querySTUNOnSharedPacketConn(ctx, conn, endpoint.Addr, timeout)
		result.Duration = time.Since(started)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Reachable = true
			result.MappedAddress = mapped.String()
		}
		results = append(results, result)
	}
	return results
}

func querySTUNOnSharedPacketConn(ctx context.Context, conn net.PacketConn, serverAddr string, timeout time.Duration) (*net.UDPAddr, error) {
	addr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return nil, err
	}
	request, txID, err := stunclient.BuildBindingRequest()
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	if _, err := conn.WriteTo(request, addr); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	var lastParseErr error
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if lastParseErr != nil {
				return nil, lastParseErr
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, err
		}
		mapped, err := stunclient.ParseBindingResponse(buf[:n], txID)
		if err == nil {
			return mapped, nil
		}
		lastParseErr = err
	}
}

func ClassifySTUN(results []STUNCheckResult) STUNMappingSummary {
	summary := STUNMappingSummary{
		Classification: "no_endpoints",
		TotalEndpoints: len(results),
	}
	if len(results) == 0 {
		summary.Error = "network map does not contain STUN endpoints"
		return summary
	}
	mapped := map[string]bool{}
	for _, result := range results {
		if !result.Reachable || strings.TrimSpace(result.MappedAddress) == "" {
			continue
		}
		summary.ReachableEndpoints++
		mapped[strings.TrimSpace(result.MappedAddress)] = true
	}
	if summary.ReachableEndpoints == 0 {
		summary.Classification = "unreachable"
		summary.Error = "no STUN endpoint returned a mapped address"
		return summary
	}
	summary.MappedAddresses = make([]string, 0, len(mapped))
	for addr := range mapped {
		summary.MappedAddresses = append(summary.MappedAddresses, addr)
	}
	sort.Strings(summary.MappedAddresses)
	if len(summary.MappedAddresses) == 1 {
		summary.Classification = "consistent_mapping"
	} else {
		summary.Classification = "varying_mapping"
	}
	return summary
}

func LocalInterfaceStatuses() []NetworkInterfaceStatus {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []NetworkInterfaceStatus{{Error: err.Error()}}
	}
	out := make([]NetworkInterfaceStatus, 0, len(interfaces))
	for _, iface := range interfaces {
		status := NetworkInterfaceStatus{
			Name:  iface.Name,
			Index: iface.Index,
			MTU:   iface.MTU,
			Flags: interfaceFlagNames(iface.Flags),
		}
		addrs, err := iface.Addrs()
		if err != nil {
			status.Error = err.Error()
		} else {
			status.AddressCount = len(addrs)
			status.Addresses = networkAddresses(addrs)
			status.Prefixes = networkAddressPrefixes(addrs)
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Index == out[j].Index {
			return out[i].Name < out[j].Name
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func OverlayCIDRConflicts(networkMap clientapi.RegisterNodeResponse, interfaces []NetworkInterfaceStatus, ignoredInterfaces ...string) []OverlayCIDRConflict {
	overlayPrefixes := overlayCIDRPrefixes(networkMap)
	if len(overlayPrefixes) == 0 || len(interfaces) == 0 {
		return []OverlayCIDRConflict{}
	}
	ignored := map[string]bool{}
	for _, iface := range ignoredInterfaces {
		iface = strings.TrimSpace(iface)
		if iface != "" {
			ignored[iface] = true
		}
	}
	conflicts := []OverlayCIDRConflict{}
	for _, iface := range interfaces {
		if iface.Error != "" || ignored[iface.Name] || !interfaceFlagPresent(iface.Flags, "up") || interfaceFlagPresent(iface.Flags, "loopback") {
			continue
		}
		for _, prefixValue := range iface.Prefixes {
			localPrefix, err := netip.ParsePrefix(strings.TrimSpace(prefixValue))
			if err != nil {
				continue
			}
			localPrefix = localPrefix.Masked()
			if localPrefix.Bits() == localPrefix.Addr().BitLen() {
				continue
			}
			for _, overlay := range overlayPrefixes {
				if !prefixesOverlap(overlay, localPrefix) {
					continue
				}
				family := "ipv4"
				if overlay.Addr().Is6() {
					family = "ipv6"
				}
				conflicts = append(conflicts, OverlayCIDRConflict{
					OverlayCIDR:   overlay.String(),
					LocalPrefix:   localPrefix.String(),
					Interface:     iface.Name,
					AddressFamily: family,
					Reason:        "local interface prefix overlaps overlay CIDR",
				})
			}
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Interface != conflicts[j].Interface {
			return conflicts[i].Interface < conflicts[j].Interface
		}
		if conflicts[i].LocalPrefix != conflicts[j].LocalPrefix {
			return conflicts[i].LocalPrefix < conflicts[j].LocalPrefix
		}
		return conflicts[i].OverlayCIDR < conflicts[j].OverlayCIDR
	})
	return conflicts
}

func overlayCIDRPrefixes(networkMap clientapi.RegisterNodeResponse) []netip.Prefix {
	values := []string{networkMap.Network.CIDR, networkMap.Network.IPv6CIDR}
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			continue
		}
		out = append(out, prefix.Masked())
	}
	return out
}

func networkAddressPrefixes(addrs []net.Addr) []string {
	out := []string{}
	for _, addr := range addrs {
		prefix, ok := networkAddressPrefix(addr)
		if !ok {
			continue
		}
		out = append(out, prefix.String())
	}
	sort.Strings(out)
	return out
}

func networkAddresses(addrs []net.Addr) []string {
	out := []string{}
	for _, addr := range addrs {
		parsed, ok := networkAddress(addr)
		if !ok {
			continue
		}
		out = append(out, parsed.String())
	}
	sort.Strings(out)
	return out
}

func networkAddress(addr net.Addr) (netip.Addr, bool) {
	switch value := addr.(type) {
	case *net.IPNet:
		bits := 0
		if value.IP.To4() != nil {
			bits = 32
		}
		return netipAddrFromIP(value.IP, bits)
	case *net.IPAddr:
		return netipAddrFromIP(value.IP, 0)
	default:
		return netip.Addr{}, false
	}
}

func networkAddressPrefix(addr net.Addr) (netip.Prefix, bool) {
	switch value := addr.(type) {
	case *net.IPNet:
		ones, bits := value.Mask.Size()
		if ones < 0 {
			return netip.Prefix{}, false
		}
		parsed, ok := netipAddrFromIP(value.IP, bits)
		if !ok {
			return netip.Prefix{}, false
		}
		return netip.PrefixFrom(parsed, ones).Masked(), true
	case *net.IPAddr:
		parsed, ok := netipAddrFromIP(value.IP, 0)
		if !ok {
			return netip.Prefix{}, false
		}
		return netip.PrefixFrom(parsed, parsed.BitLen()), true
	default:
		return netip.Prefix{}, false
	}
}

func netipAddrFromIP(ip net.IP, bits int) (netip.Addr, bool) {
	if bits == 32 || ip.To4() != nil {
		addr, ok := netip.AddrFromSlice(ip.To4())
		return addr, ok
	}
	addr, ok := netip.AddrFromSlice(ip.To16())
	return addr, ok
}

func prefixesOverlap(a, b netip.Prefix) bool {
	if a.Addr().Is4() != b.Addr().Is4() || a.Addr().Is6() != b.Addr().Is6() {
		return false
	}
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func interfaceFlagPresent(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}
	return false
}

func interfaceFlagNames(flags net.Flags) []string {
	names := []string{}
	if flags&net.FlagUp != 0 {
		names = append(names, "up")
	}
	if flags&net.FlagBroadcast != 0 {
		names = append(names, "broadcast")
	}
	if flags&net.FlagLoopback != 0 {
		names = append(names, "loopback")
	}
	if flags&net.FlagPointToPoint != 0 {
		names = append(names, "point_to_point")
	}
	if flags&net.FlagMulticast != 0 {
		names = append(names, "multicast")
	}
	if flags&net.FlagRunning != 0 {
		names = append(names, "running")
	}
	return names
}
