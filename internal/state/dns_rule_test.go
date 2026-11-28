package state

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDNSRuleReceiptSurvivesRestartAndRejectsRetarget(t *testing.T) {
	s, backend, now := registeredStateFixture(t)
	change := func(fn func(*ConnectionState)) {
		t.Helper()
		if err := connectionChange(s, fn); err != nil {
			t.Fatal(err)
		}
	}
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) })
	a := AdapterRecord{ID: journalID, Name: "DNS fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}
	if err := s.RecordAdapterCreation(t.Context(), profileID, 1, a); err != nil {
		t.Fatal(err)
	}
	a.LUID = 7
	a.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	if err := s.RecordAdapterCreation(t.Context(), profileID, 1, a); err != nil {
		t.Fatal(err)
	}
	r := DNSRuleRecord{Key: filterKey, EffectStamp: EffectStamp{Phase: EffectPrepared, Fingerprint: strings.Repeat("b", 64)}}
	change(func(c *ConnectionState) { c.Journal.DNSRules = []DNSRuleRecord{r} })
	backend.failAfter = true
	if err := connectionChange(s, func(c *ConnectionState) { c.Journal.DNSRules[0].Phase = EffectApplied }); !errors.Is(err, ErrUncertain) {
		t.Fatal("lost write acknowledgement", err)
	}
	backend.failAfter = false
	var err error
	s, err = New(backend, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedConnection(t, s).Journal.DNSRules; len(got) != 1 || got[0].Phase != EffectApplied || got[0].Fingerprint != r.Fingerprint {
		t.Fatal("lost DNS receipt", got)
	}
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(time.Second)) })
	for _, fn := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Journal.DNSRules = nil },
		func(c *ConnectionState) { c.Journal.DNSRules[0].Key = profileID },
		func(c *ConnectionState) { c.Journal.DNSRules[0].Fingerprint = strings.Repeat("c", 64) },
		func(c *ConnectionState) {
			fresh := r
			fresh.Key = profileID
			c.Journal.DNSRules = append(c.Journal.DNSRules, fresh)
		},
	} {
		if err = connectionChange(s, fn); !errors.Is(err, ErrCorrupt) {
			t.Fatal("invalid receipt transition", err)
		}
	}
	change(func(c *ConnectionState) { c.Journal.DNSRules[0].Phase = EffectRemoving })
	change(func(c *ConnectionState) { c.Journal.DNSRules = nil })
}
