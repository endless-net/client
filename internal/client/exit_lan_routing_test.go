package client

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANRouteTestKernel struct {
	t            *testing.T
	routes       map[string]map[string]map[string]any
	rules        map[string]map[string]any
	writes, fail int
	foreign      bool
}

func newExitLANRouteTestKernel(t *testing.T) *exitLANRouteTestKernel {
	return &exitLANRouteTestKernel{t: t, routes: map[string]map[string]map[string]any{"-4": {}, "-6": {}}, rules: map[string]map[string]any{}}
}

func (k *exitLANRouteTestKernel) run(_ context.Context, input, name string, args ...string) ([]byte, error) {
	if input != "" || name != "ip" || len(args) < 4 {
		k.t.Fatal("unexpected routing command", name, args)
	}
	family := args[0]
	if family != "-4" && family != "-6" {
		k.t.Fatal("unbound routing family", args)
	}
	kindAt := slices.Index(args, "route")
	kind := "route"
	if kindAt < 0 {
		kindAt = slices.Index(args, "rule")
		kind = "rule"
	}
	if kindAt < 0 {
		k.t.Fatal("not a route/rule command")
	}
	verb := args[kindAt+1]
	if verb == "show" {
		rows := []any{}
		if kind == "route" {
			for _, row := range k.routes[family] {
				rows = append(rows, row)
			}
		} else if row := k.rules[family]; row != nil {
			rows = append(rows, row)
		}
		if k.foreign && kind == "route" {
			rows = append(rows, map[string]any{"dst": "203.0.113.0/24", "dev": "foreign0", "protocol": "4"})
		}
		return json.Marshal(rows)
	}
	if verb != "add" && verb != "del" {
		k.t.Fatal("nonexclusive route mutation", args)
	}
	k.writes++
	if k.writes == k.fail {
		return nil, errors.New("injected route write failure")
	}
	parts := args[kindAt+2:]
	if kind == "route" {
		row := map[string]any{"dst": parts[1], "flags": []any{}}
		if parts[0] != "unicast" {
			row["type"] = parts[0]
		}
		for i := 2; i < len(parts); i += 2 {
			key, value := parts[i], parts[i+1]
			switch key {
			case "dev":
				row[key] = value
			case "proto":
				row["protocol"] = value
			case "metric":
				v, _ := strconv.Atoi(value)
				row[key] = v
			case "scope":
				if value != "link" {
					k.t.Fatal("unexpected scope")
				}
				row[key] = "253"
			case "table":
			default:
				k.t.Fatal("unexpected route selector", key)
			}
		}
		if family == "-6" {
			row["pref"] = "medium"
		}
		if verb == "add" {
			if k.routes[family][parts[1]] != nil {
				return nil, errors.New("exists")
			}
			k.routes[family][parts[1]] = row
		} else {
			if !reflect.DeepEqual(k.routes[family][parts[1]], row) {
				k.t.Fatal("deletion changed owned tuple")
			}
			delete(k.routes[family], parts[1])
		}
	} else {
		row := map[string]any{"src": "all"}
		for i := 0; i < len(parts); i += 2 {
			key, value := parts[i], parts[i+1]
			switch key {
			case "priority":
				v, _ := strconv.Atoi(value)
				row[key] = v
			case "fwmark":
				slash := slices.Index([]byte(value), byte('/'))
				if slash < 0 {
					k.t.Fatal("missing exact mark mask")
				}
				row[key] = value[:slash]
			case "table", "protocol":
				row[key] = value
			default:
				k.t.Fatal("unexpected rule selector")
			}
		}
		if verb == "add" {
			if k.rules[family] != nil {
				return nil, errors.New("exists")
			}
			k.rules[family] = row
		} else {
			if !reflect.DeepEqual(k.rules[family], row) {
				k.t.Fatal("changed rule deletion")
			}
			delete(k.rules, family)
		}
	}
	return nil, nil
}

func TestExitLANRoutingOwnsTerminalTableAndExactCleanup(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		t.Run(string(family), func(t *testing.T) {
			r := &exitLANRoutingOwnership{Family: family, Table: 1073793644, Mark: 2147535468}
			if family != api.ExitFamilyIPv6Only {
				r.Routes = append(r.Routes, exitLANOwnedRoute{"192.0.2.0/24", "eth0"})
			}
			if family != api.ExitFamilyIPv4Only {
				r.Routes = append(r.Routes, exitLANOwnedRoute{"2001:db8:1::/64", "wlan0"})
			}
			k := newExitLANRouteTestKernel(t)
			g := &linuxExitGuard{run: k.run}
			if err := applyExitLANRouting(t.Context(), g, r); err != nil {
				t.Fatal(err)
			}
			for _, f := range r.families() {
				if k.routes[f]["default"]["type"] != "prohibit" || k.rules[f] == nil {
					t.Fatal("missing terminal route or mark lookup")
				}
				for _, row := range k.routes[f] {
					if row["gateway"] != nil {
						t.Fatal("LAN acquired a gateway")
					}
				}
			}
			if family == api.ExitFamilyIPv4Only && len(k.routes["-6"]) != 0 || family == api.ExitFamilyIPv6Only && len(k.routes["-4"]) != 0 {
				t.Fatal("unselected family modified")
			}
			writes := k.writes
			k.foreign = true
			if err := cleanupExitLANRouting(t.Context(), g, r); err == nil || k.writes != writes {
				t.Fatal("foreign route granted deletion")
			}
			k.foreign = false
			if err := cleanupExitLANRouting(t.Context(), g, r); err != nil {
				t.Fatal(err)
			}
			if len(k.routes["-4"])+len(k.routes["-6"])+len(k.rules) != 0 {
				t.Fatal("owned artifacts survived cleanup")
			}
			if err := cleanupExitLANRouting(t.Context(), g, r); err != nil {
				t.Fatal("absent retry failed", err)
			}
		})
	}
}

func TestExitLANRoutingRecoversEveryPartialInstall(t *testing.T) {
	r := &exitLANRoutingOwnership{Family: api.ExitFamilyDualStack, Table: 1073793644, Mark: 2147535468, Routes: []exitLANOwnedRoute{{"192.0.2.0/24", "eth0"}, {"2001:db8:1::/64", "wlan0"}}}
	for failure := 1; failure <= 6; failure++ {
		k := newExitLANRouteTestKernel(t)
		k.fail = failure
		g := &linuxExitGuard{run: k.run}
		if err := applyExitLANRouting(t.Context(), g, r); err == nil {
			t.Fatal("partial install succeeded")
		}
		k.fail = 0
		if err := cleanupExitLANRouting(t.Context(), g, r); err != nil {
			t.Fatal("partial install could not recover", failure, err)
		}
	}
}
