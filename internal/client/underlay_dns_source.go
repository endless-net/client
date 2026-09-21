package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This is an observation of non-Client systemd-resolved sources, not a grant
// to resolve arbitrary names or proof of their current network reachability.
// Callers bind it to their runtime identity and revalidate before reusing it.
type underlayDNSSource struct {
	OwnInterface string
	Owner        string
	Links        []underlayDNSLink
	// Non-owned interface identity/address state also invalidates a source when
	// DHCP or uplink changes leave the resolver's numeric address unchanged.
	Interfaces []underlayDNSInterface
}

type underlayDNSLink struct {
	Index        int
	Name         string
	Servers      []netip.AddrPort
	Domains      []underlayDNSDomain
	DefaultRoute bool
	DNSSEC       string
	DNSOverTLS   string
}

type underlayDNSDomain struct {
	Name      string
	RouteOnly bool
}

func cloneUnderlayDNSSource(source *underlayDNSSource) *underlayDNSSource {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Links = slices.Clone(source.Links)
	clone.Interfaces = slices.Clone(source.Interfaces)
	for i := range clone.Interfaces {
		clone.Interfaces[i].Addresses = slices.Clone(source.Interfaces[i].Addresses)
	}
	for i := range clone.Links {
		clone.Links[i].Servers = slices.Clone(source.Links[i].Servers)
		clone.Links[i].Domains = slices.Clone(source.Links[i].Domains)
	}
	return &clone
}

var errUnderlayDNSSource = errors.New("pre-exit DNS source is unavailable or ambiguous")

const resolveManagerPath = "/org/freedesktop/resolve1"

// The two complete observations must agree, including link identity and local
// addresses. All D-Bus reads target the captured unique owner. This detects a
// concurrent resolver restart/configuration change; it is not an OS watch or a
// lease. No /etc/resolv.conf/stub/global-public-resolver fallback is permitted.
func captureUnderlayDNSSource(ctx context.Context, ownInterface string, runner CommandRunner) (*underlayDNSSource, error) {
	if strings.TrimSpace(ownInterface) != ownInterface || !safeWireGuardInterfaceName(ownInterface) || ownInterface == "lo" {
		return nil, errUnderlayDNSSource
	}
	if runner == nil {
		if runtime.GOOS != "linux" {
			return nil, errUnderlayDNSSource
		}
		runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return runExitCommand(ctx, "", name, args...)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	read := func(name string, args ...string) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := runner(ctx, name, args...)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, errUnderlayDNSSource
		}
		return underlayDNSJSON(raw)
	}
	bus := func(destination, path, iface, method, signature string, args ...string) (any, error) {
		command := []string{"--system", "--json=short", "--timeout=5s", "--auto-start=no", "--allow-interactive-authorization=no", "call", destination, path, iface, method, signature}
		return read("busctl", append(command, args...)...)
	}
	owner := func() (string, error) {
		raw, err := bus("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "GetNameOwner", "s", "org.freedesktop.resolve1")
		if err != nil {
			return "", err
		}
		value, ok := underlayDNSBusReply(raw, "s")
		name, valid := value.(string)
		if !ok || !valid || !underlayDNSOwnerValid(name) {
			return "", errUnderlayDNSSource
		}
		return name, nil
	}
	name, err := owner()
	if err != nil {
		return nil, err
	}
	properties := func(path, iface string) (map[string]any, error) {
		raw, err := bus(name, path, "org.freedesktop.DBus.Properties", "GetAll", "s", iface)
		if err != nil {
			return nil, err
		}
		value, ok := underlayDNSBusReply(raw, "a{sv}")
		props, valid := value.(map[string]any)
		if !ok || !valid || len(props) > 64 {
			return nil, errUnderlayDNSSource
		}
		return props, nil
	}
	var first *underlayDNSSource
	var firstInterfaces []underlayDNSInterface
	for range 2 {
		raw, err := read("ip", "-j", "address", "show")
		if err != nil {
			return nil, err
		}
		interfaces, err := underlayDNSInterfaces(raw)
		if err != nil {
			return nil, err
		}
		manager, err := properties(resolveManagerPath, "org.freedesktop.resolve1.Manager")
		if err != nil {
			return nil, err
		}
		source, err := underlayDNSCaptureLinks(name, ownInterface, interfaces, manager, func(index int) (map[string]any, error) {
			raw, err := bus(name, resolveManagerPath, "org.freedesktop.resolve1.Manager", "GetLink", "i", strconv.Itoa(index))
			if err != nil {
				return nil, err
			}
			value, ok := underlayDNSBusReply(raw, "o")
			path, valid := value.(string)
			if !ok || !valid || path != resolveManagerPath+"/link/_"+strconv.Itoa(index) {
				return nil, errUnderlayDNSSource
			}
			return properties(path, "org.freedesktop.resolve1.Link")
		})
		if err != nil {
			return nil, err
		}
		if first != nil && (!reflect.DeepEqual(source, first) || !reflect.DeepEqual(interfaces, firstInterfaces)) {
			return nil, errUnderlayDNSSource
		}
		first, firstInterfaces = source, interfaces
	}
	lastOwner, err := owner()
	if err != nil {
		return nil, err
	}
	if lastOwner != name {
		return nil, errUnderlayDNSSource
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return first, nil
}

func underlayDNSJSON(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errUnderlayDNSSource
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, ok := exitGuardJSONValue(decoder, 0)
	if !ok {
		return nil, errUnderlayDNSSource
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errUnderlayDNSSource
	}
	return value, nil
}

func underlayDNSVariant(value any, signature string) (any, bool) {
	object, ok := value.(map[string]any)
	return object["data"], ok && len(object) == 2 && object["type"] == signature && object["data"] != nil
}

func underlayDNSBusReply(value any, signature string) (any, bool) {
	data, ok := underlayDNSVariant(value, signature)
	values, valid := data.([]any)
	if !ok || !valid || len(values) != 1 {
		return nil, false
	}
	return values[0], true
}

func underlayDNSOwnerValid(name string) bool {
	if len(name) < 4 || len(name) > 128 || name[0] != ':' {
		return false
	}
	parts := strings.Split(name[1:], ".")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return true
}

func underlayDNSInt(value any, min, max int) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(string(number), 10, 64)
	return int(n), err == nil && n >= int64(min) && n <= int64(max)
}

