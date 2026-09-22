package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"strconv"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitLANRouteProtocol = "186"
const exitLANRouteMetric = "42760"
const exitLANRulePriority = "10000"

type exitLANOwnedRoute struct {
	Prefix    string `json:"prefix"`
	Interface string `json:"interface"`
}
type exitLANRoutingOwnership struct {
	Family api.ExitFamilyMode  `json:"family"`
	Table  uint32              `json:"table"`
	Mark   uint32              `json:"mark"`
	Routes []exitLANOwnedRoute `json:"routes"`
}

func exitLANRoutingPlan(guard *linuxExitGuard, plan *exitLANPlan) (*exitLANRoutingOwnership, error) {
	if guard == nil || plan == nil {
		return nil, errExitLANPolicy
	}
	if plan.topology == nil {
		return nil, errExitLANPolicy
	}
	r := &exitLANRoutingOwnership{Family: plan.topology.Family, Table: guard.mark ^ 0x40000000, Mark: guard.mark ^ 0x80000000}
	for _, binding := range plan.bindings {
		for _, prefix := range binding.destinations {
			r.Routes = append(r.Routes, exitLANOwnedRoute{prefix.String(), binding.linkName})
		}
	}
	if err := validateExitLANRouting(r); err != nil {
		return nil, err
	}
	return r, nil
}

func validateExitLANRouting(r *exitLANRoutingOwnership) error {
	if r == nil || !exitGuardFamilyValid(r.Family) || !validExitPolicyTable(r.Table) || r.Mark == 0 || len(r.Routes) == 0 || len(r.Routes) > exitLANPrefixLimit {
		return errExitLANPolicy
	}
	seen := map[string]bool{}
	for _, route := range r.Routes {
		p, err := netip.ParsePrefix(route.Prefix)
		if err != nil || p != p.Masked() || p.Bits() == 0 || p.Addr().Is4In6() || !exitLANInterfaceName(route.Interface) || route.Interface == "lo" || seen[route.Prefix] {
			return errExitLANPolicy
		}
		if p.Addr().Is4() && r.Family == api.ExitFamilyIPv6Only || p.Addr().Is6() && r.Family == api.ExitFamilyIPv4Only {
			return errExitLANPolicy
		}
		seen[route.Prefix] = true
	}
	return nil
}

func (r *exitLANRoutingOwnership) families() []string {
	switch r.Family {
	case api.ExitFamilyIPv4Only:
		return []string{"-4"}
	case api.ExitFamilyIPv6Only:
		return []string{"-6"}
	default:
		return []string{"-4", "-6"}
	}
}

func (r *exitLANRoutingOwnership) routeCommands(family string) [][]string {
	table := strconv.FormatUint(uint64(r.Table), 10)
	result := [][]string{{"prohibit", "default", "table", table, "proto", exitLANRouteProtocol, "metric", exitLANRouteMetric}}
	for _, route := range r.Routes {
		p := netip.MustParsePrefix(route.Prefix)
		if (family == "-4") != p.Addr().Is4() {
			continue
		}
		cmd := []string{"unicast", route.Prefix, "dev", route.Interface, "table", table, "proto", exitLANRouteProtocol, "metric", exitLANRouteMetric}
		if family == "-4" {
			cmd = append(cmd, "scope", "link")
		}
		result = append(result, cmd)
	}
	return result
}

// Returns the exact owned tuples present. Any unrecognized row in the exclusive
// table rejects both installation and cleanup; tables are never blindly flushed.
func (r *exitLANRoutingOwnership) routesPresent(ctx context.Context, g *linuxExitGuard, family string) ([]bool, error) {
	raw, err := g.run(ctx, "", "ip", family, "-j", "-N", "route", "show", "table", strconv.FormatUint(uint64(r.Table), 10))
	if err != nil || ctx.Err() != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	value, err := underlayDNSJSON(raw)
	rows, ok := value.([]any)
	if err != nil || !ok {
		return nil, errExitLANPolicy
	}
	commands := r.routeCommands(family)
	present := make([]bool, len(commands))
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, errExitLANPolicy
		}
		if flags, ok := row["flags"]; ok {
			if !reflect.DeepEqual(flags, []any{}) {
				return nil, errExitLANPolicy
			}
			delete(row, "flags")
		}
		if typ, ok := row["type"]; ok && typ == "unicast" {
			delete(row, "type")
		}
		if family == "-6" {
			if pref, ok := row["pref"]; ok {
				if pref != "medium" {
					return nil, errExitLANPolicy
				}
				delete(row, "pref")
			}
			if scope, ok := row["scope"]; ok && scope == "0" {
				delete(row, "scope")
			}
		}
		if table, ok := row["table"]; ok {
			if table != json.Number(strconv.FormatUint(uint64(r.Table), 10)) && table != strconv.FormatUint(uint64(r.Table), 10) {
				return nil, errExitLANPolicy
			}
			delete(row, "table")
		}
		found := -1
		for i, cmd := range commands {
			want := map[string]any{"dst": cmd[1], "protocol": exitLANRouteProtocol, "metric": json.Number(exitLANRouteMetric)}
			if i == 0 {
				want["type"] = "prohibit"
			} else {
				want["dev"] = cmd[3]
				if family == "-4" {
					want["scope"] = "253"
				}
			}
			if reflect.DeepEqual(row, want) {
				found = i
				break
			}
		}
		if found < 0 || present[found] {
			return nil, errExitLANPolicy
		}
		present[found] = true
	}
	return present, ctx.Err()
}

