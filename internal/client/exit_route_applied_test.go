package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitDefaultRouteObservationQueriesSelectedFamilies(t *testing.T) {
	for _, tc := range []struct {
		family  api.ExitFamilyMode
		queries []string
	}{
		{api.ExitFamilyIPv4Only, []string{"-4"}},
		{api.ExitFamilyIPv6Only, []string{"-6"}},
		{api.ExitFamilyDualStack, []string{"-4", "-6"}},
	} {
		for _, outcome := range []string{"present", "missing", "error", "cancelled"} {
			t.Run(string(tc.family)+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				guard, err := newLinuxExitGuard("exit0", 51999, func(call context.Context, input, command string, args ...string) ([]byte, error) {
					if len(args) > 3 && args[3] == "rule" {
						return exitAppliedRuleFixture(args[len(args)-1]), nil
					}
					if calls >= len(tc.queries) {
						t.Fatal("unexpected query")
					}
					want := []string{"-j", "-N", tc.queries[calls], "route", "show", "table", "51999", "exact", "default"}
					if command != "ip" || input != "" || !reflect.DeepEqual(args, want) {
						t.Fatalf("query %s %v", command, args)
					}
					if _, ok := call.Deadline(); !ok {
						t.Fatal("unbounded observation")
					}
					calls++
					if calls == len(tc.queries) {
						switch outcome {
						case "missing":
							return []byte(`[]`), nil
						case "error":
							return nil, errors.New("synthetic-private-error")
						case "cancelled":
							cancel()
						}
					}
					return []byte(`[{"dst":"default","dev":"exit0","flags":[]}]`), nil
				})
				if err != nil {
					t.Fatal(err)
				}
				err = confirmExitDefaultRoutes(ctx, guard, tc.family)
				if calls != len(tc.queries) || (err == nil) != (outcome == "present") {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
				if err != nil && strings.Contains(err.Error(), "private") {
					t.Fatal("native error disclosed")
				}
			})
		}
	}
}

func TestExitDefaultRouteObservationRejectsAmbiguousRoutes(t *testing.T) {
	for _, raw := range []string{
		`[]`, `null`, `[null]`, `{}`, `[`,
		`[{"dst":"default","dev":"other"}]`,
		`[{"dst":"10.0.0.0/8","dev":"exit0"}]`,
		`[{"dst":"default","dev":"exit0","flags":["linkdown"]}]`,
		`[{"dst":"default","dev":"exit0","flags":null}]`,
		`[{"dst":"default","dev":"exit0","gateway":"192.0.2.1"}]`,
		`[{"dst":"default","dev":"exit0","nexthops":[]}]`,
		`[{"dst":"default","dev":"exit0","nhid":1}]`,
		`[{"dst":"default","dev":"exit0","type":"blackhole"}]`,
		`[{"dst":"default","dev":"exit0"},{"dst":"default","dev":"exit0"}]`,
		strings.Repeat(" ", (1<<20)+1),
	} {
		if exitDirectDefaultObserved([]byte(raw), "exit0") {
			t.Fatal("accepted invalid route observation")
		}
	}
	for _, raw := range []string{
		`[{"dst":"default","dev":"exit0"}]`,
		`[{"dst":"default","dev":"exit0","type":"unicast","protocol":"boot","scope":"link","metric":1024,"pref":"medium","flags":[]}]`,
	} {
		if !exitDirectDefaultObserved([]byte(raw), "exit0") {
			t.Fatal("rejected direct default route")
		}
	}
}
