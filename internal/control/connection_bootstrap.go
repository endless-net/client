package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/endless-net/client/internal/state"
)

// ForConnection shares the independently verified bootstrap within one
// bounded connection attempt. Map and endpoint callers still verify their
// own authority and signatures. Background work fetches normally afterwards.
func (b *Bootstrap) ForConnection(operation context.Context, origin string) Announcements {
	return &connectionBootstrap{source: b, operation: operation, origin: origin, lock: make(chan struct{}, 1)}
}

type connectionBootstrap struct {
	source    *Bootstrap
	operation context.Context
	origin    string
	lock      chan struct{}
	cached    Announcement
}

func (b *connectionBootstrap) Fetch(ctx context.Context, origin string) (Announcement, error) {
	if origin != b.origin {
		return Announcement{}, ErrBootstrap
	}
	if b.operation.Err() != nil {
		return b.source.Fetch(ctx, origin)
	}
	select {
	case <-ctx.Done():
		return Announcement{}, ctx.Err()
	case <-b.operation.Done():
		return b.source.Fetch(ctx, origin)
	case b.lock <- struct{}{}:
		defer func() { <-b.lock }()
	}
	if b.operation.Err() != nil {
		return b.source.Fetch(ctx, origin)
	}
	if b.cached.VerifiedAt(b.source.now()) {
		// An independent fetch may already have advanced the durable floor.
		// Returning the same checkpoint performs no write.
		err := b.source.store.AcceptBootstrap(ctx, origin, func(current state.BootstrapCheckpoint) (state.BootstrapCheckpoint, error) {
			if current != b.cached.Checkpoint() || !b.cached.VerifiedAt(b.source.now()) {
				return state.BootstrapCheckpoint{}, ErrBootstrap
			}
			return current, nil
		})
		if ctx.Err() != nil {
			return Announcement{}, ctx.Err()
		}
		if err != nil && !errors.Is(err, ErrBootstrap) {
			return Announcement{}, fmt.Errorf("%w: %w", ErrBootstrapState, err)
		}
		if err == nil && b.operation.Err() == nil && b.cached.VerifiedAt(b.source.now()) {
			result := b.cached
			result.Bundle = bytes.Clone(result.Bundle)
			return result, nil
		}
	}
	result, err := b.source.Fetch(ctx, origin)
	if err == nil && b.operation.Err() == nil {
		b.cached = result
		b.cached.Bundle = bytes.Clone(result.Bundle)
	}
	return result, err
}
