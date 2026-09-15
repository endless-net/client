package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func exitAppliedRuleFixture(table string) []byte {
	if table == "254" {
		return []byte(`[{"priority":32764,"src":"all","table":"254","suppress_prefixlen":0},{"priority":32766,"src":"all","table":"254"}]`)
	}
	return []byte(fmt.Sprintf(`[{"priority":32765,"src":"all","table":%q,"not":null,"fwmark":%q}]`, table, table))
}

func TestExitPolicyAppliedRequiresBothSelectorsAndOrder(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		for _, outcome := range []string{"present", "missing", "error", "cancelled", "order", "main_before", "main_equal"} {
			t.Run(family+"/"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				guard, err := newLinuxExitGuard("exit0", 51999, func(_ context.Context, input, command string, args ...string) ([]byte, error) {
					table := "51999"
					if calls == 1 {
						table = "254"
					}
					if input != "" || command != "ip" || !reflect.DeepEqual(args, []string{family, "-j", "-N", "rule", "show", "table", table}) {
						t.Fatal("incorrect query", args)
					}
					calls++
					if calls == 2 {
						switch outcome {
						case "missing":
							return []byte(`[]`), nil
						case "error":
							return nil, errors.New("private native output")
						case "cancelled":
							cancel()
						case "order":
							return []byte(strings.ReplaceAll(string(exitAppliedRuleFixture(table)), "32764", "32765")), nil
						case "main_before":
							return []byte(strings.ReplaceAll(string(exitAppliedRuleFixture(table)), "32766", "100")), nil
						case "main_equal":
							return []byte(strings.ReplaceAll(string(exitAppliedRuleFixture(table)), "32766", "32765")), nil
						}
					}
					return exitAppliedRuleFixture(table), nil
				})
				if err != nil {
					t.Fatal(err)
				}
				err = confirmExitPolicyRules(ctx, guard, family)
				if calls != 2 || (err == nil) != (outcome == "present") {
					t.Fatal(calls, err)
				}
				if err != nil && strings.Contains(err.Error(), "private") {
					t.Fatal("native output disclosed")
				}
			})
		}
	}
}

func TestExitPolicyAppliedRejectsChangedSelectors(t *testing.T) {
	valid := string(exitAppliedRuleFixture("51999"))
	for _, raw := range []string{
		`[]`, `null`, `[null]`,
		strings.ReplaceAll(valid, `"not":null,`, ""),
		strings.ReplaceAll(valid, `"not":null`, `"not":false`),
		strings.ReplaceAll(valid, `"fwmark":"51999"`, `"fwmark":"51820"`),
		strings.ReplaceAll(valid, `"src":"all"`, `"src":"192.0.2.1"`),
		strings.ReplaceAll(valid, `"priority":32765`, `"priority":null`),
		strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","fwmask":"0xff"`),
		strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","iif":"eth0"`),
		strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","goto":100`),
		"[" + valid[1:len(valid)-1] + "," + valid[1:len(valid)-1] + "]",
	} {
		if _, ok := exitPolicyRuleObserved([]byte(raw), 51999, false, 0); ok {
			t.Fatal("accepted changed selector", raw)
		}
	}
	for _, raw := range []string{valid, strings.ReplaceAll(valid, `"fwmark":"51999"`, `"fwmark":"0xcb1f"`)} {
		if _, ok := exitPolicyRuleObserved([]byte(raw), 51999, false, 0); !ok {
			t.Fatal("rejected complete mark selector", raw)
		}
	}
}

func TestExitPolicySuppressionRejectsChangedOrDuplicateRules(t *testing.T) {
	valid := string(exitAppliedRuleFixture("254"))
	for _, raw := range []string{
		strings.ReplaceAll(valid, `"suppress_prefixlen":0`, `"suppress_prefixlen":null`),
		strings.ReplaceAll(valid, `"suppress_prefixlen":0`, `"suppress_prefixlen":1`),
		strings.ReplaceAll(valid, `"suppress_prefixlen":0`, `"suppress_prefixlen":0,"not":null`),
		strings.ReplaceAll(valid, `"suppress_prefixlen":0`, `"suppress_prefixlen":0,"fwmark":"0xcb1f"`),
		strings.ReplaceAll(valid, `"suppress_prefixlen":0`, `"suppress_prefixlen":0,"dst":"192.0.2.0","dstlen":24`),
		strings.ReplaceAll(valid, `"priority":32766,"src":"all","table":"254"`, `"priority":32763,"src":"all","table":"254","suppress_prefixlen":0`),
	} {
		if _, ok := exitPolicyRuleObserved([]byte(raw), 51999, true, 32765); ok {
			t.Fatal("accepted changed suppression", raw)
		}
	}
}
