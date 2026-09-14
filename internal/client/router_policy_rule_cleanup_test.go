package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPolicyRuleCleanupRequiresObservedAbsence(t *testing.T) {
	for _, family := range []string{"-4", "-6"} {
		for _, suppress := range []bool{false, true} {
			for _, scenario := range []string{"removed", "already_absent", "remaining", "duplicate", "observation_failed", "malformed", "cancelled"} {
				t.Run(family+"/"+fmt.Sprint(suppress)+"/"+scenario, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					var calls [][]string
					failure := errors.New("delete failed")
					err := removeLinuxPolicyRule(ctx, func(_ context.Context, name string, args ...string) ([]byte, error) {
						if name != "ip" {
							t.Fatal(name)
						}
						calls = append(calls, append([]string(nil), args...))
						if len(calls) == 1 {
							if scenario == "cancelled" {
								cancel()
							}
							if scenario != "removed" && scenario != "duplicate" {
								return []byte("untrusted native details"), failure
							}
							return nil, nil
						}
						switch scenario {
						case "remaining", "duplicate":
							return []byte(`[{"priority":32764,"table":"51820","suppress_prefixlen":0}]`), nil
						case "observation_failed":
							return nil, errors.New("dump interrupted")
						case "malformed":
							return []byte("null"), nil
						}
						return []byte("[]"), nil
					}, family, 51820, suppress)
					if scenario == "removed" || scenario == "already_absent" {
						if err != nil {
							t.Fatal(err)
						}
					} else if err == nil {
						t.Fatal("unconfirmed removal became success")
					}
					wantDelete := []string{family, "rule", "del", "not", "fwmark", "51820", "table", "51820"}
					wantQuery := []string{family, "-j", "rule", "show", "table", "51820", "not", "fwmark", "51820"}
					if suppress {
						wantDelete = []string{family, "rule", "del", "table", "main", "suppress_prefixlength", "0"}
						wantQuery = []string{family, "-j", "rule", "show", "table", "main"}
					}
					if !reflect.DeepEqual(calls[0], wantDelete) {
						t.Fatal("removal selector widened", calls)
					}
					if scenario == "cancelled" {
						if !errors.Is(err, context.Canceled) || len(calls) != 1 {
							t.Fatal("cancelled cleanup continued", err, calls)
						}
					} else if len(calls) != 2 || !reflect.DeepEqual(calls[1], wantQuery) {
						t.Fatal("observation selector differs", calls)
					}
				})
			}
		}
	}
}

func TestPolicyRuleObservationRejectsUnknownAndBoundedOutput(t *testing.T) {
	for _, output := range []string{"", "null", "{}", "[null]", "[{}]", `[{"priority":null,"table":"main"}]`, `[{"priority":1,"table":"main","suppress_prefixlen":null}]`, `[{"priority":1,"table":"main","suppress_prefixlen":"0"}]`, strings.Repeat(" ", (1<<20)+1), "[" + strings.Repeat(`{"priority":1,"table":"main"},`, 4096) + `{"priority":1,"table":"main"}]`} {
		if absent, err := linuxPolicyRuleAbsent([]byte(output), true); err == nil || absent {
			t.Fatal("invalid observation established absence")
		}
	}
	for _, output := range []string{"[]", `[{"priority":32766,"table":"main"}]`, `[{"priority":32764,"table":"main","suppress_prefixlen":24}]`} {
		if absent, err := linuxPolicyRuleAbsent([]byte(output), true); err != nil || !absent {
			t.Fatal("unrelated rule blocked cleanup", err)
		}
	}
}
