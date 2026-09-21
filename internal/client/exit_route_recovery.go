package client

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

var errExitRouteRecovery = errors.New("owned exit default route recovery is unconfirmed")

// Recover only the direct default tuple emitted by the Linux router. The
// caller holds the effect lock, has stopped the runtime and has observed closed
// containment. Neither this helper nor successful deletion releases protection.
// The table/interface tuple is the durable ownership boundary. iproute has no
// compare-and-delete transaction: an external administrator concurrently
// replacing that same tuple cannot be distinguished from its original owner.
func recoverExitOwnedDefaultRoutes(ctx context.Context, guard *linuxExitGuard) error {
	if guard == nil || guard.run == nil || guard.mark == 0 || (guard.mark >= 253 && guard.mark <= 255) || guard.interfaceName == "lo" || strings.TrimSpace(guard.interfaceName) != guard.interfaceName || !safeWireGuardInterfaceName(guard.interfaceName) {
		return errExitRouteRecovery
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	families := []string{"-4", "-6"}
	read := func(family string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		raw, err := guard.run(ctx, "", "ip", "-j", "-N", family, "route", "show", "table", "all")
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil {
			return false, errExitRouteRecovery
		}
		return exitOwnedDefaultRoutePresent(raw, family, guard.mark, guard.interfaceName)
	}
	// A foreign/ambiguous row in either family prevents every mutation.
	for _, family := range families {
		if _, err := read(family); err != nil {
			return err
		}
	}
	for _, family := range families {
		// Recheck the entire scope immediately before each individual deletion.
		present := false
		for _, checkFamily := range families {
			found, err := read(checkFamily)
			if err != nil {
				return err
			}
			if checkFamily == family {
				present = found
			}
		}
		if !present {
			continue
		}
		scope, metric := "253", "0"
		if family == "-6" {
			scope = "0"
			metric = "1024"
		}
		_, removeErr := guard.run(ctx, "", "ip", family, "route", "del", "unicast", "default", "dev", guard.interfaceName, "table", strconv.FormatUint(uint64(guard.mark), 10), "proto", "3", "scope", scope, "metric", metric)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		remains, inspectErr := read(family)
		if inspectErr != nil {
			return inspectErr
		}
		if remains {
			return errExitRouteRecovery
		}
		// An error plus authoritative absence is an idempotent success; stderr
		// alone never proves that the route was already absent.
		_ = removeErr
	}
	for _, family := range families {
		present, err := read(family)
		if err != nil {
			return err
		}
		if present {
			return errExitRouteRecovery
		}
	}
	return ctx.Err()
}

// iproute2's omitted protocol is RTPROT_BOOT (3), omitted table is main
// (254), and omitted scope is universe (0). Its plain direct IPv4 route has
// link scope and metric 0; IPv6 uses universe scope and IP6_RT_PRIO_USER=1024.
// Foreign tables are preserved. Any non-owned entry in the target table stops
// cleanup, rather than granting permission to flush a table or interface.
func exitOwnedDefaultRoutePresent(raw []byte, family string, table uint32, iface string) (bool, error) {
	value, err := underlayDNSJSON(raw)
	if err != nil {
		return false, errExitRouteRecovery
	}
	rows, ok := value.([]any)
	if !ok || len(rows) > 4096 {
		return false, errExitRouteRecovery
	}
	found := false
	for _, entry := range rows {
		row, ok := entry.(map[string]any)
		if !ok {
			return false, errExitRouteRecovery
		}
		dst, ok := row["dst"].(string)
		if !ok || dst == "" {
			return false, errExitRouteRecovery
		}
		id := uint64(254)
		if value, present := row["table"]; present {
			label, ok := value.(string)
			if !ok {
				return false, errExitRouteRecovery
			}
			id, err = strconv.ParseUint(label, 10, 32)
			if err != nil || id == 0 {
				return false, errExitRouteRecovery
			}
		}
		if id != uint64(table) {
			continue
		}
		if found || dst != "default" || row["dev"] != iface {
			return false, errExitRouteRecovery
		}
		found = true
		scope := "0"
		metric := uint64(0)
		for key, value := range row {
			switch key {
			case "dst", "dev", "table":
			case "protocol":
				if value != "3" {
					return false, errExitRouteRecovery
				}
			case "type":
				if value != "unicast" {
					return false, errExitRouteRecovery
				}
			case "scope":
				var ok bool
				scope, ok = value.(string)
				if !ok {
					return false, errExitRouteRecovery
				}
			case "metric":
				number, ok := value.(json.Number)
				if !ok {
					return false, errExitRouteRecovery
				}
				metric, err = strconv.ParseUint(number.String(), 10, 32)
				if err != nil {
					return false, errExitRouteRecovery
				}
			case "pref":
				if family != "-6" || value != "medium" {
					return false, errExitRouteRecovery
				}
			case "flags":
				flags, ok := value.([]any)
				if !ok || len(flags) > 1 {
					return false, errExitRouteRecovery
				}
				for _, flag := range flags {
					if flag != "linkdown" {
						return false, errExitRouteRecovery
					}
				}
			default:
				return false, errExitRouteRecovery
			}
		}
		switch family {
		case "-4":
			if scope != "253" || metric != 0 {
				return false, errExitRouteRecovery
			}
		case "-6":
			if scope != "0" || metric != 1024 {
				return false, errExitRouteRecovery
			}
		default:
			return false, errExitRouteRecovery
		}
	}
	return found, nil
}
