package state

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPreparedAddressCanCaptureIntrinsicRouteReceiptAfterDisconnect(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	change := func(fn func(*ConnectionState)) {
		t.Helper()
		if err := connectionChange(s, fn); err != nil {
			t.Fatal(err)
		}
	}
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) })
	change(func(c *ConnectionState) {
		c.Journal = &ResourceJournal{ID: journalID, Generation: c.Generation, Adapter: &AdapterRecord{ID: journalID, Name: "receipt fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	})
	change(func(c *ConnectionState) {
		c.Journal.Adapter.LUID = 77
		c.Journal.Adapter.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	})
	change(func(c *ConnectionState) {
		c.Journal.Addresses = []AddressRecord{{Prefix: "100.70.0.1/32", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	})
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(time.Second)) })
	// The OS may report its intrinsic local route with the original address
	// receipt after cancellation. This is cleanup evidence, not a new effect.
	b.failAfter = true
	err := connectionChange(s, func(c *ConnectionState) {
		a := &c.Journal.Addresses[0]
		a.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("b", 64)}
		a.AutomaticRoute = &RouteRecord{Destination: a.Prefix, NextHop: "0.0.0.0", Metric: 9, EffectStamp: EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("c", 64)}}
	})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal("lost receipt commit reported success", err)
	}
	b.failAfter = false
	restarted, err := New(b, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	c := storedConnection(t, restarted)
	if c.Intent != IntentDisconnected || c.Generation != 2 || c.Journal.Generation != 1 || c.Journal.Addresses[0].AutomaticRoute == nil || c.Journal.Addresses[0].AutomaticRoute.Metric != 9 {
		t.Fatal("receipt changed intent or was lost across restart")
	}
	for _, mutate := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Journal.Addresses[0].AutomaticRoute = nil },
		func(c *ConnectionState) { c.Journal.Addresses[0].AutomaticRoute.Metric++ },
		func(c *ConnectionState) { c.Journal.Addresses[0].AutomaticRoute.Destination = "100.70.0.2/32" },
		func(c *ConnectionState) { c.Journal.Addresses[0].AutomaticRoute.Fingerprint = strings.Repeat("d", 64) },
		func(c *ConnectionState) { c.Journal.Addresses[0].Phase = EffectRemoving },
		func(c *ConnectionState) {
			c.Journal.Addresses = append(c.Journal.Addresses, AddressRecord{Prefix: "100.70.0.2/32", EffectStamp: EffectStamp{Phase: EffectPrepared}})
		},
	} {
		if err := connectionChange(s, mutate); !errors.Is(err, ErrCorrupt) {
			t.Fatal("intrinsic receipt bypassed ownership/intent fence", err)
		}
	}
	change(func(c *ConnectionState) {
		a := &c.Journal.Addresses[0]
		a.Phase = EffectRemoving
		a.AutomaticRoute.Phase = EffectRemoving
	})
	value, err := restarted.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value.Profiles[profileID].Connection.Journal.Addresses[0].AutomaticRoute.Phase != EffectRemoving {
		t.Fatal("cleanup phase lost")
	}
}
