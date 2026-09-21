package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

var errResourceHostObservation = errors.New("resource host transport observation unavailable")

func resourceRouteRunner(runner CommandRunner) (CommandRunner, error) {
	if runner != nil {
		return runner, nil
	}
	if runtime.GOOS != "linux" {
		return nil, errResourceHostObservation
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return runExitCommand(ctx, "", name, args...)
	}, nil
}

func observeResourceHostRules(ctx context.Context, mark uint32, runner CommandRunner) ([32]byte, error) {
	all := []byte{}
	for _, family := range []string{"-4", "-6"} {
		raw, err := runner(ctx, "ip", family, "-j", "-N", "-d", "rule", "show")
		if ctx.Err() != nil {
			return [32]byte{}, ctx.Err()
		}
		if err != nil || !resourceHostRulesObserved(raw, mark) {
			return [32]byte{}, errResourceHostObservation
		}
		all = append(all, raw...)
		all = append(all, 0)
	}
	return sha256.Sum256(all), nil
}

// Different UID/source/transport selectors can make the same host lookup route
// differently for another local flow. Accept only selector-free table lookups
// and this engine's exact understood unmarked exit lookup/suppression rules.
func resourceHostRulesObserved(raw []byte, mark uint32) bool {
	rules, err := decodeLinuxPolicyRules(raw)
	if err != nil || len(rules) == 0 || len(rules) > 4096 {
		return false
	}
	priorities := map[uint32]bool{}
	for _, rule := range rules {
		var priority uint32
		if json.Unmarshal(rule["priority"], &priority) != nil || priorities[priority] {
			return false
		}
		priorities[priority] = true
		var table, src string
		if json.Unmarshal(rule["table"], &table) != nil || json.Unmarshal(rule["src"], &src) != nil || src != "all" {
			return false
		}
		if validExitPolicyTable(mark) && ((table == strconv.FormatUint(uint64(mark), 10) && linuxPolicyRuleExact(rule, mark, false)) || (table == "254" && linuxPolicyRuleExact(rule, mark, true))) {
			continue
		}
		id, err := strconv.ParseUint(table, 10, 32)
		if err != nil || id == 0 {
			return false
		}
		for key, value := range rule {
			switch key {
			case "priority", "table", "src":
			case "protocol":
				var text string
				if json.Unmarshal(value, &text) != nil {
					return false
				}
				if _, err := strconv.ParseUint(text, 10, 32); err != nil {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func resourceRouteInterface(ctx context.Context, iface string, runner CommandRunner) (underlayDNSInterface, error) {
	var empty underlayDNSInterface
	if !safeWireGuardInterfaceName(iface) || strings.TrimSpace(iface) != iface || iface == "lo" {
		return empty, errResourceHostObservation
	}
	raw, err := runner(ctx, "ip", "-j", "address", "show", "dev", iface)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if err != nil {
		return empty, errResourceHostObservation
	}
	value, err := underlayDNSJSON(raw)
	if err != nil {
		return empty, errResourceHostObservation
	}
	if rows, ok := value.([]any); ok {
		for _, item := range rows {
			row, ok := item.(map[string]any)
			if !ok {
				return empty, errResourceHostObservation
			}
			addresses, _ := row["addr_info"].([]any)
			for _, item := range addresses {
				address, ok := item.(map[string]any)
				if !ok {
					return empty, errResourceHostObservation
				}
				for _, flag := range []string{"tentative", "dadfailed", "deprecated"} {
					if value, exists := address[flag]; exists && value != false {
						return empty, errResourceHostObservation
					}
				}
			}
		}
	}
	links, err := underlayDNSInterfaces(value)
	if err != nil || len(links) != 1 || links[0].Name != iface || !links[0].Up || links[0].Loopback {
		return empty, errResourceHostObservation
	}
	return links[0], nil
}

// No forced output interface: that would constrain the lookup instead of
// observing the kernel's ordinary unmarked local route. This proves only this
// lookup, not all policy-routing selectors, remote service health or a lease.
func observeResourceHostRoute(ctx context.Context, target netip.Addr, own underlayDNSInterface, sources []netip.Prefix, runner CommandRunner) error {
	if !target.IsValid() || target.Is4In6() || target.Zone() != "" || target.IsUnspecified() || target.IsLoopback() || target.IsMulticast() || target.IsLinkLocalUnicast() {
		return errResourceHostObservation
	}
	family := "-6"
	if target.Is4() {
		family = "-4"
	}
	raw, err := runner(ctx, "ip", "-j", "-N", family, "route", "get", target.String(), "mark", "0")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errResourceHostObservation
	}
	return resourceHostRouteObserved(raw, target, own, sources)
}

func resourceHostRouteObserved(raw []byte, target netip.Addr, own underlayDNSInterface, sources []netip.Prefix) error {
	value, err := underlayDNSJSON(raw)
	if err != nil {
		return errResourceHostObservation
	}
	rows, ok := value.([]any)
	if !ok || len(rows) != 1 {
		return errResourceHostObservation
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		return errResourceHostObservation
	}
	dst, _ := row["dst"].(string)
	dev, _ := row["dev"].(string)
	preferred, _ := row["prefsrc"].(string)
	address, err := netip.ParseAddr(dst)
	if err != nil || address != target || dev != own.Name || own.Index <= 0 || !own.Up || own.Loopback {
		return errResourceHostObservation
	}
	source, err := netip.ParseAddr(preferred)
	if err != nil || source.Is4() != target.Is4() || source.IsUnspecified() || !slices.Contains(own.Addresses, source) {
		return errResourceHostObservation
	}
	assigned := false
	for _, prefix := range sources {
		assigned = assigned || prefix.Addr() == source
	}
	if !assigned {
		return errResourceHostObservation
	}
	for key, v := range row {
		switch key {
		case "dst", "dev", "prefsrc":
		case "type":
			if v != "unicast" {
				return errResourceHostObservation
			}
		case "flags", "cache":
			a, ok := v.([]any)
			if !ok || len(a) != 0 {
				return errResourceHostObservation
			}
		case "src", "from":
			text, ok := v.(string)
			a, err := netip.ParseAddr(text)
			if !ok || err != nil || a.Is4() != source.Is4() || (!a.IsUnspecified() && a != source) {
				return errResourceHostObservation
			}
		case "mark":
			text := ""
			switch value := v.(type) {
			case json.Number:
				text = value.String()
			case string:
				text = value
			}
			number, err := strconv.ParseUint(text, 0, 32)
			if err != nil || number != 0 {
				return errResourceHostObservation
			}
		case "table", "protocol", "scope", "metric", "pref", "uid":
		default:
			return errResourceHostObservation
		}
	}
	return nil
}
