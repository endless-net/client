package client

import (
	"context"
	"errors"
	"io/fs"
)

// Caller holds the matching namespace. Persistent pins, not held process FDs,
// keep the packet gate attached after process death. Revalidate every name.
func (s *exitLANBPFSession) observePins(ctx context.Context) error {
	if s == nil || s.ownership == nil || s.directory == nil || s.preparation == nil {
		return errExitLANBPF
	}
	d, p := s.directory, s.preparation
	if !lockExitRuntime(ctx, &d.mu) {
		return ctx.Err()
	}
	defer d.mu.Unlock()
	if !lockExitRuntime(ctx, &p.mu) {
		return ctx.Err()
	}
	defer p.mu.Unlock()
	if d.fd < 0 || d.check == nil || p.outer < 0 || p.program < 0 {
		return errExitLANBPF
	}
	if err := d.check(d.fd); err != nil {
		return err
	}
	names, err := exitLANBPFPinNames(s.ownership.Scope)
	if err != nil {
		return err
	}
	expected := [4]exitLANBPFLinkIdentity{{id: s.ownership.MapID}, {id: s.ownership.ProgramID}}
	for _, link := range s.ownership.Links {
		i := 2
		if link.Family == 10 {
			i = 3
		}
		expected[i] = exitLANBPFLinkIdentity{link.ID, link.ProgramID, link.Family, link.Hook, link.Priority}
	}
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		fd, err := p.pinCommand(exitLANBPFObjectGet, d.fd, 0, name)
		if expected[i].id == 0 && errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var actual exitLANBPFLinkIdentity
		if i >= 2 {
			actual, err = p.netfilterLinkIdentity(fd)
		} else {
			var info []byte
			info, err = p.objectInfo(fd, 8)
			if err == nil {
				kind := uint32(exitLANBPFArrayOfMaps)
				if i == 1 {
					kind = 32
				}
				if p.order.Uint32(info) != kind {
					err = errExitLANBPF
				} else {
					actual.id = p.order.Uint32(info[4:])
				}
			}
		}
		closeErr := p.closeFD(fd)
		if err != nil || closeErr != nil || actual != expected[i] {
			return errors.Join(errExitLANBPF, err, closeErr)
		}
	}
	if err := d.check(d.fd); err != nil {
		return err
	}
	return ctx.Err()
}
