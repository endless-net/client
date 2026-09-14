package client

import (
	"context"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceObservationClockInvalidatesTransitions(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	confirmed := false
	calls := 0
	s.ResourceEnforcementProvider = func(Config, time.Time) bool { calls++; return confirmed }
	if err := s.publishResourceClock(); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := sub.next(ctx); err != nil {
		t.Fatal(err)
	}
	for _, next := range []bool{true, false} {
		before := m.Metadata().Revision
		confirmed = next
		if err := s.publishResourceClock(); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("observation transition did not advance revision")
		}
		for {
			event, err := sub.next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if inv := event.GetInvalidated(); inv != nil {
				if inv.Domain != ipc.Domain_DOMAIN_RESOURCES || inv.ProfileId != profile.ProfileId || event.Metadata.Revision != before+1 {
					t.Fatal("wrong observation invalidation", event)
				}
				break
			}
		}
		if err := s.publishResourceClock(); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("unchanged observation published twice")
		}
	}
	beforeCalls := calls
	if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Name = "tampered"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.publishResourceClock(); err != nil {
		t.Fatal(err)
	}
	if calls != beforeCalls {
		t.Fatal("invalid source reached observation provider")
	}
}

func TestResourceObservationClockStopsWithHost(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	s.ResourceEnforcementProvider = func(Config, time.Time) bool { return false }
	ctx, cancel := context.WithCancel(t.Context())
	done := s.startResourceClock(ctx)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resource clock leaked")
	}
}
