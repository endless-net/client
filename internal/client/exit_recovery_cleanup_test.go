package client

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestExitRecoveryCleansSurvivingRulesAcrossProcessRetry(t *testing.T) {
	remaining := map[string]bool{"-4/51820": true, "-4/254": true, "-6/51820": true, "-6/254": true}
	routes := map[string]bool{"-4": true, "-6": true}
	routeDeletions := 0
	contained, released, failIPv6 := false, false, true
	deletions := 0
	runner := exitGuardReadbackRunner(t, "endlessnet", 51820, func(_ context.Context, input, name string, args ...string) ([]byte, error) {
		if name == "nft" {
			if strings.Contains(input, "add chain") {
				contained = true
			} else {
				released = true
			}
			return nil, nil
		}
		if name != "ip" || !contained {
			t.Fatal("cleanup escaped containment", name, args)
		}
		if slices.Contains(args, "route") {
			if slices.Contains(args, "del") {
				if !slices.Contains(args, "endlessnet") || !slices.Contains(args, "51820") || !slices.Contains(args, "default") || !slices.Contains(args, "proto") {
					t.Fatal("unscoped route delete", args)
				}
				routes[args[0]] = false
				routeDeletions++
				return nil, nil
			}
			family := args[2]
			if !routes[family] {
				return []byte("[]"), nil
			}
			if family == "-4" {
				return []byte(`[{"dst":"default","dev":"endlessnet","table":"51820","scope":"253","flags":[]}]`), nil
			}
			return []byte(`[{"dst":"default","dev":"endlessnet","table":"51820","metric":1024,"pref":"medium","flags":[]}]`), nil
		}
		family := args[0]
		table := ""
		for i, value := range args {
			if value == "table" && i+1 < len(args) {
				table = args[i+1]
			}
		}
		if table == "main" {
			table = "254"
		}
		key := family + "/" + table
		if _, exists := remaining[key]; !exists {
			t.Fatal("cleanup changed durable scope", args)
		}
		if slices.Contains(args, "del") {
			if !slices.Contains(args, "not") || !slices.Contains(args, "51820/0xffffffff") || !slices.Contains(args, "pref") {
				t.Fatal("unscoped rule delete", args)
			}
			deletions++
			if family == "-6" && failIPv6 {
				return nil, errors.New("injected delete failure")
			}
			remaining[key] = false
			return nil, nil
		}
		var rows []string
		if remaining[key] {
			if table == "254" {
				rows = append(rows, `{"priority":32764,"src":"all","table":"254","protocol":"0","not":null,"fwmark":"0xca6c","suppress_prefixlen":0}`)
			} else {
				rows = append(rows, `{"priority":32765,"src":"all","table":"51820","protocol":"0","not":null,"fwmark":"0xca6c"}`)
			}
		}
		if table == "254" {
			rows = append(rows, `{"priority":32000,"src":"all","table":"254","suppress_prefixlen":0}`, `{"priority":32766,"src":"all","table":"254"}`)
		}
		return []byte("[" + strings.Join(rows, ",") + "]"), nil
	})
	guard, err := newLinuxExitGuard("endlessnet", 51820, runner)
	if err != nil {
		t.Fatal(err)
	}
	e := &WireGuardEngine{exitGuard: guard, exitFilter: &exitPacketFilter{}}
	if err := e.releaseClearedExit(t.Context(), guard); err == nil || released || e.exitGuard != guard {
		t.Fatal("partial cleanup released protection", err)
	}
	if remaining["-4/51820"] || remaining["-4/254"] || !remaining["-6/51820"] {
		t.Fatal("partial recovery state is incorrect", remaining)
	}
	failIPv6 = false
	// Nothing from router.current or the first engine survives this retry.
	restarted := &WireGuardEngine{}
	if err := restarted.restoreClearProtection(t.Context(), guard); err != nil {
		t.Fatal(err)
	}
	if err := restarted.releaseClearedExit(t.Context(), guard); err != nil || !released || restarted.exitGuard != nil {
		t.Fatal("fresh-process retry failed", err)
	}
	if deletions != 5 {
		t.Fatal("already removed rules were deleted again", deletions)
	}
	if routeDeletions != 2 || routes["-4"] || routes["-6"] {
		t.Fatal("default cleanup did not survive process retry", routeDeletions, routes)
	}
	for key, exists := range remaining {
		if exists {
			t.Fatal("owned rule survived", key)
		}
	}
}

func TestExitRecoveryRejectsForeignOrLiveScopeBeforeCommands(t *testing.T) {
	for _, scenario := range []string{"foreign", "live", "cancelled", "reserved"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			guard, err := newLinuxExitGuard("endlessnet", 51820, func(context.Context, string, string, ...string) ([]byte, error) {
				calls++
				return nil, errors.New("unexpected command")
			})
			if err != nil {
				t.Fatal(err)
			}
			e := &WireGuardEngine{exitGuard: guard}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "foreign":
				guard = &linuxExitGuard{}
			case "live":
				e.configured = true
			case "cancelled":
				cancel()
			case "reserved":
				guard.mark = 254
			}
			if err := e.recoverStoppedExit(ctx, guard); err == nil || calls != 0 {
				t.Fatal(fmt.Sprint("invalid recovery mutated OS: ", err, calls))
			}
		})
	}
}
