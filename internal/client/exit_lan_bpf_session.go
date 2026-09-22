package client

import (
	"context"
	"errors"
	"math"
	"sync"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitLANBPFSessionOps struct {
	namespace   func(context.Context) (*exitLANBootNamespace, error)
	directory   func(context.Context) (*exitLANBPFDirectory, error)
	preparation func(context.Context, uint32) (*exitLANBPFPreparation, error)
	observe     func(context.Context, exitLANBPFLinkIdentity) error
}

// Owns a closed, pinned preparation and its namespace/directory lifetimes.
// Successful preparation is not permission to publish a deadline or open LAN.
type exitLANBPFSession struct {
	mu          sync.Mutex
	namespace   *exitLANBootNamespace
	directory   *exitLANBPFDirectory
	preparation *exitLANBPFPreparation
	ownership   *exitLANOwnership
	closed      bool
}

// Caller holds the runtime effect lock and has durably recorded exit protection.
// Namespace binding spans every namespace-sensitive syscall in preparation.
// No acquired handle escapes on failure; pinned objects and their manifest
// remain closed for recovery, including ambiguous pin/checkpoint results.
// checkpoint is store-only and must not reacquire runtime, namespace, directory
// or preparation locks. A failed guard/namespace readback does not confirm nft
// containment; the caller must recover that protection independently. Retained
// manifests require recovery before another scope can be prepared.
func prepareExitLANBPFSession(ctx context.Context, mark uint32, mode api.ExitFamilyMode, hook uint32, priority int32, scope string, checkpoint func(context.Context, *exitLANOwnership) error, confirmBlocked func(context.Context) error, ops exitLANBPFSessionOps) (result *exitLANBPFSession, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := exitLANBPFPinNames(scope); err != nil {
		return nil, err
	}
	if _, err := exitLANBPFFamilies(mode); err != nil {
		return nil, err
	}
	if mark == 0 || hook >= 5 || priority == math.MinInt32 || priority == math.MaxInt32 || checkpoint == nil || confirmBlocked == nil || ops.namespace == nil || ops.directory == nil || ops.preparation == nil || ops.observe == nil {
		return nil, errExitLANBPF
	}
	s := &exitLANBPFSession{}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, s.Close())
		}
	}()
	var err error
	s.namespace, err = ops.namespace(ctx)
	if err != nil {
		return nil, err
	}
	if s.namespace == nil {
		return nil, errExitLANBPF
	}
	err = s.namespace.withCurrent(ctx, func(ctx context.Context, identity exitLANNamespaceIdentity) error {
		if err := confirmBlocked(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.directory, err = ops.directory(ctx)
		if err != nil {
			return err
		}
		if s.directory == nil {
			return errExitLANBPF
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.preparation, err = ops.preparation(ctx, mark)
		if err != nil {
			return err
		}
		if s.preparation == nil {
			return errExitLANBPF
		}
		if err := s.preparation.attachClosed(ctx, mode, hook, priority); err != nil {
			return err
		}
		_, err := s.preparation.pinClosed(ctx, s.directory, scope, identity.BootID, identity.Device, identity.Inode, func(ctx context.Context, owned *exitLANOwnership) error {
			if err := checkpoint(ctx, cloneExitLANOwnership(owned)); err != nil {
				return err
			}
			s.ownership = cloneExitLANOwnership(owned)
			return ctx.Err()
		})
		if err != nil {
			return err
		}
		if err := s.observeClosed(ctx, mode, hook, priority, ops.observe); err != nil {
			return err
		}
		if err := confirmBlocked(ctx); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// The constructor has exclusive access to s. Keep directory -> preparation
// ordering while validating held objects and live hooks in the bound namespace.
func (s *exitLANBPFSession) observeClosed(ctx context.Context, mode api.ExitFamilyMode, hook uint32, priority int32, observe func(context.Context, exitLANBPFLinkIdentity) error) error {
	if !lockExitRuntime(ctx, &s.directory.mu) {
		return ctx.Err()
	}
	defer s.directory.mu.Unlock()
	if s.directory.fd < 0 || s.directory.check == nil {
		return errExitLANBPF
	}
	if err := s.directory.check(s.directory.fd); err != nil {
		return err
	}
	if !lockExitRuntime(ctx, &s.preparation.mu) {
		return ctx.Err()
	}
	defer s.preparation.mu.Unlock()
	if err := s.preparation.revokeLocked(); err != nil {
		return err
	}
	if err := s.preparation.observeHeldLinksLocked(ctx, mode, hook, priority, observe); err != nil {
		return err
	}
	if err := s.directory.check(s.directory.fd); err != nil {
		return err
	}
	return ctx.Err()
}

// Revoke before releasing descriptors. Pins are never removed here: even a
// failed close must leave durable ownership available for explicit recovery.
func (s *exitLANBPFSession) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.preparation != nil {
		err = errors.Join(err, s.preparation.revoke(), s.preparation.Close())
	}
	if s.directory != nil {
		err = errors.Join(err, s.directory.Close())
	}
	if s.namespace != nil {
		err = errors.Join(err, s.namespace.Close())
	}
	return err
}
