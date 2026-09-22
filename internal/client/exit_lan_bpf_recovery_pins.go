package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"math"
	"sync"
)

// Inventory of reopened descriptors only. It does not adopt, unpin, detach or
// grant publication authority. Without an outer-map pin, a surviving program
// can still reference a lease map: missing map is not proof of revocation.
type exitLANBPFOwnedPins struct {
	mu           sync.Mutex
	ownership    *exitLANOwnership
	fds          [4]int
	leaseRevoked bool
	closeFD      func(int) error
	closed       bool
}

func (p *exitLANBPFOwnedPins) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	var err error
	for i := len(p.fds) - 1; i >= 0; i-- {
		if p.fds[i] >= 0 {
			err = errors.Join(err, p.closeFD(p.fds[i]))
			p.fds[i] = -1
		}
	}
	return err
}

// Caller holds a matching namespace binding and has confirmed nft BLOCK.
// Directory lock spans two complete pin snapshots and any map revocation.
// Pin directory entries are never changed. Only a verified owned outer map's
// contents are revoked; failures close only descriptors we opened.
func openExitLANBPFOwnedPins(ctx context.Context, directory *exitLANBPFDirectory, ownership *exitLANOwnership, order binary.ByteOrder, call exitLANBPFCall, closeFD func(int) error) (result *exitLANBPFOwnedPins, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if validateExitLANOwnership(ownership) != nil || directory == nil || call == nil || closeFD == nil || (order != binary.LittleEndian && order != binary.BigEndian) {
		return nil, errExitLANBPF
	}
	ownership = cloneExitLANOwnership(ownership)
	names, _ := exitLANBPFPinNames(ownership.Scope)
	if !lockExitRuntime(ctx, &directory.mu) {
		return nil, ctx.Err()
	}
	defer directory.mu.Unlock()
	if directory.fd < 0 || directory.check == nil {
		return nil, errExitLANBPF
	}
	if err := directory.check(directory.fd); err != nil {
		return nil, err
	}
	owned := &exitLANBPFOwnedPins{ownership: ownership, fds: [4]int{-1, -1, -1, -1}, closeFD: closeFD}
	defer func() {
		if result == nil {
			resultErr = errors.Join(ctx.Err(), resultErr, owned.Close())
		}
	}()
	p := &exitLANBPFPreparation{outer: -1, program: -1, order: order, call: call, closeFD: closeFD}
	expected := [4]exitLANBPFLinkIdentity{{id: ownership.MapID}, {id: ownership.ProgramID}}
	for _, link := range ownership.Links {
		i := 2
		if link.Family == 10 {
			i = 3
		}
		expected[i] = exitLANBPFLinkIdentity{link.ID, link.ProgramID, link.Family, link.Hook, link.Priority}
	}
	identity := func(fd, index int) (exitLANBPFLinkIdentity, error) {
		if index >= 2 {
			return p.netfilterLinkIdentity(fd)
		}
		info, err := p.objectInfo(fd, 8)
		if err != nil {
			return exitLANBPFLinkIdentity{}, err
		}
		kind := uint32(exitLANBPFArrayOfMaps)
		if index == 1 {
			kind = 32
		}
		if order.Uint32(info) != kind || order.Uint32(info[4:]) == 0 {
			return exitLANBPFLinkIdentity{}, errExitLANBPF
		}
		return exitLANBPFLinkIdentity{id: order.Uint32(info[4:])}, nil
	}
	read := func(index int) (fd int, id exitLANBPFLinkIdentity, err error) {
		fd = -1
		if err = ctx.Err(); err != nil {
			return
		}
		fd, err = p.pinCommand(exitLANBPFObjectGet, directory.fd, 0, names[index])
		if err != nil {
			fd = -1
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
			return
		}
		if fd < 0 || fd > math.MaxInt32 {
			return fd, id, errExitLANBPF
		}
		if ctx.Err() != nil {
			return fd, id, ctx.Err()
		}
		if expected[index].id == 0 {
			return fd, id, errExitLANBPF
		}
		id, err = identity(fd, index)
		if ctx.Err() != nil {
			err = ctx.Err()
		} else if err == nil && id != expected[index] {
			err = errExitLANBPF
		}
		return
	}
	for i := range names {
		fd, _, err := read(i)
		owned.fds[i] = fd
		if err != nil {
			return nil, err
		}
	}
	// Keep first-snapshot FDs held while checking every name again. A missing,
	// newly appeared, foreign or rebound pin must fail before the first write.
	for i := range names {
		fd, _, err := read(i)
		if (fd >= 0) != (owned.fds[i] >= 0) {
			err = errors.Join(err, errExitLANBPF)
		}
		if fd >= 0 {
			err = errors.Join(err, closeFD(fd))
		}
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := directory.check(directory.fd); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if owned.fds[0] >= 0 {
		p.outer = owned.fds[0]
		if err := p.revokeLocked(); err != nil {
			return nil, err
		}
		owned.leaseRevoked = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := directory.check(directory.fd); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return owned, nil
}
