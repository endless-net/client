package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type hintLookup struct {
	epoch     uint64
	value     connectivity.Map
	context   string
	preferred []state.TransportHintKey
	err       error
}

func (hintLookup) String() string   { return "[private transport hint lookup]" }
func (hintLookup) GoString() string { return "[private transport hint lookup]" }

// transportHintRuntime is private installation metadata I/O. Neither queued
// changes nor lookup results contain authority to activate a transport.
type transportHintRuntime struct {
	mu         sync.Mutex
	ctx        context.Context
	done, wake chan struct{}
	store      *state.Store
	clock      connectivity.Clock
	lease      *connectivity.Lease
	profile    string
	generation uint64
	private    []byte
	source     func(context.Context, []byte) (string, error)
	notify     func()
	value      connectivity.Map
	epoch      uint64
	pending    map[state.TransportHintKey]*state.TransportHint
	reset      bool
	result     hintLookup
}

func (*transportHintRuntime) String() string   { return "[private transport hint worker]" }
func (*transportHintRuntime) GoString() string { return "[private transport hint worker]" }

func newTransportHintRuntime(ctx context.Context, store *state.Store, clock connectivity.Clock, lease *connectivity.Lease, profile string, generation uint64, private []byte, source func(context.Context, []byte) (string, error), notify func()) *transportHintRuntime {
	h := &transportHintRuntime{ctx: ctx, done: make(chan struct{}), wake: make(chan struct{}, 1), store: store, clock: clock, lease: lease, profile: profile, generation: generation, private: private, source: source, notify: notify, pending: make(map[state.TransportHintKey]*state.TransportHint)}
	go h.run()
	return h
}

func (h *transportHintRuntime) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *transportHintRuntime) request(value connectivity.Map) uint64 {
	h.mu.Lock()
	if h.epoch == ^uint64(0) {
		h.mu.Unlock()
		return 0
	}
	h.value = value
	h.epoch++
	epoch := h.epoch
	h.mu.Unlock()
	h.signal()
	return epoch
}

func (h *transportHintRuntime) lookup(epoch uint64, value connectivity.Map) (hintLookup, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if epoch == 0 || h.result.epoch < epoch || h.result.value.Cursor() != value.Cursor() {
		return hintLookup{}, false
	}
	result := h.result
	result.preferred = append([]state.TransportHintKey(nil), result.preferred...)
	return result, true
}

func (h *transportHintRuntime) change(key state.TransportHintKey, value *state.TransportHint) {
	h.mu.Lock()
	if len(h.pending) == state.MaxTransportHints {
		if _, exists := h.pending[key]; !exists {
			h.pending = make(map[state.TransportHintKey]*state.TransportHint)
			h.reset = true // Lost invalidations can never leave an old priority.
		}
	}
	h.pending[key] = value
	h.mu.Unlock()
	h.signal()
}

func hintKeys(value connectivity.Map, profile, contextDigest string, now time.Time) ([]state.TransportHintKey, error) {
	if contextDigest == "" {
		return nil, nil
	}
	if !transport.ValidDigest(contextDigest) || value.Denied() {
		return nil, ErrNoAuthority
	}
	snapshot, err := value.Snapshot()
	if err != nil {
		return nil, err
	}
	var keys []state.TransportHintKey
	for _, peer := range snapshot.Peers {
		if peer.Transport == nil || !pairGranted(peer) || !coordinator.TransportPlanCurrent(*peer.Transport, now) {
			continue
		}
		for _, offer := range peer.Transport.Profiles {
			keys = append(keys, state.TransportHintKey{LocalProfileID: profile, AccountID: snapshot.Network.AccountID, NetworkID: snapshot.Network.ID, LocalNodeID: snapshot.Node.ID, PeerNodeID: peer.ID, LocalPublicKey: snapshot.Node.PublicKey, PeerPublicKey: peer.PublicKey, ContextDigest: contextDigest, Engine: offer.Profile.Engine, ProtocolVersion: offer.Profile.ProtocolVersion, TransportProfileID: offer.Profile.ID, ProfileDigest: offer.Digest, ProfileVersion: offer.Profile.Version})
		}
	}
	return keys, nil
}

func (h *transportHintRuntime) current(value connectivity.Map) bool {
	return h.ctx.Err() == nil && h.lease.MatchesMap(value) && h.lease.Permissions().NewFlows && value.IntentGeneration() == h.generation
}

