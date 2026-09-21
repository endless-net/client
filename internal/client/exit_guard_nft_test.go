package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestLinuxExitGuardAtomicContainmentAndRelease(t *testing.T) {
	var batches []string
	fail := false
	runner := func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		if name != "nft" || !reflect.DeepEqual(args, []string{"-f", "-"}) {
			t.Fatal("guard must submit one batch over stdin")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded native command")
		}
		batches = append(batches, input)
		if fail {
			return []byte("private host detail"), errors.New("private host detail")
		}
		return nil, nil
	}
	observedRunner := exitGuardReadbackRunner(t, "endlessnet", 51820, runner)
	guard, err := newLinuxExitGuard("endlessnet", 51820, observedRunner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := guard.Contain(ctx); err != nil {
		t.Fatal(err)
	}
	closed := batches[0]
	reset := "add table inet " + guard.table + "\ndelete table inet " + guard.table + "\nadd table inet " + guard.table + "\n"
	if !strings.HasPrefix(closed, reset) || strings.Count(closed, "delete table") != 1 || strings.Contains(closed, "flush chain") {
		t.Fatal("restart must replace the owned table structure within the same protection transaction")
	}
	for _, want := range []string{
		"add table inet " + guard.table,
		"hook output priority 0; policy drop;",
		"hook forward priority 0; policy drop;",
		"output oifname \"lo\" accept",
		"output meta mark 51820 meta l4proto { tcp, udp } accept",
		"output oifname != \"endlessnet\" ip6 hoplimit 255 icmpv6 type { nd-router-solicit, nd-neighbor-solicit, nd-neighbor-advert } icmpv6 code 0 accept",
	} {
		if !strings.Contains(closed, want) {
			t.Fatalf("missing guard constraint: %s", want)
		}
	}
	for _, forbidden := range []string{"flush ruleset", "ct state", "ip daddr", "ip6 daddr", "oifname \"endlessnet\"", "echo-request", "echo-reply", "nd-router-advert", "nd-redirect"} {
		if strings.Contains(closed, forbidden) {
			t.Fatalf("unexpected containment bypass: %s", forbidden)
		}
	}
	if strings.Count(closed, " accept\n") != 3 {
		t.Fatal("unexpected direct-traffic exemption")
	}
	if err := guard.OpenTunnel(ctx, api.ExitFamilyDualStack); err != nil {
		t.Fatal(err)
	}
	open := batches[1]
	if open != closed+"add rule inet "+guard.table+" output oifname \"endlessnet\" accept\n" {
		t.Fatal("opening tunnel changed non-tunnel containment")
	}
	fail = true
	if err := guard.Contain(ctx); err == nil || strings.Contains(err.Error(), "private host detail") {
		t.Fatal("native failure must be reported without native output")
	}
	if len(batches) != 3 || batches[2] != closed {
		t.Fatal("failure must not issue cleanup or a permissive rollback")
	}
	fail = false
	// No in-memory success flag may suppress kernel reconciliation after restart.
	reopened, err := newLinuxExitGuard("endlessnet", 51820, observedRunner)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Contain(ctx); err != nil || batches[3] != closed {
		t.Fatal("restart changed ownership or skipped protection")
	}
	for range 2 {
		if err := reopened.Release(ctx); err != nil {
			t.Fatal(err)
		}
	}
	wantRelease := "add table inet " + guard.table + "\ndelete table inet " + guard.table + "\n"
	if batches[4] != wantRelease || batches[5] != wantRelease {
		t.Fatal("cleanup must be repeatable and limited to the owned table")
	}
}

