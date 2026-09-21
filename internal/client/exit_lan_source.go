package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errExitLANSource = errors.New("physical LAN source is unavailable")

// A source is read-only hardware-backed candidate evidence, never a packet-time device
// incarnation or no-gateway proof. In particular ifindex/name may be reused.
// Nonvirtual sysfs topology cannot rule out emulated hardware in a guest;
// platform qualification is required before treating candidates as physical.
// No LAN_ALLOW capability may be inferred from successful collection alone.
// ValidUntil is the earliest finite address validity/preference or route deadline,
// conservatively measured from before its command. It is not a renewable lease.
type exitLANSource struct {
	OwnInterface string
	Links        []exitLANLink
	ValidUntil   time.Time
}
type exitLANLink struct {
	Index, LinkIndex                                     int
	Name, DevicePath, HardwareAddress, Driver, Subsystem string
	CarrierChanges                                       uint32
	Addresses                                            []netip.Prefix
	Routes                                               []exitLANDirectRoute
}
type exitLANDirectRoute struct {
	Prefix                  netip.Prefix
	Protocol, Metric, Scope uint32
	Preference              string
	Timed                   bool
}
type exitLANPhysical struct {
	Index, LinkIndex, Type                         int
	DevicePath, HardwareAddress, Driver, Subsystem string
	CarrierChanges                                 uint32
	Up                                             bool
}

// eligible=false means a positively classified unsupported/nonphysical link.
// Unknown/unreadable identity is an error, never physical-by-default.
type exitLANPhysicalInspector func(context.Context, string) (identity exitLANPhysical, eligible bool, err error)

func exitLANInterfaceName(name string) bool {
	return name != "." && name != ".." && strings.TrimSpace(name) == name && safeWireGuardInterfaceName(name)
}

func captureExitLANSource(ctx context.Context, own string, run CommandRunner, inspect exitLANPhysicalInspector) (*exitLANSource, error) {
	if !exitLANInterfaceName(own) || own == "lo" || run == nil || inspect == nil {
		return nil, errExitLANSource
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before, err := readExitLANSource(ctx, own, run, inspect)
	if err != nil {
		return nil, err
	}
	after, err := readExitLANSource(ctx, own, run, inspect)
	if err != nil {
		return nil, err
	}
	deadline := before.ValidUntil
	if deadline.IsZero() || (!after.ValidUntil.IsZero() && after.ValidUntil.Before(deadline)) {
		deadline = after.ValidUntil
	}
	before.ValidUntil, after.ValidUntil = time.Time{}, time.Time{}
	if !reflect.DeepEqual(before, after) {
		return nil, errExitLANSource
	}
	after.ValidUntil = deadline
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return nil, errExitLANSource
	}
	return after, nil
}

func exitLANRows(ctx context.Context, run CommandRunner, args ...string) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := run(ctx, "ip", args...)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errExitLANSource
	}
	value, err := underlayDNSJSON(raw)
	rows, ok := value.([]any)
	if err != nil || !ok || len(rows) > 4096 {
		return nil, errExitLANSource
	}
	return rows, nil
}

