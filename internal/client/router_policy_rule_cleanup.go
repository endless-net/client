package client

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

// iproute2 filters the dump by the same family/table/mark as removal. The
// suppress-prefix selector is not available for "show", so inspect that JSON
// attribute within the table-filtered dump. Never infer absence from stderr.
func removeLinuxPolicyRule(ctx context.Context, runner CommandRunner, family string, table uint32, suppress bool) error {
	if runner == nil || (family != "-4" && family != "-6") || table == 0 {
		return errors.New("invalid policy rule cleanup target")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := strconv.FormatUint(uint64(table), 10)
	selector := []string{"not", "fwmark", id, "table", id}
	query := []string{family, "-j", "rule", "show", "table", id, "not", "fwmark", id}
	if suppress {
		selector = []string{"table", "main", "suppress_prefixlength", "0"}
		query = []string{family, "-j", "rule", "show", "table", "main"}
	}
	_, removeErr := runner(ctx, "ip", append([]string{family, "rule", "del"}, selector...)...)
	if err := ctx.Err(); err != nil {
		return errors.Join(removeErr, err)
	}
	output, inspectErr := runner(ctx, "ip", query...)
	if err := ctx.Err(); err != nil {
		return errors.Join(removeErr, inspectErr, err)
	}
	if inspectErr != nil {
		return errors.Join(removeErr, errors.New("policy rule observation failed"))
	}
	absent, err := linuxPolicyRuleAbsent(output, suppress)
	if err != nil {
		return errors.Join(removeErr, err)
	}
	if !absent {
		return errors.Join(removeErr, errors.New("policy rule removal is not confirmed"))
	}
	return nil
}

func linuxPolicyRuleAbsent(output []byte, suppress bool) (bool, error) {
	invalid := func() (bool, error) { return false, errors.New("invalid policy rule observation") }
	if len(output) == 0 || len(output) > 1<<20 {
		return invalid()
	}
	var rules []map[string]json.RawMessage
	if json.Unmarshal(output, &rules) != nil || rules == nil || len(rules) > 4096 {
		return invalid()
	}
	found := false
	for _, rule := range rules {
		var priority uint32
		var table string
		if rule == nil || string(rule["priority"]) == "null" || json.Unmarshal(rule["priority"], &priority) != nil || json.Unmarshal(rule["table"], &table) != nil || table == "" {
			return invalid()
		}
		if !suppress {
			found = true
			continue
		}
		if raw, ok := rule["suppress_prefixlen"]; ok {
			var prefix int
			if string(raw) == "null" || json.Unmarshal(raw, &prefix) != nil || prefix < 0 || prefix > 128 {
				return invalid()
			}
			found = found || prefix == 0
		}
	}
	return !found, nil
}
