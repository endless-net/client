package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestV4BootstrapMigrationPreservesPrivateState(t *testing.T) {
	store, backend, _ := registeredStateFixture(t)
	original, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy := original
	legacy.Version = 4
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	backend.data, err = store.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != CurrentVersion || next.Revision != original.Revision+1 || len(next.BootstrapCheckpoints) != 0 {
		t.Fatal("wrong migration")
	}
	next.Version, next.Revision = original.Version, original.Revision
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(next)
	if !bytes.Equal(want, got) {
		t.Fatal("migration changed keys/installation/attempt/connection")
	}
}

func TestV5DefaultTargetMigrationPreservesSignedCheckpoints(t *testing.T) {
	store, backend, _ := registeredStateFixture(t)
	original, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original.BootstrapCheckpoints = map[string]BootstrapCheckpoint{"https://control.example": {Revision: 13, Digest: [32]byte{9}}}
	legacy := original
	legacy.Version = 5
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	backend.data, err = store.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != CurrentVersion || next.Revision != original.Revision+1 {
		t.Fatal("wrong migration")
	}
	next.Version, next.Revision = original.Version, original.Revision
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(next)
	if !bytes.Equal(want, got) {
		t.Fatal("v5 migration changed keys/attempt/connection/checkpoint")
	}
}

func TestBootstrapAtomicFloorAndNoGenericUpdateRollback(t *testing.T) {
	store, _ := fixture(t)
	origin := "https://control.example"
	accept := func(revision uint64) error {
		return store.AcceptBootstrap(t.Context(), origin, func(previous BootstrapCheckpoint) (BootstrapCheckpoint, error) {
			if previous.Revision > revision {
				return BootstrapCheckpoint{}, ErrStale
			}
			return BootstrapCheckpoint{Revision: revision, Digest: [32]byte{byte(revision)}}, nil
		})
	}
	var wg sync.WaitGroup
	for revision := uint64(1); revision <= 16; revision++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := accept(revision)
			if err != nil && !errors.Is(err, ErrStale) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	saved, err := store.Load(t.Context())
	if err != nil || saved.BootstrapCheckpoints[origin].Revision != 16 {
		t.Fatal("concurrent acceptance lowered floor", err)
	}
	if err := store.Update(t.Context(), saved.Revision, func(s *State) error { delete(s.BootstrapCheckpoints, origin); return nil }); !errors.Is(err, ErrCorrupt) {
		t.Fatal("ordinary state update erased checkpoint", err)
	}
	if err := store.AcceptBootstrap(t.Context(), origin, func(previous BootstrapCheckpoint) (BootstrapCheckpoint, error) {
		previous.Digest[1] = 1
		return previous, nil
	}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("same revision changed digest", err)
	}
}

func TestBootstrapCommitFailureNeverConfirmsAcceptance(t *testing.T) {
	for _, lost := range []bool{false, true} {
		store, backend := fixture(t)
		backend.failBefore, backend.failAfter = !lost, lost
		checkpoint := BootstrapCheckpoint{Revision: 7, Digest: [32]byte{7}}
		err := store.AcceptBootstrap(t.Context(), "https://control.example", func(BootstrapCheckpoint) (BootstrapCheckpoint, error) { return checkpoint, nil })
		if !errors.Is(err, ErrUncertain) {
			t.Fatal("failed replace reported successful acceptance", err)
		}
		backend.failBefore, backend.failAfter = false, false
		saved, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if (saved.BootstrapCheckpoints["https://control.example"] == checkpoint) != lost {
			t.Fatal("commit failure lost known outcome")
		}
	}
}
