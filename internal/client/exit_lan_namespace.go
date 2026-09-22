package client

import (
	"context"
	"errors"
	"runtime"
	"sync"
)

type exitLANNamespaceIdentity struct {
	BootID        string
	Device, Inode uint64
}

func (id exitLANNamespaceIdentity) valid() bool {
	return exitLANOwnershipBootID(id.BootID) && id.Device != 0 && id.Inode != 0
}

// inspect revalidates a held namespace descriptor and observes the namespace
// of the calling OS thread. A descriptor alone cannot bind later syscalls.
type exitLANBootNamespace struct {
	mu              sync.Mutex
	identity        exitLANNamespaceIdentity
	inspect         func(context.Context) (exitLANNamespaceIdentity, error)
	close           func() error
	closed, invalid bool
}

// Takes ownership of close, including constructor failure. Native capture must
// also pin its calling OS thread while opening the initial namespace handle.
func newExitLANBootNamespace(ctx context.Context, inspect func(context.Context) (exitLANNamespaceIdentity, error), closeHandle func() error) (result *exitLANBootNamespace, err error) {
	if closeHandle == nil {
		return nil, errExitLANBPF
	}
	defer func() {
		if result == nil {
			err = errors.Join(err, closeHandle())
		}
	}()
	if inspect == nil {
		return nil, errExitLANBPF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	first, err := inspect(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !first.valid() {
		return nil, errExitLANBPF
	}
	second, err := inspect(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if first != second {
		return nil, errExitLANBPF
	}
	return &exitLANBootNamespace{identity: first, inspect: inspect, close: closeHandle}, nil
}

// Lock order: namespace -> directory -> preparation. fn must execute its
// namespace-sensitive syscalls synchronously on this goroutine, honor ctx,
// never call setns, and never reacquire this object's lock. Errors do not undo
// effects: the caller must retain independent nft containment throughout.
func (n *exitLANBootNamespace) withCurrent(ctx context.Context, fn func(context.Context, exitLANNamespaceIdentity) error) error {
	if n == nil || fn == nil {
		return errExitLANBPF
	}
	if !lockExitRuntime(ctx, &n.mu) {
		return ctx.Err()
	}
	defer n.mu.Unlock()
	if n.closed || n.invalid || n.inspect == nil || n.close == nil {
		return errExitLANBPF
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	check := func() error {
		actual, err := n.inspect(ctx)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && (!actual.valid() || actual != n.identity) {
			err = errExitLANBPF
		}
		if err != nil {
			n.invalid = true
		}
		return err
	}
	if err := check(); err != nil {
		return err
	}
	err := fn(ctx, n.identity)
	return errors.Join(err, check())
}

func (n *exitLANBootNamespace) Close() error {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil
	}
	n.closed = true
	if n.close == nil {
		return errExitLANBPF
	}
	return n.close()
}

// Recovery must reject a manifest from another boot or namespace before any
// object lookup or cleanup. Matching here still does not prove pin ownership.
func (n *exitLANBootNamespace) withOwnership(ctx context.Context, owned *exitLANOwnership, fn func(context.Context) error) error {
	if validateExitLANOwnership(owned) != nil || fn == nil {
		return errExitLANBPF
	}
	expected := exitLANNamespaceIdentity{BootID: owned.BootID, Device: owned.NamespaceDevice, Inode: owned.NamespaceInode}
	return n.withCurrent(ctx, func(ctx context.Context, actual exitLANNamespaceIdentity) error {
		if expected != actual {
			return errExitLANBPF
		}
		return fn(ctx)
	})
}
