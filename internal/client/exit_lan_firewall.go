package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANFirewallState struct {
	plan    *exitLANPlan
	routing *exitLANRoutingOwnership
}

func (lan *exitLANFirewallState) rules(table string, underlay uint32) (string, []any, []any, error) {
	if lan == nil || lan.plan == nil || validateExitLANRouting(lan.routing) != nil || lan.routing.Mark == underlay {
		return "", nil, nil, errExitLANPolicy
	}
	var batch strings.Builder
	var output, marking []any
	match := func(left, right any) any {
		return map[string]any{"match": map[string]any{"op": "==", "left": left, "right": right}}
	}
	meta := func(key string) any { return map[string]any{"meta": map[string]any{"key": key}} }
	payload := func(protocol, field string) any {
		return map[string]any{"payload": map[string]any{"protocol": protocol, "field": field}}
	}
	number := func(v uint64) any { return json.Number(strconv.FormatUint(v, 10)) }
	// DHCP renewal remains infrastructure even when its unicast server lies
	// inside a LAN prefix. It must not acquire the expiring application mark.
	fmt.Fprintf(&batch, "add rule inet %s lanroute meta nfproto ipv4 meta l4proto udp udp sport 68 udp dport 67 return\n", table)
	marking = append(marking, []any{exitGuardNFProtoMatch("ipv4"), match(meta("l4proto"), number(17)), match(payload("udp", "sport"), number(68)), match(payload("udp", "dport"), number(67)), map[string]any{"return": nil}})
	for _, binding := range lan.plan.bindings {
		for _, prefix := range binding.destinations {
			protocol := "ip"
			if prefix.Addr().Is6() {
				protocol = "ip6"
			}
			destination := any(map[string]any{"prefix": map[string]any{"addr": prefix.Addr().String(), "len": number(uint64(prefix.Bits()))}})
			if prefix.Bits() == prefix.Addr().BitLen() {
				destination = prefix.Addr().String()
			}
			fmt.Fprintf(&batch, "add rule inet %s lanroute meta mark != %d %s daddr %s meta mark set %d\n", table, underlay, protocol, prefix, lan.routing.Mark)
			marking = append(marking, []any{map[string]any{"match": map[string]any{"op": "!=", "left": meta("mark"), "right": number(uint64(underlay))}}, match(payload(protocol, "daddr"), destination), map[string]any{"mangle": map[string]any{"key": meta("mark"), "value": number(uint64(lan.routing.Mark))}}})
			for _, source := range binding.sources {
				fmt.Fprintf(&batch, "add rule inet %s output meta mark %d meta oif %d %s saddr %s %s daddr %s accept\n", table, lan.routing.Mark, binding.linkIndex, protocol, source, protocol, prefix)
				fmt.Fprintf(&batch, "add rule inet %s lanfinal meta mark %d meta oif %d %s saddr %s %s daddr %s accept\n", table, lan.routing.Mark, binding.linkIndex, protocol, source, protocol, prefix)
				output = append(output, []any{match(meta("mark"), number(uint64(lan.routing.Mark))), match(meta("oif"), number(uint64(binding.linkIndex))), match(payload(protocol, "saddr"), source.String()), match(payload(protocol, "daddr"), destination), map[string]any{"accept": nil}})
			}
			if len(output) > exitLANPrefixLimit || batch.Len() > 1<<20 {
				return "", nil, nil, errExitLANPolicy
			}
		}
	}
	fmt.Fprintf(&batch, "add rule inet %s lanfinal meta mark %d drop\n", table, lan.routing.Mark)
	return batch.String(), output, marking, nil
}

// Recheck the complete binding after routing and NAT. An output verdict alone
// cannot authorize a packet rerouted to another otherwise eligible LAN device.
func (lan *exitLANFirewallState) finalRules(output []any) []any {
	return append(append([]any(nil), output...), []any{map[string]any{"match": map[string]any{"op": "==", "left": map[string]any{"meta": map[string]any{"key": "mark"}}, "right": json.Number(strconv.FormatUint(uint64(lan.routing.Mark), 10))}}, map[string]any{"drop": nil}})
}

// The caller has already pinned and read back the closed BPF hook, installed
// the terminal routing table and published a bounded lease. Any ambiguity
// replaces the entire owned table with containment before returning.
func (g *linuxExitGuard) openLAN(ctx context.Context, family api.ExitFamilyMode, lan *exitLANFirewallState) error {
	if !exitGuardFamilyValid(family) {
		return errExitLANPolicy
	}
	if _, _, _, err := lan.rules(g.table, g.mark); err != nil {
		return err
	}
	if !lockExitRuntime(ctx, &g.mu) {
		return ctx.Err()
	}
	defer g.mu.Unlock()
	g.lan = lan
	err := g.applyObserved(ctx, g.rulesBatch(true, family), true, false, family)
	if err != nil {
		g.lan = nil
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(err, g.applyObserved(recovery, g.rulesBatch(false, ""), false, false, ""))
	}
	return nil
}
