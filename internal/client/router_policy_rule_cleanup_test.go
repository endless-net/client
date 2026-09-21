package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func ownedPolicyRuleFixture(mark uint32, suppress bool) []byte {
	table, priority, extra := mark, 32765, ""
	if suppress {
		table = 254
		priority = 32764
		extra = `,"suppress_prefixlen":0`
	}
	return []byte(fmt.Sprintf(`[{"priority":%d,"src":"all","table":"%d","protocol":"0","not":null,"fwmark":"0x%x"%s}]`, priority, table, mark, extra))
}

func TestPolicyRuleCleanupRequiresObservedAbsence(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		for _, suppress := range []bool{false, true} {
			for _, scenario := range []string{"removed", "already_absent", "delete_error_but_absent", "remaining", "duplicate", "observation_failed", "malformed", "cancelled"} {
				t.Run(family+"/"+fmt.Sprint(suppress)+"/"+scenario, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					var calls [][]string
					fixture := ownedPolicyRuleFixture(51820, suppress)
					err := removeLinuxPolicyRule(ctx, func(_ context.Context, name string, args ...string) ([]byte, error) {
						if name != "ip" {
							t.Fatal(name)
						}
						calls = append(calls, append([]string(nil), args...))
						switch len(calls) {
						case 1:
							switch scenario {
							case "already_absent":
								return []byte(`[]`), nil
							case "malformed":
								return []byte(`null`), nil
							case "duplicate":
								return []byte("[" + string(fixture[1:len(fixture)-1]) + "," + string(fixture[1:len(fixture)-1]) + "]"), nil
							}
							return fixture, nil
						case 2:
							if scenario == "cancelled" {
								cancel()
							}
							if scenario == "delete_error_but_absent" {
								return []byte("private native detail"), errors.New("delete failed")
							}
							return nil, nil
						default:
							if scenario == "remaining" {
								return fixture, nil
							}
							if scenario == "observation_failed" {
								return nil, errors.New("private native detail")
							}
							return []byte(`[]`), nil
						}
					}, family, 51820, suppress)
					wantOK := scenario == "removed" || scenario == "already_absent" || scenario == "delete_error_but_absent"
					if (err == nil) != wantOK {
						t.Fatal("incorrect observed cleanup result", err)
					}
					table, priority := "51820", "32765"
					if suppress {
						table, priority = "254", "32764"
					}
					query := []string{family, "-j", "-N", "-d", "rule", "show", "table", table}
					if !reflect.DeepEqual(calls[0], query) {
						t.Fatal("pre-observation scope", calls)
					}
					if scenario == "already_absent" || scenario == "malformed" || scenario == "duplicate" {
						if len(calls) != 1 {
							t.Fatal("unowned or ambiguous rule was mutated", calls)
						}
						return
					}
					deletion := []string{family, "rule", "del", "pref", priority, "not", "fwmark", "51820/0xffffffff", "table", table}
					if suppress {
						deletion = append(deletion, "suppress_prefixlength", "0")
					}
					if !reflect.DeepEqual(calls[1], deletion) {
						t.Fatal("delete selector widened", calls)
					}
					if scenario == "cancelled" {
						if !errors.Is(err, context.Canceled) || len(calls) != 2 {
							t.Fatal("cancelled cleanup continued", err, calls)
						}
					} else if len(calls) != 3 || !reflect.DeepEqual(calls[2], query) {
						t.Fatal("post-observation scope", calls)
					}
				})
			}
		}
	}
}

func TestPolicyRuleCleanupRejectsAmbiguousOwnershipBeforeDelete(t *testing.T) {
	for _, suppress := range []bool{false, true} {
		valid := string(ownedPolicyRuleFixture(51820, suppress))
		for _, raw := range []string{
			strings.ReplaceAll(valid, `,"not":null`, ""),
			strings.ReplaceAll(valid, `"not":null`, `"not":false`),
			strings.ReplaceAll(valid, `"protocol":"0"`, `"protocol":"2"`),
			strings.ReplaceAll(valid, `,"protocol":"0"`, ``),
			strings.ReplaceAll(valid, `"src":"all"`, `"src":"192.0.2.1"`),
			strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","iif":"eth0"`),
			strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","fwmask":"0xff"`),
			strings.ReplaceAll(valid, `"src":"all"`, `"src":"all","src":"all"`),
			"[" + valid[1:len(valid)-1] + "," + strings.ReplaceAll(valid[1:len(valid)-1], `"fwmark":"0xca6c"`, `"fwmark":"0xca6d"`) + "]",
		} {
			calls := 0
			err := removeLinuxPolicyRule(t.Context(), func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte(raw), nil }, "-4", 51820, suppress)
			if err == nil || calls != 1 {
				t.Fatal("ambiguous scope allowed mutation", raw, err, calls)
			}
		}
	}
}

func TestPolicyRuleObservationRejectsUnknownAndBoundedOutput(t *testing.T) {
	for _, output := range []string{"", "null", "{}", "[null]", "[{}]", `[{"priority":null,"table":"254"}]`, `[{"priority":1,"table":"254","suppress_prefixlen":null}]`, `[{"priority":1,"table":"254","suppress_prefixlen":"0"}]`, strings.Repeat(" ", (1<<20)+1), "[" + strings.Repeat(`{"priority":1,"table":"254"},`, 4096) + `{"priority":1,"table":"254"}]`} {
		if absent, err := linuxPolicyRuleAbsent([]byte(output), 51820, true); err == nil || absent {
			t.Fatal("invalid observation established absence")
		}
	}
	for _, output := range []string{`[]`, `[{"priority":32766,"table":"254"}]`, `[{"priority":32764,"table":"254","suppress_prefixlen":0}]`, `[{"priority":32764,"table":"254","suppress_prefixlen":0,"not":null,"fwmark":"0xca6d"}]`} {
		if absent, err := linuxPolicyRuleAbsent([]byte(output), 51820, true); err != nil || !absent {
			t.Fatal("foreign rule blocked absence", err)
		}
		calls := 0
		if err := removeLinuxPolicyRule(t.Context(), func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte(output), nil }, "-4", 51820, true); err != nil || calls != 1 {
			t.Fatal("foreign suppression was deleted", err, calls)
		}
	}
}

func TestPolicyRuleCleanupRejectsReservedTables(t *testing.T) {
	for _, table := range []uint32{0, 253, 254, 255} {
		calls := 0
		if err := removeLinuxPolicyRule(t.Context(), func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }, "-4", table, true); err == nil || calls != 0 {
			t.Fatal("reserved table reached native runner", table)
		}
	}
}
