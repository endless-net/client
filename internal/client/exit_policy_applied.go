package client

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

// Observe the installed selectors and their relative order. Other tables' rules
// and the resulting packet path still require separate qualification.
func confirmExitPolicyRules(ctx context.Context, guard *linuxExitGuard, family string) error {
	priorities := [2]uint32{}
	for i, table := range []uint32{guard.mark, 254} {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := guard.run(ctx, "", "ip", family, "-j", "-N", "rule", "show", "table", strconv.FormatUint(uint64(table), 10))
		if err != nil {
			return errors.New("exit policy rule observation failed")
		}
		priority, ok := exitPolicyRuleObserved(raw, guard.mark, i == 1, priorities[0])
		if !ok {
			return errors.New("exit policy rule is not confirmed")
		}
		priorities[i] = priority
	}
	if priorities[1] >= priorities[0] {
		return errors.New("exit policy rule order is not confirmed")
	}
	return ctx.Err()
}

func exitPolicyRuleObserved(raw []byte, mark uint32, suppress bool, exitPriority uint32) (uint32, bool) {
	if len(raw) > 1<<20 {
		return 0, false
	}
	var rules []map[string]json.RawMessage
	if json.Unmarshal(raw, &rules) != nil || rules == nil || len(rules) > 4096 {
		return 0, false
	}
	var selected uint32
	found := false
	for _, rule := range rules {
		var table, src string
		var priority uint32
		if json.Unmarshal(rule["table"], &table) != nil || json.Unmarshal(rule["src"], &src) != nil || string(rule["priority"]) == "null" || json.Unmarshal(rule["priority"], &priority) != nil {
			return 0, false
		}
		want := strconv.FormatUint(uint64(mark), 10)
		if suppress {
			want = "254"
		}
		if table != want {
			return 0, false
		}
		if suppress {
			if _, ok := rule["suppress_prefixlen"]; !ok {
				// A main-table lookup before (or tied with) the exit lookup
				// can select the ordinary default route despite suppression.
				if priority <= exitPriority {
					return 0, false
				}
				continue
			}
			if string(rule["suppress_prefixlen"]) != "0" {
				return 0, false
			}
		} else {
			var value string
			if string(rule["not"]) != "null" || json.Unmarshal(rule["fwmark"], &value) != nil {
				return 0, false
			}
			parsed, err := strconv.ParseUint(value, 0, 32)
			if err != nil || parsed != uint64(mark) {
				return 0, false
			}
		}
		if src != "all" || found {
			return 0, false
		}
		for key := range rule {
			switch key {
			case "priority", "src", "table", "protocol":
			case "not", "fwmark":
				if suppress {
					return 0, false
				}
			case "suppress_prefixlen":
				if !suppress {
					return 0, false
				}
			default:
				return 0, false
			}
		}
		selected, found = priority, true
	}
	return selected, found
}
