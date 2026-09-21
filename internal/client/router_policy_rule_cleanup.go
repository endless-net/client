package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

func validExitPolicyTable(table uint32) bool {
	return table != 0 && table != 253 && table != 254 && table != 255
}

// Observe a complete owned rule before deletion: the kernel does not compare
// every omitted selector or inversion flag. Include its unique priority and
// full mark mask; never claim ownership of generic main-table suppression.
func removeLinuxPolicyRule(ctx context.Context, runner CommandRunner, family string, table uint32, suppress bool) error {
	if runner == nil || (family != "-4" && family != "-6") || !validExitPolicyTable(table) {
		return errors.New("invalid policy rule cleanup target")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := strconv.FormatUint(uint64(table), 10)
	target := id
	if suppress {
		target = "254"
	}
	query := []string{family, "-j", "-N", "-d", "rule", "show", "table", target}
	output, err := runner(ctx, "ip", query...)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err != nil {
		return errors.New("policy rule observation failed")
	}
	rules, err := decodeLinuxPolicyRules(output)
	if err != nil {
		return err
	}
	var selected *uint32
	priorities := map[uint32]int{}
	for _, rule := range rules {
		var priority uint32
		_ = json.Unmarshal(rule["priority"], &priority)
		priorities[priority]++
		owned, err := linuxPolicyRuleScope(rule, table, suppress)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}
		if string(rule["protocol"]) != `"0"` || !linuxPolicyRuleExact(rule, table, suppress) || selected != nil {
			return errors.New("ambiguous owned policy rule")
		}
		selected = &priority
	}
	if selected == nil {
		return nil
	}
	if priorities[*selected] != 1 {
		return errors.New("ambiguous policy rule priority")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	selector := []string{family, "rule", "del", "pref", strconv.FormatUint(uint64(*selected), 10), "not", "fwmark", id + "/0xffffffff", "table", target}
	if suppress {
		selector = append(selector, "suppress_prefixlength", "0")
	}
	_, removeErr := runner(ctx, "ip", selector...)
	if err := ctx.Err(); err != nil {
		return errors.Join(removeErr, err)
	}
	output, err = runner(ctx, "ip", query...)
	if err := ctx.Err(); err != nil {
		return errors.Join(removeErr, err)
	}
	if err != nil {
		return errors.Join(removeErr, errors.New("policy rule observation failed"))
	}
	absent, err := linuxPolicyRuleAbsent(output, table, suppress)
	if err != nil {
		return errors.Join(removeErr, err)
	}
	if !absent {
		return errors.Join(removeErr, errors.New("policy rule removal is not confirmed"))
	}
	return nil
}

func decodeLinuxPolicyRules(output []byte) ([]map[string]json.RawMessage, error) {
	invalid := errors.New("invalid policy rule observation")
	if len(output) == 0 || len(output) > 1<<20 {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	if _, ok := exitGuardJSONValue(decoder, 0); !ok {
		return nil, invalid
	}
	var rules []map[string]json.RawMessage
	if json.Unmarshal(output, &rules) != nil || rules == nil || len(rules) > 4096 {
		return nil, invalid
	}
	for _, rule := range rules {
		var priority uint32
		var table string
		if rule == nil || string(rule["priority"]) == "null" || json.Unmarshal(rule["priority"], &priority) != nil || json.Unmarshal(rule["table"], &table) != nil {
			return nil, invalid
		}
		id, err := strconv.ParseUint(table, 10, 32)
		if err != nil || id == 0 {
			return nil, invalid
		}
		if raw, ok := rule["suppress_prefixlen"]; ok {
			var prefix int
			if string(raw) == "null" || json.Unmarshal(raw, &prefix) != nil || prefix < 0 || prefix > 128 {
				return nil, invalid
			}
		}
		if raw, ok := rule["fwmark"]; ok {
			var mark string
			if json.Unmarshal(raw, &mark) != nil {
				return nil, invalid
			}
			if _, err := strconv.ParseUint(mark, 0, 32); err != nil {
				return nil, invalid
			}
		}
	}
	return rules, nil
}

// Dedicated tables must be free of all references. Main is shared: only our
// explicit mark identifies owned scope; generic and other-owner rules remain.
func linuxPolicyRuleScope(rule map[string]json.RawMessage, mark uint32, suppress bool) (bool, error) {
	var table string
	_ = json.Unmarshal(rule["table"], &table)
	want := strconv.FormatUint(uint64(mark), 10)
	if suppress {
		want = "254"
	}
	if table != want {
		return false, errors.New("policy observation escaped target table")
	}
	if !suppress {
		return true, nil
	}
	raw, ok := rule["fwmark"]
	if !ok {
		return false, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return false, errors.New("invalid policy mark")
	}
	parsed, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return false, errors.New("invalid policy mark")
	}
	return parsed == uint64(mark), nil
}

func linuxPolicyRuleExact(rule map[string]json.RawMessage, mark uint32, suppress bool) bool {
	var source, value string
	if json.Unmarshal(rule["src"], &source) != nil || source != "all" || string(rule["not"]) != "null" || json.Unmarshal(rule["fwmark"], &value) != nil {
		return false
	}
	parsed, err := strconv.ParseUint(value, 0, 32)
	if err != nil || parsed != uint64(mark) {
		return false
	}
	for key, raw := range rule {
		switch key {
		case "priority", "src", "table", "not", "fwmark":
		case "protocol":
			if string(raw) != `"0"` {
				return false
			}
		case "fwmask":
			var mask string
			if json.Unmarshal(raw, &mask) != nil {
				return false
			}
			value, err := strconv.ParseUint(mask, 0, 32)
			if err != nil || value != 0xffffffff {
				return false
			}
		case "suppress_prefixlen":
			if !suppress || string(raw) != "0" {
				return false
			}
		default:
			return false
		}
	}
	_, hasSuppress := rule["suppress_prefixlen"]
	return hasSuppress == suppress
}

func linuxPolicyRuleAbsent(output []byte, mark uint32, suppress bool) (bool, error) {
	if !validExitPolicyTable(mark) {
		return false, errors.New("invalid policy observation target")
	}
	rules, err := decodeLinuxPolicyRules(output)
	if err != nil {
		return false, err
	}
	found := false
	for _, rule := range rules {
		owned, err := linuxPolicyRuleScope(rule, mark, suppress)
		if err != nil {
			return false, err
		}
		found = found || owned
	}
	return !found, nil
}
