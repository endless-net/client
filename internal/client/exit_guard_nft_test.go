package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
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
	guard, err := newLinuxExitGuard("endlessnet", 51820, runner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := guard.Contain(ctx); err != nil {
		t.Fatal(err)
	}
	closed := batches[0]
	for _, want := range []string{
		"add table inet " + guard.table,
		"hook output priority 0; policy drop;",
		"hook forward priority 0; policy drop;",
		"output oifname \"lo\" accept",
		"output meta mark 51820 meta l4proto udp accept",
	} {
		if !strings.Contains(closed, want) {
			t.Fatalf("missing guard constraint: %s", want)
		}
	}
	for _, forbidden := range []string{"flush ruleset", "delete table", "ct state", "ip daddr", "ip6 daddr", "oifname \"endlessnet\""} {
		if strings.Contains(closed, forbidden) {
			t.Fatalf("unexpected containment bypass: %s", forbidden)
		}
	}
	if strings.Count(closed, " accept\n") != 2 {
		t.Fatal("unexpected direct-traffic exemption")
	}
	if err := guard.OpenTunnel(ctx); err != nil {
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
	reopened, err := newLinuxExitGuard("endlessnet", 51820, runner)
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
	if _, err := newLinuxExitGuard("endlessnet", 0, runner); err == nil {
		t.Fatal("zero mark exempts ordinary unmarked traffic")
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
	for _, action := range []func(context.Context) error{guard.Contain, guard.OpenTunnel, guard.Release} {
		if !errors.Is(action(ctx), context.Canceled) {
			t.Fatal("cancelled operation was accepted")
		}
	}
	if calls != 0 {
		t.Fatal("pre-cancelled command had native effects")
	}
	ctx, cancel = context.WithCancel(context.Background())
	guard.run = func(context.Context, string, string, ...string) ([]byte, error) {
		calls++
		cancel()
		return nil, nil
	}
	if !errors.Is(guard.OpenTunnel(ctx), context.Canceled) || calls != 1 {
		t.Fatal("late cancellation must retain ambiguous effects without cleanup")
	}
}
