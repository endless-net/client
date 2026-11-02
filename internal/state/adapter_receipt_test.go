package state

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAdapterPreparationCannotRedispatchAfterLostACKOrRestart(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	record := AdapterRecord{ID: journalID, Name: "prepared adapter fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}
	b.failAfter = true
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, record); !errors.Is(err, ErrUncertain) {
		t.Fatal("lost preparation ACK claimed success", err)
	}
	b.failAfter = false
	restarted, err := New(b, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.RecordAdapterCreation(context.Background(), profileID, 1, record); !errors.Is(err, ErrStale) {
		t.Fatal("pending journal dispatched again", err)
	}
	record.LUID = 77
	record.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	b.failAfter = true
	if err := restarted.RecordAdapterCreation(context.Background(), profileID, 1, record); !errors.Is(err, ErrUncertain) {
		t.Fatal("lost receipt ACK claimed success", err)
	}
	b.failAfter = false
	if got := storedConnection(t, s).Journal.Adapter; got == nil || *got != record {
		t.Fatal("measured creation receipt was not preserved")
	}
	if err := restarted.RecordAdapterCreation(context.Background(), profileID, 1, record); err != nil {
		t.Fatal("same receipt identity changed", err)
	}
	record.LUID++
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, record); !errors.Is(err, ErrCorrupt) {
		t.Fatal("creation rebound to different LUID", err)
	}
}

func TestAdapterReceiptCannotAcknowledgeDifferentIntentOrInventJournal(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	prepared := AdapterRecord{ID: journalID, Name: "prepared adapter fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}
	receipt := prepared
	receipt.LUID = 77
	receipt.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, receipt); !errors.Is(err, ErrStale) {
		t.Fatal("receipt invented an unprepared journal", err)
	}
	if err := s.RecordAdapterCreation(context.Background(), profileID, 2, prepared); !errors.Is(err, ErrStale) {
		t.Fatal("preparation bypassed generation", err)
	}
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, prepared); err != nil {
		t.Fatal(err)
	}
	changed := receipt
	changed.Name = "another adapter"
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, changed); !errors.Is(err, ErrCorrupt) {
		t.Fatal("receipt changed prepared identity", err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(1)) }); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAdapterCreation(context.Background(), profileID, 1, receipt); !errors.Is(err, ErrStale) {
		t.Fatal("late native receipt acknowledged superseded intent", err)
	}
	if storedConnection(t, s).Journal.Adapter.LUID != 0 {
		t.Fatal("rejected result changed protected state")
	}
}
