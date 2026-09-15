package client

import (
	"context"
	"errors"
	"fmt"
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

func TestExitPolicyCleanupRequiresEveryRuleObservation(t *testing.T) {
	for target := 1; target <= 4; target++ {
		for _, outcome := range []string{"clear", "remaining", "failed", "malformed", "cancelled"} {
			t.Run(fmt.Sprintf("%d/%s", target, outcome), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				absent, err := exitPolicyRulesAbsent(ctx, 51820, func(call context.Context, name string, args ...string) ([]byte, error) {
					calls++
					family, table := "-4", "51820"
					if calls > 2 {
						family = "-6"
					}
					if calls%2 == 0 {
						table = "254"
					}
					if name != "ip" || !reflect.DeepEqual(args, []string{family, "-j", "-N", "rule", "show", "table", table}) {
						t.Fatal("incorrect rule observation scope", args)
					}
					if _, ok := call.Deadline(); !ok {
						t.Fatal("unbounded rule observation")
					}
					if calls == target {
						switch outcome {
						case "remaining":
							return []byte(fmt.Sprintf(`[{"priority":32764,"table":%q,"suppress_prefixlen":0}]`, table)), nil
						case "failed":
							return nil, errors.New("synthetic-private-error")
						case "malformed":
							return []byte(`null`), nil
						case "cancelled":
							cancel()
						}
					}
					if table == "254" {
						return []byte(`[{"priority":32766,"table":"254"}]`), nil
					}
					return []byte(`[]`), nil
				})
				if outcome == "clear" {
					if !absent || err != nil || calls != 4 {
						t.Fatal("clear rules not confirmed", absent, err, calls)
					}
				} else if outcome == "remaining" {
					if absent || err != nil || calls != 4 {
						t.Fatal("remaining rules misclassified", absent, err, calls)
					}
				} else if absent || err == nil || strings.Contains(err.Error(), "private") || calls != target {
					t.Fatal("failed observation permitted cleanup", absent, err, calls)
				}
			})
		}
	}
}

func TestExitRouteObservationRejectsAmbiguousOutput(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`, `[null]`, `[{}]`, `[{"dst":"default","table":"custom"}]`, `[{"dst":"default","table":51820}]`, `[{"dst":"default","table":null}]`, `[{"dst":"default","table":""}]`, `[] []`, strings.Repeat(" ", (1<<20)+1)} {
		if absent, err := exitRouteTableAbsent([]byte(raw), 51820); absent || err == nil {
			t.Fatal("invalid route output proved cleanup")
		}
	}
}

func TestExitRouteObservationDistinguishesOmittedMainTable(t *testing.T) {
	for _, raw := range []string{`[{"dst":"default"}]`, `[{"dst":"default","table":"254"}]`} {
		if absent, err := exitRouteTableAbsent([]byte(raw), 254); err != nil || absent {
			t.Fatal("main route was not observed", err)
		}
		if absent, err := exitRouteTableAbsent([]byte(raw), 51820); err != nil || !absent {
			t.Fatal("main route was assigned to the dedicated table", err)
		}
	}
}
