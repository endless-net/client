package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxDNSReceiptPreservesOwnershipAndRetry(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("protected Linux state requires root")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := NewLinuxNetworkLedger(directory)
	// A prior session may have written the original table-only format.
	legacy := []byte(`{"Table":"en017abcdef012","Owner":"487fb261-c50e-4fad-8e3a-47622509959b"}`)
	if err := os.WriteFile(filepath.Join(directory, linuxNetworkFile), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	previous, err := ledger.Read(ctx)
	if err != nil || previous.DNSIndex != 0 || previous.Table != "en017abcdef012" {
		t.Fatal("original receipt", previous, err)
	}
	next := previous
	next.DNSIndex, next.DNSNewPort = 5, 41235
	if err := ledger.Replace(ctx, previous, next); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Replace(ctx, previous, next); err != nil {
		t.Fatal("retry", err)
	}
	replacement := next
	replacement.ExitRule = true
	replacement.DNSOldPort, replacement.DNSNewPort = next.DNSNewPort, 41236
	if err := ledger.Replace(ctx, previous, replacement); !errors.Is(err, ErrStale) {
		t.Fatal("stale writer", err)
	}
	foreign := next
	foreign.Owner = "b8c8f430-ce98-444d-84c5-61dcd346b4b5"
	if err := ledger.Replace(ctx, next, foreign); !errors.Is(err, ErrCorrupt) {
		t.Fatal("changed owner", err)
	}
	if err := ledger.Retire(ctx, previous); !errors.Is(err, ErrStale) {
		t.Fatal("lost resolver receipt", err)
	}
	if err := ledger.Replace(ctx, next, replacement); err != nil {
		t.Fatal(err)
	}
	if got, err := ledger.Read(ctx); err != nil || got != replacement {
		t.Fatal(got, err)
	}
	reopened := NewLinuxNetworkLedger(directory)
	withdrawn := replacement
	withdrawn.ExitRule = false
	if err := reopened.Replace(ctx, replacement, withdrawn); err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.Read(ctx); err != nil || got != withdrawn || got.DNSNewPort != 41236 {
		t.Fatal("withdrawing exit lost independent DNS effects", got, err)
	}
}
