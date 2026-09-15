package client

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Confirm the direct default routes installed by the Linux router. This is not
// firewall, policy-rule, peer-health or end-to-end exit evidence.
func confirmExitDefaultRoutes(ctx context.Context, guard *linuxExitGuard, family api.ExitFamilyMode) error {
	families := []string{"-4", "-6"}
	switch family {
	case api.ExitFamilyIPv4Only:
		families = families[:1]
	case api.ExitFamilyIPv6Only:
		families = families[1:]
	case api.ExitFamilyDualStack:
	default:
		return errors.New("invalid exit route family")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, ipFamily := range families {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := guard.run(ctx, "", "ip", "-j", "-N", ipFamily, "route", "show", "table", strconv.FormatUint(uint64(guard.mark), 10), "exact", "default")
		if err != nil || !exitDirectDefaultObserved(raw, guard.interfaceName) {
			return errors.New("exit default route is not confirmed")
		}
		if err := confirmExitPolicyRules(ctx, guard, ipFamily); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func exitDirectDefaultObserved(raw []byte, name string) bool {
	if len(raw) > 1<<20 {
		return false
	}
	var routes []map[string]json.RawMessage
	if json.Unmarshal(raw, &routes) != nil || len(routes) != 1 {
		return false
	}
	route := routes[0]
	var destination, device string
	if json.Unmarshal(route["dst"], &destination) != nil || destination != "default" || json.Unmarshal(route["dev"], &device) != nil || device != name {
		return false
	}
	for key, value := range route {
		switch key {
		case "dst", "dev", "protocol", "scope", "prefsrc", "metric", "pref":
		case "flags":
			var flags []string
			if json.Unmarshal(value, &flags) != nil || flags == nil || len(flags) != 0 {
				return false
			}
		case "type":
			var kind string
			if json.Unmarshal(value, &kind) != nil || kind != "unicast" {
				return false
			}
		default:
			// Gateway, multipath, nexthop IDs and encapsulation are not the
			// direct dev route installed by this adapter.
			return false
		}
	}
	return true
}