func (r *exitLANRoutingOwnership) ruleArgs() []string {
	return []string{"priority", exitLANRulePriority, "fwmark", strconv.FormatUint(uint64(r.Mark), 10) + "/4294967295", "table", strconv.FormatUint(uint64(r.Table), 10), "protocol", exitLANRouteProtocol}
}

func (r *exitLANRoutingOwnership) rulePresent(ctx context.Context, g *linuxExitGuard, family string) (bool, error) {
	raw, err := g.run(ctx, "", "ip", family, "-j", "-N", "rule", "show", "table", strconv.FormatUint(uint64(r.Table), 10))
	if err != nil || ctx.Err() != nil {
		return false, errors.Join(err, ctx.Err())
	}
	rows, err := decodeLinuxPolicyRules(raw)
	if err != nil || len(rows) > 1 {
		return false, errExitLANPolicy
	}
	if len(rows) == 0 {
		return false, nil
	}
	row := rows[0]
	var table, src, mark string
	var priority uint32
	if json.Unmarshal(row["table"], &table) != nil || json.Unmarshal(row["src"], &src) != nil || json.Unmarshal(row["fwmark"], &mark) != nil || json.Unmarshal(row["priority"], &priority) != nil {
		return false, errExitLANPolicy
	}
	actualMark, markErr := strconv.ParseUint(mark, 0, 32)
	if mask, ok := row["fwmask"]; ok {
		var text string
		if json.Unmarshal(mask, &text) != nil {
			return false, errExitLANPolicy
		}
		number, err := strconv.ParseUint(text, 0, 32)
		if err != nil || number != 0xffffffff {
			return false, errExitLANPolicy
		}
		delete(row, "fwmask")
	}
	if len(row) != 5 || string(row["protocol"]) != `"186"` || table != strconv.FormatUint(uint64(r.Table), 10) || src != "all" || priority != 10000 || markErr != nil || actualMark != uint64(r.Mark) {
		return false, errExitLANPolicy
	}
	return true, ctx.Err()
}

func applyExitLANRouting(ctx context.Context, g *linuxExitGuard, r *exitLANRoutingOwnership) error {
	if validateExitLANRouting(r) != nil || g == nil {
		return errExitLANPolicy
	}
	for _, family := range r.families() {
		rows, err := r.routesPresent(ctx, g, family)
		if err != nil {
			return err
		}
		for _, present := range rows {
			if present {
				return errExitLANPolicy
			}
		}
		present, err := r.rulePresent(ctx, g, family)
		if err != nil {
			return err
		}
		if present {
			return errExitLANPolicy
		}
	}
	for _, family := range r.families() {
		for _, cmd := range r.routeCommands(family) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := g.run(ctx, "", "ip", append([]string{family, "route", "add"}, cmd...)...); err != nil {
				return err
			}
		}
		if _, err := g.run(ctx, "", "ip", append([]string{family, "rule", "add"}, r.ruleArgs()...)...); err != nil {
			return err
		}
	}
	return observeExitLANRouting(ctx, g, r)
}

func observeExitLANRouting(ctx context.Context, g *linuxExitGuard, r *exitLANRoutingOwnership) error {
	if validateExitLANRouting(r) != nil || g == nil {
		return errExitLANPolicy
	}
	for _, family := range r.families() {
		rows, err := r.routesPresent(ctx, g, family)
		if err != nil {
			return err
		}
		for _, present := range rows {
			if !present {
				return errExitLANPolicy
			}
		}
		present, err := r.rulePresent(ctx, g, family)
		if err != nil {
			return err
		}
		if !present {
			return errExitLANPolicy
		}
	}
	return ctx.Err()
}

func cleanupExitLANRouting(ctx context.Context, g *linuxExitGuard, r *exitLANRoutingOwnership) error {
	if r == nil {
		return nil
	}
	if validateExitLANRouting(r) != nil || g == nil {
		return errExitLANPolicy
	}
	for _, family := range r.families() {
		if _, err := r.routesPresent(ctx, g, family); err != nil {
			return err
		}
		if _, err := r.rulePresent(ctx, g, family); err != nil {
			return err
		}
	}
	for _, family := range r.families() {
		present, err := r.rulePresent(ctx, g, family)
		if err != nil {
			return err
		}
		if present {
			_, _ = g.run(ctx, "", "ip", append([]string{family, "rule", "del"}, r.ruleArgs()...)...)
			present, err = r.rulePresent(ctx, g, family)
			if err != nil {
				return err
			}
			if present {
				return errExitLANPolicy
			}
		}
		commands := r.routeCommands(family)
		for i := len(commands) - 1; i >= 0; i-- {
			rows, err := r.routesPresent(ctx, g, family)
			if err != nil {
				return err
			}
			if !rows[i] {
				continue
			}
			_, _ = g.run(ctx, "", "ip", append([]string{family, "route", "del"}, commands[i]...)...)
			rows, err = r.routesPresent(ctx, g, family)
			if err != nil {
				return err
			}
			if rows[i] {
				return errExitLANPolicy
			}
		}
	}
	return ctx.Err()
}