func readExitLANSource(ctx context.Context, own string, run CommandRunner, inspect exitLANPhysicalInspector) (*exitLANSource, error) {
	rows, err := exitLANRows(ctx, run, "-j", "-d", "link", "show")
	if err != nil {
		return nil, err
	}
	if len(rows) > 128 {
		return nil, errExitLANSource
	}
	source := &exitLANSource{OwnInterface: own}
	byName := map[string]int{}
	indices := map[int]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, errExitLANSource
		}
		name, ok := row["ifname"].(string)
		index, valid := underlayDNSInt(row["ifindex"], 1, 1<<31-1)
		if !ok || !valid || !exitLANInterfaceName(name) || indices[index] {
			return nil, errExitLANSource
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, errExitLANSource
		}
		indices[index] = true
		byName[name] = -1
		if name == own || name == "lo" || row["link_type"] != "ether" || row["master"] != nil || row["linkinfo"] != nil || row["link_netnsid"] != nil || row["protodown"] == true {
			continue
		}
		flags, ok := row["flags"].([]any)
		if !ok || len(flags) > 32 {
			return nil, errExitLANSource
		}
		up, lower, excluded := false, false, false
		for _, value := range flags {
			flag, ok := value.(string)
			if !ok {
				return nil, errExitLANSource
			}
			up = up || flag == "UP"
			lower = lower || flag == "LOWER_UP"
			excluded = excluded || flag == "LOOPBACK" || flag == "POINTOPOINT" || flag == "NOARP"
		}
		if !up || !lower || excluded {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		physical, eligible, err := inspect(ctx, name)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, errExitLANSource
		}
		if !eligible {
			continue
		}
		if physical.Driver == "" || physical.Subsystem == "" {
			return nil, errExitLANSource
		}
		if exitLANVirtualDevice(physical.DevicePath, physical.Driver, physical.Subsystem) {
			continue
		}
		address, ok := row["address"].(string)
		if value, present := row["link_index"]; present {
			linked, valid := underlayDNSInt(value, 1, 1<<31-1)
			if !valid || linked != physical.LinkIndex {
				return nil, errExitLANSource
			}
		}
		hardware, hardwareErr := net.ParseMAC(address)
		if !ok || hardwareErr != nil || len(hardware) != 6 || !physical.Up || physical.Index != index || physical.LinkIndex != index || physical.Type != 1 || physical.HardwareAddress != address || path.Clean(physical.DevicePath) != physical.DevicePath || !strings.HasPrefix(physical.DevicePath, "/sys/devices/") || physical.DevicePath == "/sys/devices/virtual" || strings.HasPrefix(physical.DevicePath, "/sys/devices/virtual/") {
			return nil, errExitLANSource
		}
		byName[name] = len(source.Links)
		source.Links = append(source.Links, exitLANLink{Index: index, LinkIndex: physical.LinkIndex, Name: name, DevicePath: physical.DevicePath, HardwareAddress: physical.HardwareAddress, Driver: physical.Driver, Subsystem: physical.Subsystem, CarrierChanges: physical.CarrierChanges})
	}
	addressReadStarted := time.Now()
	rows, err = exitLANRows(ctx, run, "-j", "address", "show")
	if err != nil {
		return nil, err
	}
	if len(rows) > 128 {
		return nil, errExitLANSource
	}
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, errExitLANSource
		}
		name, ok := row["ifname"].(string)
		if !ok || seen[name] {
			return nil, errExitLANSource
		}
		seen[name] = true
		position, exists := byName[name]
		if !exists || position < 0 {
			continue
		}
		link := &source.Links[position]
		index, valid := underlayDNSInt(row["ifindex"], 1, 1<<31-1)
		if !valid || index != link.Index {
			return nil, errExitLANSource
		}
		addresses, ok := row["addr_info"].([]any)
		if !ok || len(addresses) > 128 {
			return nil, errExitLANSource
		}
		for _, value := range addresses {
			a, ok := value.(map[string]any)
			if !ok {
				return nil, errExitLANSource
			}
			local, ok := a["local"].(string)
			ip, parseErr := netip.ParseAddr(local)
			bits, valid := underlayDNSInt(a["prefixlen"], 0, 128)
			if !ok || parseErr != nil || ip.Zone() != "" || ip.Is4In6() || !valid || bits > ip.BitLen() {
				return nil, errExitLANSource
			}
			if a["family"] != "inet" && a["family"] != "inet6" {
				return nil, errExitLANSource
			}
			if (a["family"] == "inet") != ip.Is4() {
				return nil, errExitLANSource
			}
			prefix := netip.PrefixFrom(ip, bits)
			if !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() || !exitLANPrefixAllowed(prefix) || a["scope"] != "global" || a["tentative"] == true || a["dadfailed"] == true || a["deprecated"] == true || a["peer"] != nil {
				continue
			}
			if flags, present := a["flags"]; present {
				values, ok := flags.([]any)
				if !ok || len(values) > 32 {
					return nil, errExitLANSource
				}
				bad := false
				for _, value := range values {
					flag, ok := value.(string)
					if !ok {
						return nil, errExitLANSource
					}
					bad = bad || flag == "tentative" || flag == "dadfailed" || flag == "deprecated"
				}
				if bad {
					continue
				}
			}
			validLife, ok := exitLANLifetime(a["valid_life_time"])
			preferredLife, preferredOK := exitLANLifetime(a["preferred_life_time"])
			if !ok || !preferredOK {
				return nil, errExitLANSource
			}
			if !validLife || !preferredLife {
				continue
			}
			for _, field := range []string{"valid_life_time", "preferred_life_time"} {
				if seconds, ok := exitLANUint(a[field]); ok && seconds != 1<<32-1 {
					deadline := addressReadStarted.Add(time.Duration(seconds) * time.Second)
					if source.ValidUntil.IsZero() || deadline.Before(source.ValidUntil) {
						source.ValidUntil = deadline
					}
				}
			}
			link.Addresses = append(link.Addresses, prefix)
		}
	}
	for _, link := range source.Links {
		if !seen[link.Name] {
			return nil, errExitLANSource
		}
	}
	var indirect []netip.Prefix
	for _, family := range []string{"-4", "-6"} {
		routeReadStarted := time.Now()
		rows, err = exitLANRows(ctx, run, "-j", "-N", family, "route", "show", "table", "main")
		if err != nil {
			return nil, err
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				return nil, errExitLANSource
			}
			dst, ok := row["dst"].(string)
			if !ok {
				return nil, errExitLANSource
			}
			if dst == "default" {
				continue
			}
			prefix, err := netip.ParsePrefix(dst)
			if err != nil {
				if ip, e := netip.ParseAddr(dst); e == nil {
					prefix = netip.PrefixFrom(ip, ip.BitLen())
				} else {
					return nil, errExitLANSource
				}
			}
			if (family == "-4") != prefix.Addr().Is4() {
				return nil, errExitLANSource
			}
			if !exitLANPrefixAllowed(prefix) {
				continue
			}
			direct := true
			for _, key := range []string{"gateway", "via", "nexthop", "nhid", "multipath", "nexthops", "encap"} {
				if _, present := row[key]; present {
					direct = false
				}
			}
			if typ, present := row["type"]; present && typ != "unicast" {
				direct = false
			}
			if !direct {
				indirect = append(indirect, prefix)
				continue
			}
			name, _ := row["dev"].(string)
			position, exists := byName[name]
			if !exists || position < 0 {
				continue
			}
			link := &source.Links[position]
			matched := false
			for _, address := range link.Addresses {
				matched = matched || address.Masked() == prefix.Masked()
			}
			if !matched {
				continue
			}
			route := exitLANDirectRoute{Prefix: prefix.Masked(), Protocol: 3}
			for key, value := range row {
				switch key {
				case "dst", "dev", "type":
				case "table":
					n, ok := exitLANUint(value)
					if !ok || n != 254 {
						return nil, errExitLANSource
					}
				case "protocol":
					n, ok := exitLANUint(value)
					if !ok {
						return nil, errExitLANSource
					}
					route.Protocol = n
				case "metric":
					n, ok := exitLANUint(value)
					if !ok {
						return nil, errExitLANSource
					}
					route.Metric = n
				case "scope":
					n, ok := exitLANUint(value)
					if !ok {
						return nil, errExitLANSource
					}
					route.Scope = n
				case "prefsrc":
					text, ok := value.(string)
					ip, e := netip.ParseAddr(text)
					local := false
					for _, address := range link.Addresses {
						local = local || address.Addr() == ip
					}
					if !ok || e != nil || !prefix.Contains(ip) || !local {
						return nil, errExitLANSource
					}
				case "pref":
					if family != "-6" || (value != "low" && value != "medium" && value != "high") {
						return nil, errExitLANSource
					}
					route.Preference = value.(string)
				case "expires":
					// iproute2 print_rta_cacheinfo emits signed integer seconds,
					// truncating the remaining kernel ticks. Never round upward or
					// treat an omitted/zero RA deadline as permanent authority.
					number, numeric := value.(json.Number)
					seconds, parseErr := strconv.ParseInt(string(number), 10, 32)
					if family != "-6" || !numeric || parseErr != nil || seconds <= 0 {
						return nil, errExitLANSource
					}
					route.Timed = true
					deadline := routeReadStarted.Add(time.Duration(seconds) * time.Second)
					if source.ValidUntil.IsZero() || deadline.Before(source.ValidUntil) {
						source.ValidUntil = deadline
					}
				case "flags":
					flags, ok := value.([]any)
					if !ok || len(flags) != 0 {
						return nil, errExitLANSource
					}
				default:
					return nil, errExitLANSource
				}
			}
			if route.Scope != 0 && route.Scope != 253 {
				return nil, errExitLANSource
			}
			// RTPROT_RA (9) is accepted only with explicit IPv6 preference and
			// lifetime. Missing metadata cannot grant an unbounded candidate.
			if route.Protocol == 9 && (family != "-6" || !route.Timed || route.Preference == "") {
				return nil, errExitLANSource
			}
			link.Routes = append(link.Routes, route)
		}
	}
	for i := range source.Links {
		link := &source.Links[i]
		sort.Slice(link.Addresses, func(i, j int) bool { return link.Addresses[i].String() < link.Addresses[j].String() })
		sort.Slice(link.Routes, func(i, j int) bool { return link.Routes[i].Prefix.String() < link.Routes[j].Prefix.String() })
		for i := 1; i < len(link.Addresses); i++ {
			if link.Addresses[i] == link.Addresses[i-1] {
				return nil, errExitLANSource
			}
		}
		for i := 1; i < len(link.Routes); i++ {
			if link.Routes[i].Prefix == link.Routes[i-1].Prefix {
				return nil, errExitLANSource
			}
		}
	}
	sort.Slice(source.Links, func(i, j int) bool { return source.Links[i].Index < source.Links[j].Index })
	work := 0
	for i, link := range source.Links {
		for _, route := range link.Routes {
			for _, excluded := range indirect {
				work++
				if work > exitLANWorkLimit {
					return nil, errExitLANSource
				}
				if route.Prefix.Overlaps(excluded) {
					return nil, errExitLANSource
				}
			}
			for _, other := range source.Links[i+1:] {
				for _, candidate := range other.Routes {
					work++
					if work > exitLANWorkLimit {
						return nil, errExitLANSource
					}
					if route.Prefix.Overlaps(candidate.Prefix) {
						return nil, errExitLANSource
					}
				}
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return source, nil
}

func exitLANUint(value any) (uint32, bool) {
	text, ok := value.(string)
	if !ok {
		number, valid := value.(json.Number)
		if !valid {
			return 0, false
		}
		text = string(number)
	}
	n, err := strconv.ParseUint(text, 10, 32)
	return uint32(n), err == nil && text != "" && strings.Trim(text, "0123456789") == ""
}
func exitLANLifetime(value any) (bool, bool) {
	if value == "forever" {
		return true, true
	}
	n, ok := exitLANUint(value)
	return n > 0, ok
}
func exitLANVirtualDevice(device, driver, subsystem string) bool {
	for _, value := range []string{"virtio", "vmbus", "xen"} {
		if strings.Contains(device, "/"+value) || subsystem == value {
			return true
		}
	}
	switch driver {
	case "virtio_net", "virtio-net", "vmxnet3", "hv_netvsc", "xen_netfront", "xen-netfront":
		return true
	}
	return false
}
func exitLANPrefixAllowed(prefix netip.Prefix) bool {
	if !prefix.IsValid() || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
		return false
	}
	// Partial overlap is retained as topology: application policy must subtract
	// excluded destinations (including IPv4 network/broadcast) before any use.
	for _, text := range []string{"0.0.0.0/32", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255/32", "::/128", "::1/128", "fe80::/10", "ff00::/8", "::ffff:0:0/96"} {
		reserved := netip.MustParsePrefix(text)
		if prefix.Bits() >= reserved.Bits() && reserved.Contains(prefix.Addr()) {
			return false
		}
	}
	return true
}
