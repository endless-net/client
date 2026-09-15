package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExitRouteCleanupRequiresBothFamilyObservations(t *testing.T) {
	for _, scenario := range []string{"empty", "ipv4", "ipv6", "failed", "malformed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			empty, err := exitRouteTablesEmpty(ctx, 51820, func(call context.Context, name string, args ...string) ([]byte, error) {
				calls++
				family := "-4"
				if calls == 2 {
					family = "-6"
				}
				if name != "ip" || !reflect.DeepEqual(args, []string{"-j", "-N", family, "route", "show", "table", "all"}) {
					t.Fatal("unscoped route query")
				}
				if _, ok := call.Deadline(); !ok {
					t.Fatal("unbounded route query")
				}
				if calls == 2 {
					switch scenario {
					case "failed":
						return nil, errors.New("synthetic-private-native-error")
					case "malformed":
						return []byte(`null`), nil
					case "cancelled":
						cancel()
					}
				}
				if (scenario == "ipv4" && calls == 1) || (scenario == "ipv6" && calls == 2) {
					return []byte(`[{"dst":"default","dev":"endlessnet","table":"51820"}]`), nil
				}
				return []byte(`[{"dst":"default","dev":"eth0"},{"dst":"127.0.0.0/8","table":"255"}]`), nil
			})
			if calls != 2 {
				t.Fatalf("family queries=%d", calls)
			}
			if scenario == "empty" {
				if !empty || err != nil {
					t.Fatalf("empty=%v error=%v", empty, err)
				}
			} else if scenario == "ipv4" || scenario == "ipv6" {
				if empty || err != nil {
					t.Fatalf("remaining routes: empty=%v error=%v", empty, err)
				}
			} else if empty || err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("invalid observation: empty=%v error=%v", empty, err)
			}
		})
	}
}

func TestExitRouteObservationRejectsAmbiguousOutput(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`, `[null]`, `[{}]`, `[{"dst":"default","table":"custom"}]`, `[{"dst":"default","table":51820}]`, `[] []`, strings.Repeat(" ", (1<<20)+1)} {
		if absent, err := exitRouteTableAbsent([]byte(raw), 51820); absent || err == nil {
			t.Fatal("invalid route output proved cleanup")
		}
	}
}
