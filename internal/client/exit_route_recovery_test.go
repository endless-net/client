package client

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const recoveryDefault4 = `{"dst":"default","dev":"en0","table":"51999","scope":"253","flags":[]}`
const recoveryDefault6 = `{"dst":"default","dev":"en0","table":"51999","metric":1024,"pref":"medium","flags":[]}`

func TestExitRouteRecoveryRestartsPartialCleanupAndPreservesForeignTables(t *testing.T) {
	present := map[string]bool{"-4": true, "-6": true}
	fail6 := true
	var mutations []string
	guard := &linuxExitGuard{interfaceName: "en0", mark: 51999, run: func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		if input != "" || name != "ip" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no deadline")
		}
		if len(args) == 7 && args[0] == "-j" && args[1] == "-N" && strings.Join(args[3:], " ") == "route show table all" {
			row := `{"dst":"default","dev":"eth0","gateway":"192.0.2.1"}`
			if present[args[2]] {
				value := recoveryDefault4
				if args[2] == "-6" {
					value = recoveryDefault6
				}
				row += "," + value
			}
			return []byte("[" + row + "]"), nil
		}
		family := args[0]
		scope, metric := "253", "0"
		if family == "-6" {
			scope = "0"
			metric = "1024"
		}
		want := family + " route del unicast default dev en0 table 51999 proto 3 scope " + scope + " metric " + metric
		if strings.Join(args, " ") != want {
			t.Fatalf("unsafe delete %v", args)
		}
		mutations = append(mutations, family)
		if family == "-6" && fail6 {
			return nil, errors.New("synthetic failure")
		}
		present[family] = false
		// Prove removal errors never substitute for readback: the route may
		// have disappeared concurrently and must still be observed absent.
		return nil, errors.New("already absent")
	}}
	if err := recoverExitOwnedDefaultRoutes(t.Context(), guard); err == nil {
		t.Fatal("accepted incomplete family cleanup")
	}
	if present["-4"] == true || present["-6"] == false {
		t.Fatal("incorrect partial state")
	}
	fail6 = false
	if err := recoverExitOwnedDefaultRoutes(t.Context(), guard); err != nil {
		t.Fatal(err)
	}
	if strings.Join(mutations, ",") != "-4,-6,-6" {
		t.Fatalf("retry mutations %v", mutations)
	}
	if err := recoverExitOwnedDefaultRoutes(t.Context(), guard); err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 3 {
		t.Fatal("idempotent absence mutated routes")
	}
}

func TestExitRouteRecoveryPreflightsBothFamiliesBeforeMutation(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(recoveryDefault6, `"en0"`, `"foreign0"`, 1),
		strings.Replace(recoveryDefault6, `"default"`, `"2001:db8::/64"`, 1),
		strings.Replace(recoveryDefault6, `"flags":[]`, `"gateway":"2001:db8::1"`, 1),
		strings.Replace(recoveryDefault6, `"flags":[]`, `"nhid":7`, 1),
		strings.Replace(recoveryDefault6, `"flags":[]`, `"encap":{}`, 1),
		strings.Replace(recoveryDefault6, `"flags":[]`, `"protocol":"4"`, 1),
		strings.Replace(recoveryDefault6, `1024`, `1025`, 1),
		recoveryDefault6 + "," + recoveryDefault6,
	} {
		mutated := false
		guard := &linuxExitGuard{interfaceName: "en0", mark: 51999, run: func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
			if args[0] != "-j" {
				mutated = true
				return nil, nil
			}
			if args[2] == "-4" {
				return []byte("[" + recoveryDefault4 + "]"), nil
			}
			return []byte("[" + bad + "]"), nil
		}}
		if err := recoverExitOwnedDefaultRoutes(t.Context(), guard); err == nil || mutated {
			t.Fatalf("unsafe scope mutated=%v err=%v", mutated, err)
		}
	}
}

func TestExitRouteRecoveryRejectsMalformedObservation(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[{}]`, `[null]`, `[] []`, `[{"dst":"default","table":51999}]`, "[" + strings.Replace(recoveryDefault4, `"dev":"en0"`, `"dev":"foreign0","dev":"en0"`, 1) + "]", strings.Repeat(" ", 1<<20) + `[]`} {
		if _, err := exitOwnedDefaultRoutePresent([]byte(raw), "-4", 51999, "en0"); err == nil {
			t.Fatal("accepted malformed observation")
		}
	}
}

func TestExitRouteRecoveryRejectsReservedIdentityBeforeCommands(t *testing.T) {
	for _, mark := range []uint32{0, 253, 254, 255} {
		called := false
		guard := &linuxExitGuard{interfaceName: "en0", mark: mark, run: func(context.Context, string, string, ...string) ([]byte, error) { called = true; return nil, nil }}
		if err := recoverExitOwnedDefaultRoutes(t.Context(), guard); err == nil || called {
			t.Fatalf("reserved table %d: called=%v err=%v", mark, called, err)
		}
	}
}

func TestExitRouteRecoveryCancellationAndFailedReadbackRetainFailure(t *testing.T) {
	for _, cancelOnDelete := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		deleted := false
		guard := &linuxExitGuard{interfaceName: "en0", mark: 51999, run: func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
			if args[0] != "-j" {
				deleted = true
				if cancelOnDelete {
					cancel()
				}
				return nil, nil
			}
			if deleted {
				return nil, errors.New("private output")
			}
			if args[2] == "-4" {
				return []byte("[" + recoveryDefault4 + "]"), nil
			}
			return []byte(`[]`), nil
		}}
		err := recoverExitOwnedDefaultRoutes(ctx, guard)
		cancel()
		if err == nil || !deleted {
			t.Fatalf("failed readback accepted: %v", err)
		}
		if cancelOnDelete && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel lost: %v", err)
		}
		if strings.Contains(err.Error(), "private") {
			t.Fatal("native output leaked")
		}
	}
}
