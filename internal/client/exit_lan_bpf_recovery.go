package client

import (
	"context"
	"errors"
	"sync"
)

type exitLANBPFRecoveryOps struct {
	namespace func(context.Context) (*exitLANBootNamespace, error)
	directory func(context.Context) (*exitLANBPFDirectory, error)
	pins      func(context.Context, *exitLANBPFDirectory, *exitLANOwnership) (*exitLANBPFOwnedPins, error)
}

// An inventory of verified surviving pins under retained namespace/directory
// handles. This does not restore a lease or authorize removing the journal.
type exitLANBPFRecovery struct {
	mu        sync.Mutex
	namespace *exitLANBootNamespace
	directory *exitLANBPFDirectory
	pins      *exitLANBPFOwnedPins
	closed    bool
}

// Caller owns the runtime effect lock. Missing pins are expected after partial
// creation; missing map pin does not imply no live map or an expired lease.
// Keep nft BLOCK until later explicit detach/cleanup is positively observed.
func recoverExitLANBPF(ctx context.Context, owned *exitLANOwnership, confirmBlocked func(context.Context) error, ops exitLANBPFRecoveryOps) (result *exitLANBPFRecovery, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if validateExitLANOwnership(owned) != nil || confirmBlocked == nil || ops.namespace == nil || ops.directory == nil || ops.pins == nil {
		return nil, errExitLANBPF
	}
	owned = cloneExitLANOwnership(owned)
	r := &exitLANBPFRecovery{}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, r.Close())
		}
	}()
	var err error
	r.namespace, err = ops.namespace(ctx)
	if err != nil {
		return nil, err
	}
	if r.namespace == nil {
		return nil, errExitLANBPF
	}
	err = r.namespace.withOwnership(ctx, owned, func(ctx context.Context) error {
		if err := confirmBlocked(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r.directory, err = ops.directory(ctx)
		if err != nil {
			return err
		}
		if r.directory == nil {
			return errExitLANBPF
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r.pins, err = ops.pins(ctx, r.directory, cloneExitLANOwnership(owned))
		if err != nil {
			return err
		}
		if r.pins == nil {
			return errExitLANBPF
		}
		if err := confirmBlocked(ctx); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Close releases only inventory handles. It never unpins objects or discards
// durable ownership, even when the observed set was empty.
func (r *exitLANBPFRecovery) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.pins != nil {
		err = errors.Join(err, r.pins.Close())
	}
	if r.directory != nil {
		err = errors.Join(err, r.directory.Close())
	}
	if r.namespace != nil {
		err = errors.Join(err, r.namespace.Close())
	}
	return err
}
