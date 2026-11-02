//go:build windows

package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wireguard-go/device"
)

// Path callbacks are injected in these native-owner tests. Actual cryptography
// and UDP delivery are tested separately by the engine's transport fixtures.
func injectSessionPath(t *testing.T, s *WindowsSession) {
	t.Helper()
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.engine.ProbeDirect(probe); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.current.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	key := peerKey(t, snapshot.Peers[0].PublicKey)
	tracker := s.engine.direct
	tracker.session(key, device.PeerSessionEstablished)
	tracker.currentKey(key)
	config := tracker.config.Load()
	tracker.authenticated([32]byte(key), config.peers[[32]byte(key)].endpoint, config, time.Now().UTC())
}

func TestWindowsDirectWaitFencesCurrentIntentAndLeavesCompletionToWorker(t *testing.T) {
	s, store, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	operation, b := sessionFixtureOperation(t, s, parent, false)
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	injectSessionPath(t, s)
	if err := s.WaitDirect(operation, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !b.Active() {
		t.Fatal("native path observation completed operation without worker fence")
	}
	value, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), value.Revision, func(next *state.State) error {
		p := next.Profiles[cleanupProfile]
		_, err := beginSessionDisconnect(next, &p)
		next.Profiles[cleanupProfile] = p
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitDirect(operation, time.Millisecond); !errors.Is(err, state.ErrStale) || s.lease.Permissions().ExistingFlows {
		t.Fatal("late observed path ignored newer Disconnect", err)
	}
}

func TestWindowsDirectWaitDoesNotBlockConcurrentStop(t *testing.T) {
	s, _, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	operation, _ := sessionFixtureOperation(t, s, parent, false)
	if err := s.Start(operation, m); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.WaitDirect(operation, time.Millisecond) }()
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stopped path wait claimed success")
		}
	case <-parent.Done():
		t.Fatal("path wait held native mutex across network wait")
	}
	if s.lease.Permissions().ExistingFlows {
		t.Fatal("Stop left authority active")
	}
}
