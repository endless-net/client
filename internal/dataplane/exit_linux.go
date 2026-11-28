package dataplane

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
)

const linuxExitPriority = "32000"

// The table number follows the durable session identity. All its routes are
// attached to the nonpersistent TUN and disappear with the link.
func linuxExitTable(owner string) (string, error) {
	id, err := uuid.Parse(owner)
	if err != nil {
		return "", ErrIPOwnership
	}
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(id[:4])>>2|1<<30), 10), nil
}

// Read all selectors. A rule sharing our table or priority with a changed
// selector must never be adopted or deleted.
func linuxExitRulePresent(raw []byte, table string, installing bool) (bool, error) {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return false, ErrIPOwnership
	}
	found := false
	local := false
	for _, row := range rows {
		field := func(key string) string { return strings.Trim(string(row[key]), "\"") }
		priority, err := strconv.ParseUint(field("priority"), 10, 32)
		if err != nil {
			return false, ErrIPOwnership
		}
		if field("table") != table && (!installing || field("priority") != linuxExitPriority) {
			if installing && priority <= 32000 {
				if row["not"] == nil && field("iif") != "" && field("iif") != "lo" {
					continue
				}
				if local || priority != 0 || field("src") != "all" || field("table") != "255" || field("protocol") != "2" || len(row) != 4 {
					return false, ErrIPConflict
				}
				local = true
			}
			continue
		}
		mark, err := strconv.ParseUint(field("fwmark"), 0, 32)
		if err != nil || mark != 0 || len(row) != 6 || field("iif") != "lo" || field("src") != "all" ||
			field("priority") != linuxExitPriority || field("table") != table || field("protocol") != linuxRouteProtocol || found {
			return false, ErrIPConflict
		}
		found = true
	}
	if installing && !local {
		return false, ErrIPConflict
	}
	return found, nil
}

type linuxRouteCommand func(context.Context, ...string) ([]byte, error)

func linuxRouteIP(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, linuxIP, args...).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, ErrIPOwnership
	}
	return out, nil
}

func linuxReadExitRule(ctx context.Context, run linuxRouteCommand, table string, installing bool) (bool, error) {
	raw, err := run(ctx, "-j", "-N", "-details", "-4", "rule", "show")
	if err != nil {
		return false, err
	}
	return linuxExitRulePresent(raw, table, installing)
}

func changeLinuxExitRule(ctx context.Context, run linuxRouteCommand, table, action string) error {
	_, err := run(ctx, "-4", "rule", action, "pref", linuxExitPriority, "iif", "lo", "fwmark", "0/0xffffffff", "lookup", table, "protocol", linuxRouteProtocol)
	return err
}

func removeLinuxExitRule(ctx context.Context, run linuxRouteCommand, receipt state.LinuxNetworkReceipt) error {
	if !receipt.ExitRule {
		return nil
	}
	table, err := linuxExitTable(receipt.Owner)
	if err != nil {
		return err
	}
	found, err := linuxReadExitRule(ctx, run, table, false)
	if err != nil || !found {
		return err
	}
	if err := changeLinuxExitRule(ctx, run, table, "del"); err != nil {
		return err
	}
	found, err = linuxReadExitRule(ctx, run, table, false)
	if err == nil && found {
		return ErrIPOwnership
	}
	return err
}

type linuxExitRoutes struct {
	table    string
	overlay  netip.Prefix
	prefixes []netip.Prefix
}

func (x linuxExitRoutes) check(ctx context.Context, device string, source netip.Addr, installed bool) error {
	raw, err := linuxRouteIP(ctx, "-j", "-N", "-4", "route", "show", "table", "all")
	if err != nil {
		return err
	}
	return x.checkRows(raw, device, source, installed)
}

func (x linuxExitRoutes) checkRows(raw []byte, device string, source netip.Addr, installed bool) error {
	var rows []linuxInventoryRoute
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return ErrIPOwnership
	}
	expected := make(map[netip.Prefix]bool, len(x.prefixes))
	if installed {
		for _, p := range x.prefixes {
			expected[p] = true
		}
	}
	for _, row := range rows {
		table := strings.Trim(string(row.Table), "\"")
		if row.Destination == "default" && table != x.table {
			continue
		}
		p, err := linuxInventoryPrefix(row.Destination)
		if err != nil {
			return ErrIPConflict
		}
		if table == x.table {
			if !expected[p] || row.Device != device || !row.direct() || row.Source != source.String() || strings.Trim(string(row.Protocol), "\"") != linuxRouteProtocol {
				return ErrIPConflict
			}
			delete(expected, p)
		} else if table != "255" && table != "local" && row.Device != device && slices.ContainsFunc(x.prefixes, p.Overlaps) {
			return ErrIPConflict
		}
	}
	if len(expected) != 0 {
		return ErrIPOwnership
	}
	return nil
}

func (s *LinuxSession) configureExit(ctx context.Context, plan linuxFilterPlan) error {
	zero := netip.PrefixFrom(netip.IPv4Unspecified(), 0)
	want := slices.Contains(plan.activeResources, zero)
	link := s.link
	if want == (link.exit != nil) {
		if want && link.exit.overlay != plan.overlay {
			return ErrIPConflict
		}
		return nil
	}
	next := s.receipt
	next.ExitRule = want
	if !want {
		if err := removeLinuxExitRule(ctx, linuxRouteIP, s.receipt); err != nil {
			return err
		}
		for _, p := range link.exit.prefixes {
			if err := link.routeInTable(ctx, "del", link.exit.table, p); err != nil {
				return err
			}
		}
		link.exit = nil
	} else {
		table, err := linuxExitTable(s.receipt.Owner)
		if err != nil {
			return err
		}
		prefixes, err := exitPublicPrefixes(plan.overlay)
		if err != nil {
			return err
		}
		exit := &linuxExitRoutes{table: table, overlay: plan.overlay, prefixes: prefixes}
		found, err := linuxReadExitRule(ctx, linuxRouteIP, table, true)
		if err != nil {
			return err
		}
		if found {
			return ErrIPConflict
		}
		if err := exit.check(ctx, link.name, link.address, false); err != nil {
			return err
		}
		for _, p := range prefixes {
			if err := link.routeInTable(ctx, "add", table, p); err != nil {
				return err
			}
		}
		if err := exit.check(ctx, link.name, link.address, true); err != nil {
			return err
		}
		if err := s.ledger.Replace(ctx, s.receipt, next); err != nil {
			return err
		}
		s.receipt = next
		if err := changeLinuxExitRule(ctx, linuxRouteIP, table, "add"); err != nil {
			return err
		}
		link.exit = exit
		return link.verify(ctx)
	}
	if err := s.ledger.Replace(ctx, s.receipt, next); err != nil {
		return err
	}
	s.receipt = next
	return nil
}
