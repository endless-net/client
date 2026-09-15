package client

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strconv"
	"time"
)

// Read all tables so a nonexistent dedicated table is observed as absent,
// rather than treating a failed table-specific query as successful cleanup.
func exitRouteTablesEmpty(ctx context.Context, table uint32, runner CommandRunner) (bool, error) {
	if table == 0 {
		return false, errors.New("exit route observation requires a table")
	}
	if runner == nil {
		if runtime.GOOS != "linux" {
			return false, errors.New("exit route observation requires Linux")
		}
		runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return runExitCommand(ctx, "", name, args...)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	empty := true
	for _, family := range []string{"-4", "-6"} {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		raw, err := runner(ctx, "ip", "-j", "-N", family, "route", "show", "table", "all")
		if err != nil {
			return false, errors.New("exit route observation failed")
		}
		absent, err := exitRouteTableAbsent(raw, table)
		if err != nil {
			return false, err
		}
		empty = empty && absent
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return empty, nil
}

func exitRouteTableAbsent(raw []byte, table uint32) (bool, error) {
	invalid := errors.New("invalid exit route observation")
	if len(raw) > 1<<20 {
		return false, invalid
	}
	var routes []struct {
		Destination string `json:"dst"`
		Table       string `json:"table"`
	}
	if json.Unmarshal(raw, &routes) != nil || routes == nil {
		return false, invalid
	}
	absent := true
	for _, route := range routes {
		if route.Destination == "" {
			return false, invalid
		}
		// iproute2 omits the main table unless details are requested. With -N,
		// every explicit table is printed as a numeric string.
		id := uint64(254)
		if route.Table != "" {
			var err error
			id, err = strconv.ParseUint(route.Table, 10, 32)
			if err != nil || id == 0 {
				return false, invalid
			}
		}
		if uint32(id) == table {
			absent = false
		}
	}
	return absent, nil
}

// Re-observe rules after Down, including recovery where no in-memory router
// survived. Table references are checked without a mark filter so altered or
// duplicate rules cannot be mistaken for a cleared dedicated table.
func exitPolicyRulesAbsent(ctx context.Context, table uint32, runner CommandRunner) (bool, error) {
	if table == 0 || runner == nil {
		return false, errors.New("invalid exit policy rule observation target")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	absent := true
	for _, family := range []string{"-4", "-6"} {
		for _, suppress := range []bool{false, true} {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			id := strconv.FormatUint(uint64(table), 10)
			if suppress {
				id = "254"
			}
			raw, err := runner(ctx, "ip", family, "-j", "-N", "rule", "show", "table", id)
			if err != nil {
				return false, errors.New("exit policy rule observation failed")
			}
			clear, err := linuxPolicyRuleAbsent(raw, suppress)
			if err != nil {
				return false, err
			}
			absent = absent && clear
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return absent, nil
}