func TestLinuxExitGuardRejectsUnsafeIdentityAndCancellation(t *testing.T) {
	calls := 0
	runner := func(context.Context, string, string, ...string) ([]byte, error) {
		calls++
		return nil, nil
	}
	for _, name := range []string{"", "lo", "endlessnet0123456", "tun\n", "tun\"; flush ruleset", "../tun"} {
		if _, err := newLinuxExitGuard(name, 51820, runner); err == nil {
			t.Fatalf("accepted invalid interface %q", name)
		}
	}
	for _, mark := range []uint32{0, 253, 254, 255} {
		if _, err := newLinuxExitGuard("endlessnet", mark, runner); err == nil {
			t.Fatalf("accepted reserved exit table %d", mark)
		}
	}
	guard, err := newLinuxExitGuard("endlessnet", 51820, runner)
	if err != nil {
		t.Fatal(err)
	}
	other, err := newLinuxExitGuard("endlessnet2", 51820, runner)
	if err != nil || guard.table == other.table {
		t.Fatal("independent interfaces share guard ownership")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, action := range []func(context.Context) error{guard.Contain, func(ctx context.Context) error { return guard.OpenTunnel(ctx, api.ExitFamilyDualStack) }, guard.Release} {
		if !errors.Is(action(ctx), context.Canceled) {
			t.Fatal("cancelled operation was accepted")
		}
	}
	if calls != 0 {
		t.Fatal("pre-cancelled command had native effects")
	}
	ctx, cancel = context.WithCancel(context.Background())
	guard.run = exitGuardReadbackRunner(t, "endlessnet", 51820, func(context.Context, string, string, ...string) ([]byte, error) {
		calls++
		cancel()
		return nil, nil
	})
	if !errors.Is(guard.OpenTunnel(ctx, api.ExitFamilyDualStack), context.Canceled) || calls != 2 {
		t.Fatal("late cancellation must restore closed containment")
	}
}

func TestLinuxExitGuardFamilyScopeAndReadOnlyObservation(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		var batches []string
		guard, err := newLinuxExitGuard("exit0", 51820, exitGuardReadbackRunner(t, "exit0", 51820, func(_ context.Context, input, _ string, _ ...string) ([]byte, error) {
			batches = append(batches, input)
			return nil, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := guard.OpenTunnel(t.Context(), family); err != nil {
			t.Fatal(err)
		}
		if err := guard.Observe(t.Context(), family); err != nil {
			t.Fatal(err)
		}
		if len(batches) != 1 {
			t.Fatal("observation mutated firewall")
		}
		if err := guard.ObserveContained(t.Context()); err == nil {
			t.Fatal("open rule set accepted as containment")
		}
		if err := guard.ObserveAbsent(t.Context()); err == nil {
			t.Fatal("open table accepted as absent")
		}
		batch := batches[0]
		if family != api.ExitFamilyDualStack {
			selected, ordinary := "ipv4", "ipv6"
			if family == api.ExitFamilyIPv6Only {
				selected, ordinary = "ipv6", "ipv4"
			}
			for _, part := range []string{"output meta nfproto " + ordinary + " accept", "forward meta nfproto " + ordinary + " accept", "output meta nfproto " + selected + " oifname \"exit0\" accept"} {
				if !strings.Contains(batch, part) {
					t.Fatalf("missing %s", part)
				}
			}
			if strings.Contains(batch, "forward meta nfproto "+selected+" accept") {
				t.Fatal("selected-family forwarding opened")
			}
			if guard.Observe(t.Context(), api.ExitFamilyDualStack) == nil {
				t.Fatal("single family accepted as dual stack")
			}
		}
		if err := guard.Contain(t.Context()); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(batches[1], "meta nfproto") {
			t.Fatal("containment preserved family exemption")
		}
		if err := guard.ObserveContained(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := guard.Release(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := guard.ObserveAbsent(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinuxExitGuardRejectsUnknownFamilyWithoutMutation(t *testing.T) {
	calls := 0
	guard, err := newLinuxExitGuard("exit0", 51820, func(context.Context, string, string, ...string) ([]byte, error) { calls++; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []api.ExitFamilyMode{"", "unknown"} {
		if guard.OpenTunnel(t.Context(), family) == nil || guard.Observe(t.Context(), family) == nil {
			t.Fatal("invalid family accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid family caused command")
	}
}

func TestLinuxExitGuardWrongAppliedFamilyRestoresBothFamilyContainment(t *testing.T) {
	var batches []string
	base := exitGuardReadbackRunner(t, "exit0", 51820, func(_ context.Context, input, _ string, _ ...string) ([]byte, error) {
		batches = append(batches, input)
		return nil, nil
	})
	guard, err := newLinuxExitGuard("exit0", 51820, base)
	if err != nil {
		t.Fatal(err)
	}
	first := true
	guard.run = func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		if input == "" && first {
			first = false
			return exitGuardFamilyReadbackFixture(guard.table, "exit0", 51820, true, api.ExitFamilyIPv6Only), nil
		}
		return base(ctx, input, name, args...)
	}
	if guard.OpenTunnel(t.Context(), api.ExitFamilyIPv4Only) == nil {
		t.Fatal("wrong applied family accepted")
	}
	if len(batches) != 2 || strings.Contains(batches[1], "meta nfproto") || strings.Contains(batches[1], `oifname "exit0" accept`) {
		t.Fatal("ambiguous apply did not restore closed containment")
	}
	if err := guard.ObserveContained(t.Context()); err != nil {
		t.Fatal(err)
	}
}
