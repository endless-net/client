package state

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func guardReceiptFixture(t *testing.T) (*Store, *memoryBackend, FilterRecord) {
	t.Helper()
	s, b, now := registeredStateFixture(t)
	for _, change := range []func(*ConnectionState){
		func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) },
		func(c *ConnectionState) {
			c.Journal = &ResourceJournal{ID: journalID, Generation: c.Generation, Adapter: &AdapterRecord{ID: journalID, Name: "guard fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
		},
		func(c *ConnectionState) {
			c.Journal.Filters = []FilterRecord{{Key: filterKey, EffectStamp: EffectStamp{Phase: EffectPrepared, Fingerprint: strings.Repeat("a", 64)}, GuardAddress: "100.70.0.2", GuardInbound: true}}
		},
	} {
		if err := connectionChange(s, change); err != nil {
			t.Fatal(err)
		}
	}
	r := storedConnection(t, s).Journal.Filters[0]
	r.ID = 77
	return s, b, r
}

func TestGuardWriteAheadLostACKPreservesIdentityAndPreparedPhase(t *testing.T) {
	s, b, receipt := guardReceiptFixture(t)
	b.failAfter = true
	err := s.RecordPreparedGuardIDs(context.Background(), profileID, journalID, 1, []FilterRecord{receipt})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal("lost ACK reported success", err)
	}
	b.failAfter = false
	restarted, err := New(b, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedConnection(t, restarted).Journal.Filters[0]; got != receipt {
		t.Fatal("prepared kernel identity lost or phase promoted")
	}
	if err := s.RecordPreparedGuardIDs(context.Background(), profileID, journalID, 1, []FilterRecord{receipt}); err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { c.Journal.Filters[0].Phase = EffectApplied }); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPreparedGuardIDs(context.Background(), profileID, journalID, 1, []FilterRecord{receipt}); err != nil || storedConnection(t, s).Journal.Filters[0].Phase != EffectApplied {
		t.Fatal("stable ID write regressed phase", err)
	}
}

func TestGuardWriteAheadRejectsForeignOrChangedReceipt(t *testing.T) {
	for _, mutate := range []func(*FilterRecord){
		func(r *FilterRecord) { r.ID = 0 },
		func(r *FilterRecord) { r.Key = journalID },
		func(r *FilterRecord) { r.Fingerprint = strings.Repeat("b", 64) },
		func(r *FilterRecord) { r.GuardAddress = "100.70.0.3" },
		func(r *FilterRecord) { r.GuardInbound = false },
		func(r *FilterRecord) { r.GuardLocal = true },
		func(r *FilterRecord) { r.Phase = EffectApplied },
	} {
		s, _, receipt := guardReceiptFixture(t)
		mutate(&receipt)
		if err := s.RecordPreparedGuardIDs(context.Background(), profileID, journalID, 1, []FilterRecord{receipt}); !errors.Is(err, ErrCorrupt) {
			t.Fatal("changed receipt accepted", err)
		}
		if storedConnection(t, s).Journal.Filters[0].ID != 0 {
			t.Fatal("rejected write mutated state")
		}
	}
	s, _, receipt := guardReceiptFixture(t)
	write := func(generation uint64, journal string, records []FilterRecord) error {
		return s.RecordPreparedGuardIDs(context.Background(), profileID, journal, generation, records)
	}
	if err := write(1, journalID, []FilterRecord{receipt, receipt}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("duplicate receipt accepted", err)
	}
	if err := write(2, journalID, []FilterRecord{receipt}); !errors.Is(err, ErrStale) {
		t.Fatal("wrong intent generation accepted", err)
	}
	if err := write(1, filterKey, []FilterRecord{receipt}); !errors.Is(err, ErrStale) {
		t.Fatal("wrong journal accepted", err)
	}
	if err := write(1, journalID, []FilterRecord{receipt}); err != nil {
		t.Fatal(err)
	}
	receipt.ID++
	if err := write(1, journalID, []FilterRecord{receipt}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("kernel ID rebound", err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, c.AcceptedAt.Add(1)) }); err != nil {
		t.Fatal(err)
	}
	if err := write(1, journalID, []FilterRecord{receipt}); !errors.Is(err, ErrStale) {
		t.Fatal("late receipt authorized disconnected intent", err)
	}
}
