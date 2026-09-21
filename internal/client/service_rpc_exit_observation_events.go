package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// This worker-local digest is only change detection, never evidence for Get.
// Fresh reads continue to authenticate and observe the native source themselves.
type clientRPCExitObservationEvents struct {
	source   *clientRPCExitObservationSource
	previous *[sha256.Size]byte
}

func exitObservationEventFingerprint(cfg Config, observed *ipc.ExitNodeStatus, observationErr error) ([sha256.Size]byte, error) {
	profile := cfg.RPCState.ActiveProfileID
	confirmed := observationErr == nil && exitAppliedResultMatches(&clientRPCExitChange{ProfileID: profile, Requested: cfg.ExitSelection}, observed)
	// The result validator fixes every actual field of a successful observation.
	// Every failed/unavailable observation has the same public unknown projection.
	// Metadata, elapsed time and private native errors must never cause events.
	raw, err := json.Marshal([]any{cfg.LocalOwnerID, profile, cfg.NodeID, cfg.NetworkID, cfg.ExitSelection, confirmed})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func (s *ClientRPCService) publishExitObservation(ctx context.Context, state *clientRPCExitObservationEvents) error {
	source := state.source
	if ctx.Err() != nil || source == nil || source.ctx.Err() != nil || source.lock == nil || source.observe == nil {
		return nil
	}
	// Busy runtime effects are not evidence of a transition. Retry the next tick.
	if !source.lock.TryLock() {
		return nil
	}
	defer source.lock.Unlock()
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(source.ctx, cancel)
	defer stop()
	s.exitMu.Lock()
	if s.exitObservation != source || source.ctx.Err() != nil {
		s.exitMu.Unlock()
		return nil
	}
	m := s.mutations
	m.mu.Lock()
	s.exitMu.Unlock()
	cfg := m.store.Read()
	m.mu.Unlock()
	if cfg.RPCState == nil || cfg.RPCState.ActiveProfileID == "" || cfg.RPCState.ExitChange != nil {
		state.previous = nil
		return nil
	}
	if check.Err() != nil {
		return nil
	}
	observed, observationErr := source.observe(check, clonePersistentConfig(cfg))
	// Cancellation of the source/worker discards results. An observation timeout
	// with a still-live source is unavailable enforcement, not cached success.
	s.exitMu.Lock()
	if s.exitObservation != source || source.ctx.Err() != nil || ctx.Err() != nil {
		s.exitMu.Unlock()
		return nil
	}
	m.mu.Lock()
	s.exitMu.Unlock()
	defer m.mu.Unlock()
	if !reflect.DeepEqual(cfg, m.store.Read()) {
		return nil
	}
	if check.Err() != nil {
		observationErr = check.Err()
	}
	fingerprint, err := exitObservationEventFingerprint(cfg, observed, observationErr)
	if err != nil {
		return err
	}
	// A subscriber may already have read unknown enforcement before this first
	// poll (or while an operation was pending). Newly confirmed evidence must
	// prompt that consumer to refetch even without a previous polling digest.
	confirmed := observationErr == nil && exitAppliedResultMatches(&clientRPCExitChange{ProfileID: cfg.RPCState.ActiveProfileID, Requested: cfg.ExitSelection}, observed)
	if state.previous == nil && !confirmed {
		state.previous = &fingerprint
		return nil
	}
	if state.previous != nil && *state.previous == fingerprint {
		return nil
	}
	persistent := clonePersistentConfig(cfg)
	if err := m.store.Update(func(next *Config) error {
		// Read attaches caller-only store metadata; Update receives only the
		// persistent document. Compare the same representation on both sides.
		if ctx.Err() != nil || source.ctx.Err() != nil || !reflect.DeepEqual(persistent, *next) {
			return errRPCNoChange
		}
		next.RPCState.Revision++
		return nil
	}); err != nil {
		if errors.Is(err, errRPCNoChange) {
			return nil
		}
		return err
	}
	state.previous = &fingerprint
	m.publishMutationLocked(nil, ipc.Domain_DOMAIN_EXIT_NODE)
	return nil
}

func (s *ClientRPCService) startExitObservationEvents(ctx context.Context, source *clientRPCExitObservationSource) func() {
	if source == nil {
		return func() {}
	}
	ticker := time.NewTicker(time.Second)
	return s.startExitObservationEventTicks(ctx, source, ticker.C, ticker.Stop)
}

func (s *ClientRPCService) startExitObservationEventTicks(ctx context.Context, source *clientRPCExitObservationSource, ticks <-chan time.Time, stopTicks func()) func() {
	lifetime, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if stopTicks != nil {
			defer stopTicks()
		}
		state := &clientRPCExitObservationEvents{source: source}
		// A failed persistence attempt retains the previous digest and retries.
		_ = s.publishExitObservation(lifetime, state)
		for {
			select {
			case <-lifetime.Done():
				return
			case _, ok := <-ticks:
				if !ok {
					return
				}
				_ = s.publishExitObservation(lifetime, state)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(cancel); <-done }
}