type underlayDNSInterface struct {
	Index     int
	Name      string
	Up        bool
	Loopback  bool
	Addresses []netip.Addr
}

func underlayDNSInterfaces(value any) ([]underlayDNSInterface, error) {
	array, ok := value.([]any)
	if !ok || len(array) == 0 || len(array) > 128 {
		return nil, errUnderlayDNSSource
	}
	result := make([]underlayDNSInterface, 0, len(array))
	indices, names := map[int]bool{}, map[string]bool{}
	for _, value := range array {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, errUnderlayDNSSource
		}
		index, validIndex := underlayDNSInt(object["ifindex"], 1, 1<<31-1)
		name, validName := object["ifname"].(string)
		flags, validFlags := object["flags"].([]any)
		addresses, validAddresses := object["addr_info"].([]any)
		if !validIndex || !validName || strings.TrimSpace(name) != name || !safeWireGuardInterfaceName(name) || !validFlags || !validAddresses || len(flags) > 32 || len(addresses) > 128 || indices[index] || names[name] {
			return nil, errUnderlayDNSSource
		}
		indices[index], names[name] = true, true
		link := underlayDNSInterface{Index: index, Name: name}
		for _, flag := range flags {
			text, ok := flag.(string)
			if !ok {
				return nil, errUnderlayDNSSource
			}
			link.Up = link.Up || text == "UP"
			link.Loopback = link.Loopback || text == "LOOPBACK"
		}
		for _, value := range addresses {
			address, ok := value.(map[string]any)
			if !ok {
				return nil, errUnderlayDNSSource
			}
			local, ok := address["local"].(string)
			ip, err := netip.ParseAddr(local)
			if !ok || err != nil || ip.Zone() != "" {
				return nil, errUnderlayDNSSource
			}
			link.Addresses = append(link.Addresses, ip.Unmap())
		}
		sort.Slice(link.Addresses, func(i, j int) bool { return link.Addresses[i].Less(link.Addresses[j]) })
		result = append(result, link)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	return result, nil
}
