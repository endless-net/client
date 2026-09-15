package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDisabledExitFamilyRequiresRouteAndRuleAbsence(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		for stage := 0; stage < 3; stage++ {
			for _, outcome := range []string{"absent", "remaining", "failed", "malformed", "cancelled"} {
				t.Run(fmt.Sprintf("%s/%d/%s", family, stage, outcome), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					queries := [][]string{
						{"-j", "-N", family, "route", "show", "table", "all"},
						{family, "-j", "-N", "rule", "show", "table", "51999"},
						{family, "-j", "-N", "rule", "show", "table", "254"},
					}
					calls := 0
					guard, err := newLinuxExitGuard("exit0", 51999, func(_ context.Context, input, command string, args ...string) ([]byte, error) {
						if calls >= len(queries) || input != "" || command != "ip" || !reflect.DeepEqual(args, queries[calls]) {
							t.Fatal("invalid observation query", args)
						}
						current := calls
						calls++
						if current == stage {
							switch outcome {
							case "failed":
								return nil, errors.New("synthetic-private-command-error")
							case "malformed":
								return []byte(`null`), nil
							case "cancelled":
								cancel()
							case "remaining":
								if stage == 0 {
									return []byte(`[{"dst":"default","table":"51999"}]`), nil
								}
								return exitAppliedRuleFixture(args[len(args)-1]), nil
							}
						}
						if current == 0 {
							return []byte(`[{"dst":"default"},{"dst":"127.0.0.0/8","table":"255"}]`), nil
						}
						if current == 2 {
							return []byte(`[{"priority":32766,"table":"254"}]`), nil
						}
						return []byte(`[]`), nil
					})
					if err != nil {
						t.Fatal(err)
					}
					err = confirmExitFamilyAbsent(ctx, guard, family)
					wantCalls := stage + 1
					if outcome == "absent" {
						wantCalls = 3
					}
					if calls != wantCalls || (err == nil) != (outcome == "absent") {
						t.Fatal(calls, err)
					}
					if err != nil && strings.Contains(err.Error(), "private") {
						t.Fatal("native output disclosed")
					}
				})
			}
		}
	}
}