func clearHintStoreKeys(value state.State) {
	for _, p := range value.Profiles {
		clear(p.Keys.IdentityPrivate)
		clear(p.Keys.WireGuardPrivate)
	}
}

func (h *transportHintRuntime) operate(ctx context.Context, value connectivity.Map, changes map[state.TransportHintKey]*state.TransportHint, reset bool) (hintLookup, error) {
	result := hintLookup{value: value}
	if !h.current(value) {
		return result, ErrNoAuthority
	}
	digest, err := h.source(ctx, h.private)
	if err != nil {
		digest = ""
	}
	if digest != "" && !transport.ValidDigest(digest) {
		return result, ErrNoAuthority
	}
	for ctx.Err() == nil {
		preferred, err := func() ([]state.TransportHintKey, error) {
			stored, err := h.store.Load(ctx)
			if err != nil {
				return nil, err
			}
			defer clearHintStoreKeys(stored)
			p := stored.Profiles[h.profile]
			if !h.current(value) || stored.ActiveProfileID != h.profile || p.Connection == nil || p.Connection.Intent != state.IntentConnected || p.Connection.Generation != h.generation {
				return nil, ErrNoAuthority
			}
			// An empty cache has no prior success or durable clock watermark to
			// renew. Merely discovering capabilities must not write State.
			if len(stored.TransportHints) == 0 && len(changes) == 0 && !reset {
				return nil, nil
			}
			var preferred []state.TransportHintKey
			err = h.store.Update(ctx, stored.Revision, func(next *state.State) error {
				p := next.Profiles[h.profile]
				if !h.current(value) || next.ActiveProfileID != h.profile || p.Connection == nil || p.Connection.Intent != state.IntentConnected || p.Connection.Generation != h.generation {
					return ErrNoAuthority
				}
				sample, err := h.clock.Sample()
				if err != nil {
					return err
				}
				keys, err := hintKeys(value, h.profile, digest, sample.Wall)
				if err != nil {
					return err
				}
				allowed := make(map[state.TransportHintKey]bool, len(keys))
				for _, key := range keys {
					allowed[key] = true
				}
				if reset {
					if _, err := next.ReconcileTransportHints(h.profile, "", nil, sample.Wall); err != nil {
						return err
					}
				}
				if _, err := next.ReconcileTransportHints(h.profile, digest, keys, sample.Wall); err != nil {
					return err
				}
				for key, success := range changes {
					if key.LocalProfileID != h.profile {
						return state.ErrCorrupt
					}
					if success == nil {
						next.ForgetTransportHint(key)
						continue
					}
					if allowed[key] && !sample.Wall.Before(success.SucceededAt) && sample.Wall.Before(success.ExpiresAt) && !sample.Wall.Before(next.HintObservedAt) {
						if err := next.RecordTransportSuccess(*success, sample.Wall); err != nil {
							return err
						}
					}
				}
				preferred, err = next.ReconcileTransportHints(h.profile, digest, keys, sample.Wall)
				return err
			})
			return preferred, err
		}()
		if err == nil {
			if !h.current(value) {
				return result, ErrNoAuthority
			}
			result.context, result.preferred = digest, preferred
			return result, nil
		}
		if !h.current(value) || !errors.Is(err, state.ErrStale) && !errors.Is(err, state.ErrUncertain) {
			return result, err
		}
	}
	return result, ctx.Err()
}

func (h *transportHintRuntime) run() {
	defer close(h.done)
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.wake:
		}
		h.mu.Lock()
		value, epoch, changes, reset := h.value, h.epoch, h.pending, h.reset
		h.pending, h.reset = make(map[state.TransportHintKey]*state.TransportHint), false
		h.mu.Unlock()
		ctx, cancel := context.WithTimeout(h.ctx, time.Duration(transport.MaxProbeMillis)*time.Millisecond)
		result, err := h.operate(ctx, value, changes, reset)
		cancel()
		result.epoch, result.err = epoch, err
		h.mu.Lock()
		if err != nil {
			// Keep failed invalidations until a subsequent lookup/write can commit.
			// A failed lookup exposes no priority in the meantime.
			if reset || len(h.pending)+len(changes) > state.MaxTransportHints {
				h.reset = true
			}
			for key, change := range changes {
				if len(h.pending) == state.MaxTransportHints {
					h.reset = true
					break
				}
				if _, newer := h.pending[key]; !newer {
					h.pending[key] = change
				}
			}
		}
		h.result = result
		h.mu.Unlock()
		h.notify()
	}
}
