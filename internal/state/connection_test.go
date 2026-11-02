package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
)

const journalID = "a47f5341-4c8a-443b-a498-8e632369d6a7"
const filterKey = "d605f76b-bae3-4a4c-81c3-3e035d8ee644"

func registeredStateFixture(t *testing.T) (*Store, *memoryBackend, time.Time) {
	t.Helper()
	s, b := fixture(t)
	if err := s.Update(context.Background(), 1, addAttempt); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 10, 1, 0, 0, time.UTC)
	if err := s.Update(context.Background(), 2, func(value *State) error {
		p := value.Profiles[profileID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID = "account"
		p.Attempt.NetworkID = "network"
		p.Attempt.NodeID = "node"
		p.Attempt.CredentialEnds = now.Add(time.Hour)
		p.Secrets.RegistrationID = "b8c8f430-ce98-444d-84c5-61dcd346b4b5"
		p.Secrets.RegistrationRequest = "original-request"
		p.Secrets.NodeCredential = "enc_original-credential"
		value.Profiles[profileID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, b, now
}

func connectionChange(s *Store, change func(*ConnectionState)) error {
	value, err := s.Load(context.Background())
	if err != nil {
		return err
	}
	return s.Update(context.Background(), value.Revision, func(next *State) error {
		p := next.Profiles[profileID]
		if p.Connection == nil {
			p.Connection = &ConnectionState{}
		}
		change(p.Connection)
		next.Profiles[profileID] = p
		return nil
	})
}

func storedConnection(t *testing.T, s *Store) *ConnectionState {
	t.Helper()
	value, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return value.Profiles[profileID].Connection
}

func TestConnectionIntentSurvivesLostACKAndRestartWithoutExtendingDeadline(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	original, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b.failAfter = true
	err = connectionChange(s, func(c *ConnectionState) {
		if changed, err := c.AcceptIntent(IntentConnected, now); err != nil || !changed {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrUncertain) {
		t.Fatal("lost write ACK reported success", err)
	}
	b.failAfter = false
	restarted, err := New(b, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	current := storedConnection(t, restarted)
	if current.Intent != IntentConnected || current.Generation != 1 || !current.Deadline.Equal(now.Add(30*time.Second)) {
		t.Fatal("restart lost original intent/deadline")
	}
	if changed, err := current.AcceptIntent(IntentConnected, now.Add(time.Hour)); err != nil || changed || !current.Deadline.Equal(now.Add(30*time.Second)) {
		t.Fatal("duplicate Connect created a new attempt/deadline", err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(time.Second)) }); err != nil {
		t.Fatal(err)
	}
	current = storedConnection(t, restarted)
	if current.Intent != IntentDisconnected || current.Generation != 2 || !current.Deadline.Equal(now.Add(11*time.Second)) {
		t.Fatal("Disconnect intent/budget lost")
	}
	// An offline announcement also needs a new persisted generation. The
	// counter is identity/replay state, never authorization for online access.
	if err := connectionChange(s, func(c *ConnectionState) {
		if _, err := c.NextEndpointGeneration(); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal("offline announcement generation unavailable", err)
	}
	if current := storedConnection(t, s); current.Intent != IntentDisconnected || current.EndpointGeneration != 1 {
		t.Fatal("offline generation changed intent")
	}
	after, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeProfile, afterProfile := original.Profiles[profileID], after.Profiles[profileID]
	if !bytes.Equal(beforeProfile.Keys.IdentityPrivate, afterProfile.Keys.IdentityPrivate) || !bytes.Equal(beforeProfile.Keys.WireGuardPrivate, afterProfile.Keys.WireGuardPrivate) || beforeProfile.Secrets != afterProfile.Secrets || *beforeProfile.Attempt != *afterProfile.Attempt {
		t.Fatal("connection intent changed registration or key material")
	}
	if bytes.Contains(b.data, []byte(afterProfile.Secrets.NodeCredential)) || bytes.Contains(b.data, afterProfile.Keys.WireGuardPrivate) {
		t.Fatal("connection state weakened storage protection")
	}
}

func TestJournalPreservesOwnUncertainEffectsAcrossDisconnectAndCleanup(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	change := func(fn func(*ConnectionState)) {
		t.Helper()
		if err := connectionChange(s, fn); err != nil {
			t.Fatal(err)
		}
	}
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) })
	change(func(c *ConnectionState) {
		c.Journal = &ResourceJournal{ID: journalID, Generation: c.Generation, Adapter: &AdapterRecord{ID: journalID, Name: "EndlessNet fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	})
	change(func(c *ConnectionState) {
		c.Journal.Adapter.LUID = 77
		c.Journal.Adapter.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	})
	change(func(c *ConnectionState) {
		c.Journal.Addresses = []AddressRecord{{Prefix: "100.70.0.1/32", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
		c.Journal.Routes = []RouteRecord{{Destination: "100.70.0.2/32", NextHop: "0.0.0.0", Metric: 10, EffectStamp: EffectStamp{Phase: EffectPrepared}}}
		c.Journal.Filters = []FilterRecord{{Key: filterKey, EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	})
	change(func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentDisconnected, now.Add(time.Second)) })
	current := storedConnection(t, s)
	if current.Generation != 2 || current.Journal.Generation != 1 || current.Journal.Adapter == nil || len(current.Journal.Routes) != 1 {
		t.Fatal("Disconnect erased pending effects")
	}
	// A late OS result is an observation of the prepared effect; it does not
	// restore intent or authorize new effects after Disconnect.
	change(func(c *ConnectionState) {
		c.Journal.Addresses[0].EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("b", 64)}
		c.Journal.Routes[0].EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("c", 64)}
		c.Journal.Filters[0].ID = 81
		c.Journal.Filters[0].EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("d", 64)}
	})
	for _, fn := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Journal = nil },
		func(c *ConnectionState) { c.Journal.Routes = nil },
		func(c *ConnectionState) { c.Journal.Adapter.LUID = 88 },
		func(c *ConnectionState) { c.Journal.Routes[0].Metric = 99 },
		func(c *ConnectionState) { c.Journal.Filters[0].ID = 82 },
		func(c *ConnectionState) { c.Journal.Routes[0].Fingerprint = strings.Repeat("e", 64) },
		func(c *ConnectionState) {
			c.Journal.Routes = append(c.Journal.Routes, RouteRecord{Destination: "100.70.0.3/32", NextHop: "0.0.0.0", EffectStamp: EffectStamp{Phase: EffectPrepared}})
		},
	} {
		if err := connectionChange(s, fn); !errors.Is(err, ErrCorrupt) {
			t.Fatal("journal ownership/effects could be forgotten or changed", err)
		}
	}
	change(func(c *ConnectionState) {
		c.Journal.Addresses[0].Phase = EffectRemoving
		c.Journal.Routes[0].Phase = EffectRemoving
		c.Journal.Filters[0].Phase = EffectRemoving
	})
	change(func(c *ConnectionState) {
		c.Journal.Addresses = nil
		c.Journal.Routes = nil
		c.Journal.Filters = nil
		c.Journal.Adapter.Phase = EffectRemoving
	})
	change(func(c *ConnectionState) { c.Journal.Adapter = nil })
	change(func(c *ConnectionState) { c.Journal = nil })
	current = storedConnection(t, s)
	if current.Journal != nil || current.Intent != IntentDisconnected || current.Generation != 2 {
		t.Fatal("cleanup changed latest desired intent")
	}
}

func TestConnectionMutationRejectsRollbackAndUnscopedResources(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.NextEndpointGeneration() }); err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func(*ConnectionState){
		func(c *ConnectionState) { c.Generation = 0 }, func(c *ConnectionState) { c.Generation++ },
		func(c *ConnectionState) { c.Deadline = c.Deadline.Add(time.Second) }, func(c *ConnectionState) { c.EndpointGeneration = 0 }, func(c *ConnectionState) { c.EndpointGeneration += 2 },
	} {
		if err := connectionChange(s, fn); !errors.Is(err, ErrCorrupt) {
			t.Fatal("connection epoch/lease counter rolled back or skipped", err)
		}
	}
	if err := connectionChange(s, func(c *ConnectionState) {
		c.Journal = &ResourceJournal{ID: journalID, Generation: c.Generation, Adapter: &AdapterRecord{ID: journalID, Name: "fixture", EffectStamp: EffectStamp{Phase: EffectPrepared}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := connectionChange(s, func(c *ConnectionState) {
		c.Journal.Adapter.LUID = 77
		c.Journal.Adapter.EffectStamp = EffectStamp{Phase: EffectApplied, Fingerprint: strings.Repeat("a", 64)}
	}); err != nil {
		t.Fatal(err)
	}
	for _, route := range []RouteRecord{
		{Destination: "0.0.0.0/0", NextHop: "0.0.0.0"}, {Destination: "100.70.0.0/24", NextHop: "0.0.0.0"}, {Destination: "fd00::1/128", NextHop: "0.0.0.0"}, {Destination: "100.70.0.2/32", NextHop: "192.0.2.1"},
	} {
		route.Phase = EffectPrepared
		if err := connectionChange(s, func(c *ConnectionState) { c.Journal.Routes = []RouteRecord{route} }); !errors.Is(err, ErrCorrupt) {
			t.Fatal("non-device routing effect accepted", err)
		}
	}
}

func TestConnectionCountersKeepUint64PrecisionAndExhaustionFailsClosed(t *testing.T) {
	const large = uint64(9007199254740993)
	c := ConnectionState{Intent: IntentConnected, Generation: large, EndpointGeneration: large, AcceptedAt: time.Now()}
	c.Deadline = c.AcceptedAt.Add(30 * time.Second)
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var restored ConnectionState
	if json.Unmarshal(raw, &restored) != nil || restored.Generation != large || restored.EndpointGeneration != large {
		t.Fatal("uint64 journal counter rounded")
	}
	if generation, err := restored.NextEndpointGeneration(); err != nil || generation != large+1 {
		t.Fatal("endpoint generation lost precision", err)
	}
	if _, err := restored.AcceptIntent(IntentDisconnected, c.AcceptedAt.Add(time.Second)); err != nil || restored.Generation != large+1 {
		t.Fatal("intent generation lost precision", err)
	}
	restored.Generation = ^uint64(0)
	if _, err := restored.AcceptIntent(IntentConnected, c.AcceptedAt.Add(2*time.Second)); !errors.Is(err, ErrStale) || restored.Intent != IntentDisconnected {
		t.Fatal("exhausted intent counter wrapped", err)
	}
	restored.Intent = IntentConnected
	restored.EndpointGeneration = ^uint64(0)
	if _, err := restored.NextEndpointGeneration(); !errors.Is(err, ErrStale) || restored.EndpointGeneration != ^uint64(0) {
		t.Fatal("exhausted endpoint counter wrapped", err)
	}
}

func TestDirectUpgradePreservesRegistrationAndDoesNotInitializeOrReset(t *testing.T) {
	s, b, _ := registeredStateFixture(t)
	original, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy := original
	legacy.Version = 1
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	b.data, err = s.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatal("legacy runtime fallback added")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Upgrade(context.Background()) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	upgraded, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Version != CurrentVersion || upgraded.Revision != original.Revision+1 || upgraded.Profiles[profileID].Connection != nil {
		t.Fatal("migration invented intent or repeated revision")
	}
	upgraded.Version = original.Version
	upgraded.Revision = original.Revision
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(upgraded)
	if !bytes.Equal(want, got) {
		t.Fatal("migration changed identity, secrets or enrollment")
	}
	cipher := append([]byte(nil), b.data...)
	if err := s.Upgrade(context.Background()); err != nil || !bytes.Equal(cipher, b.data) {
		t.Fatal("current state resealed/reset", err)
	}
	b.data = nil
	if err := s.Upgrade(context.Background()); !errors.Is(err, ErrMissing) || b.data != nil {
		t.Fatal("migration initialized missing state", err)
	}
}

func TestUpgradeLostACKCorruptionAndUnknownVersionNeverEraseData(t *testing.T) {
	for _, name := range []string{"lost-before", "lost-after", "unknown", "overflow", "foreign-installation", "legacy-with-intent"} {
		t.Run(name, func(t *testing.T) {
			s, b, now := registeredStateFixture(t)
			legacy, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			legacy.Version = 1
			switch name {
			case "unknown":
				legacy.Version = 99
			case "overflow":
				legacy.Revision = ^uint64(0)
			case "foreign-installation":
				legacy.InstallationID = profileID
			case "legacy-with-intent":
				p := legacy.Profiles[profileID]
				p.Connection = &ConnectionState{}
				_, _ = p.Connection.AcceptIntent(IntentConnected, now)
				legacy.Profiles[profileID] = p
			}
			raw, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			b.data, err = s.protector.Seal(raw)
			if err != nil {
				t.Fatal(err)
			}
			before := append([]byte(nil), b.data...)
			b.failBefore = name == "lost-before"
			b.failAfter = name == "lost-after"
			err = s.Upgrade(context.Background())
			if name == "lost-before" || name == "lost-after" {
				if !errors.Is(err, ErrUncertain) {
					t.Fatal("uncertain migration reported success", err)
				}
				b.failBefore, b.failAfter = false, false
				if err := s.Upgrade(context.Background()); err != nil {
					t.Fatal("migration could not recover exact committed state", err)
				}
				result, err := s.Load(context.Background())
				if err != nil || result.Revision != legacy.Revision+1 {
					t.Fatal("uncertain migration repeated/reset", err)
				}
			} else if !errors.Is(err, ErrCorrupt) || !bytes.Equal(before, b.data) {
				t.Fatal("invalid migration source rewritten", err)
			}
		})
	}
}
