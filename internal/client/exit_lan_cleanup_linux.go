//go:build linux

package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"

	"golang.org/x/sys/unix"
)

func cleanupNativeExitLAN(ctx context.Context, guard *linuxExitGuard, owned *exitLANOwnership) error {
	if guard == nil || validateExitLANOwnership(owned) != nil {
		return errExitLANBPF
	}
	if owned.Routing != nil && (owned.Routing.Table != guard.mark^0x40000000 || owned.Routing.Mark != guard.mark^0x80000000) {
		return errExitLANPolicy
	}
	var order binary.ByteOrder = binary.LittleEndian
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		order = binary.BigEndian
	}
	// IDs may be reused after reboot. Never use an old-boot ID for lookup or
	// detach, nor interpret a same-named current pin as an owned object.
	n, err := newNativeExitLANBootNamespace(ctx)
	if err != nil {
		return err
	}
	oldBoot := false
	err = n.withCurrent(ctx, func(ctx context.Context, current exitLANNamespaceIdentity) error {
		oldBoot = current.BootID != owned.BootID
		if !oldBoot {
			return nil
		}
		if err := guard.ObserveContained(ctx); err != nil {
			return err
		}
		d, err := newNativeExitLANBPFDirectory(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		if err := confirmOldBootExitLANAbsent(ctx, d, owned, order, callExitLANBPF, unix.Close); err != nil {
			return err
		}
		if owned.Routing != nil {
			for _, family := range owned.Routing.families() {
				rows, err := owned.Routing.routesPresent(ctx, guard, family)
				if err != nil {
					return err
				}
				for _, present := range rows {
					if present {
						return errExitLANPolicy
					}
				}
				present, err := owned.Routing.rulePresent(ctx, guard, family)
				if err != nil {
					return err
				}
				if present {
					return errExitLANPolicy
				}
			}
		}
		return nil
	})
	err = errors.Join(err, n.Close())
	if err != nil || oldBoot {
		return err
	}
	r, err := newNativeExitLANBPFRecovery(ctx, guard, owned)
	if err != nil {
		return err
	}
	err = r.cleanup(ctx, guard.ObserveContained, order, callExitLANBPF, func(ctx context.Context, id exitLANBPFLinkIdentity) error {
		return observeExitLANHookStateWith(ctx, id, order, openNativeExitLANHookTransport, false)
	})
	if err == nil {
		err = r.namespace.withOwnership(ctx, owned, func(ctx context.Context) error { return cleanupExitLANRouting(ctx, guard, owned.Routing) })
	}
	return errors.Join(err, r.Close())
}

// The caller has verified a different boot and holds a current namespace. Only
// absence of every scoped pin can retire that old manifest. A collision remains
// untouched even if its kernel IDs happen to match the previous boot.
func confirmOldBootExitLANAbsent(ctx context.Context, d *exitLANBPFDirectory, owned *exitLANOwnership, order binary.ByteOrder, call exitLANBPFCall, closeFD func(int) error) error {
	names, err := exitLANBPFPinNames(owned.Scope)
	if err != nil {
		return err
	}
	if !lockExitRuntime(ctx, &d.mu) {
		return ctx.Err()
	}
	defer d.mu.Unlock()
	if d.fd < 0 || d.check == nil {
		return errExitLANBPF
	}
	p := &exitLANBPFPreparation{order: order, call: call, closeFD: closeFD}
	if err := d.check(d.fd); err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		fd, err := p.pinCommand(exitLANBPFObjectGet, d.fd, 0, name)
		if err == nil {
			return errors.Join(errExitLANBPF, closeFD(fd))
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := d.check(d.fd); err != nil {
		return err
	}
	return ctx.Err()
}
