package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
)

const exitLANBPFLinkDetach = 34

// Cleanup is deliberately separate from inventory Close. Keep the namespace,
// directory lock and every object descriptor until detach, absence readback and
// unpin have all completed. An error leaves the durable manifest for retry.
func (r *exitLANBPFRecovery) cleanup(ctx context.Context, blocked func(context.Context) error, order binary.ByteOrder, call exitLANBPFCall, absent func(context.Context, exitLANBPFLinkIdentity) error) error {
	if r == nil || blocked == nil || call == nil || absent == nil || (order != binary.LittleEndian && order != binary.BigEndian) {
		return errExitLANBPF
	}
	if !lockExitRuntime(ctx, &r.mu) {
		return ctx.Err()
	}
	defer r.mu.Unlock()
	if r.closed || r.pins == nil || r.directory == nil {
		return errExitLANBPF
	}
	return r.namespace.withOwnership(ctx, r.pins.ownership, func(ctx context.Context) error {
		if err := blocked(ctx); err != nil {
			return err
		}
		d, pins := r.directory, r.pins
		if !lockExitRuntime(ctx, &d.mu) {
			return ctx.Err()
		}
		defer d.mu.Unlock()
		if !lockExitRuntime(ctx, &pins.mu) {
			return ctx.Err()
		}
		defer pins.mu.Unlock()
		if pins.closed || d.fd < 0 || d.check == nil || d.unlink == nil {
			return errExitLANBPF
		}
		if err := d.check(d.fd); err != nil {
			return err
		}
		p := &exitLANBPFPreparation{order: order, call: call, closeFD: pins.closeFD}
		names, err := exitLANBPFPinNames(pins.ownership.Scope)
		if err != nil {
			return err
		}
		// Reopening every name before mutation prevents a stale inventory from
		// authorizing deletion of a replacement object.
		checkPin := func(i int) error {
			fd, err := p.pinCommand(exitLANBPFObjectGet, d.fd, 0, names[i])
			if errors.Is(err, fs.ErrNotExist) && pins.fds[i] < 0 {
				return nil
			}
			if err != nil {
				return err
			}
			defer func() { _ = pins.closeFD(fd) }()
			if pins.fds[i] < 0 {
				return errExitLANBPF
			}
			actual, err := p.objectInfo(fd, 8)
			if err != nil {
				return err
			}
			held, err := p.objectInfo(pins.fds[i], 8)
			if err != nil || string(actual) != string(held) {
				return errExitLANBPF
			}
			return ctx.Err()
		}
		for i := range names {
			if err := checkPin(i); err != nil {
				return err
			}
		}
		for _, link := range pins.ownership.Links {
			i := 2
			if link.Family == 10 {
				i = 3
			}
			identity := exitLANBPFLinkIdentity{link.ID, link.ProgramID, link.Family, link.Hook, link.Priority}
			if pins.fds[i] >= 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
				actual, err := p.netfilterLinkIdentity(pins.fds[i])
				if err != nil || actual != identity {
					return errExitLANBPF
				}
				attr := make([]byte, 4)
				order.PutUint32(attr, uint32(pins.fds[i]))
				if _, err := p.invoke(exitLANBPFLinkDetach, attr); err != nil {
					return err
				}
			}
			// Missing link pins are not evidence that the hook disappeared.
			if err := absent(ctx, identity); err != nil {
				return err
			}
		}
		for i := len(names) - 1; i >= 0; i-- {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := checkPin(i); err != nil {
				return err
			}
			if pins.fds[i] >= 0 {
				if err := d.unlink(d.fd, names[i]); err != nil {
					return err
				}
			}
		}
		for _, name := range names {
			fd, err := p.pinCommand(exitLANBPFObjectGet, d.fd, 0, name)
			if err == nil {
				return errors.Join(errExitLANBPF, pins.closeFD(fd))
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := d.check(d.fd); err != nil {
			return err
		}
		if err := blocked(ctx); err != nil {
			return err
		}
		return ctx.Err()
	})
}
