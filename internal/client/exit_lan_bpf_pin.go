package client

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"sync"
)

const (
	exitLANBPFObjectPin = 6
	exitLANBPFObjectGet = 7
	exitLANBPFPathFD    = 0x4000
)

// The native constructor holds an exclusive cross-process lock on a checked
// root-owned bpffs directory for this object's entire lifetime. Close releases
// the directory handle only; pins must survive process termination.
type exitLANBPFDirectory struct {
	mu      sync.Mutex
	fd      int
	closeFD func(int) error
	check   func(int) error
}

func (d *exitLANBPFDirectory) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fd < 0 {
		return nil
	}
	fd := d.fd
	d.fd = -1
	return d.closeFD(fd)
}

func exitLANBPFPinNames(scope string) ([]string, error) {
	if len(scope) != 24 {
		return nil, errExitLANBPF
	}
	for _, r := range scope {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return nil, errExitLANBPF
		}
	}
	return []string{scope + "_lease", scope + "_program", scope + "_ipv4", scope + "_ipv6"}, nil
}

func (p *exitLANBPFPreparation) pinCommand(command, directory, fd int, name string) (int, error) {
	if directory < 0 || directory > math.MaxInt32 || fd < 0 || fd > math.MaxInt32 || (command != exitLANBPFObjectPin && command != exitLANBPFObjectGet) {
		return -1, errExitLANBPF
	}
	if command == exitLANBPFObjectGet && fd != 0 {
		return -1, errExitLANBPF
	}
	if len(name) == 0 || len(name) > 64 {
		return -1, errExitLANBPF
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return -1, errExitLANBPF
		}
	}
	path := append([]byte(name), 0)
	attr := make([]byte, 20)
	p.order.PutUint64(attr, exitLANBPFPointer(path))
	p.order.PutUint32(attr[8:], uint32(fd))
	p.order.PutUint32(attr[12:], exitLANBPFPathFD)
	p.order.PutUint32(attr[16:], uint32(directory))
	return p.invoke(command, attr, path)
}

// pinClosed creates pins exclusively and returns the names successfully created,
// including on partial failure. It never replaces or unlinks a pin. The caller
// must keep nft BLOCK, persist ownership, and verify live hooks before opening
// LAN. After successful revocation, errors leave the lease closed and any
// created pins intact for recovery. Earlier errors make no revocation promise;
// nft BLOCK remains mandatory. Object bindings do not establish live attachment.
func (p *exitLANBPFPreparation) pinClosed(ctx context.Context, directory *exitLANBPFDirectory, scope string) (created []string, result error) {
	names, err := exitLANBPFPinNames(scope)
	if err != nil {
		return nil, err
	}
	if p == nil || directory == nil {
		return nil, errExitLANBPF
	}
	if !lockExitRuntime(ctx, &directory.mu) {
		return nil, ctx.Err()
	}
	defer directory.mu.Unlock()
	if directory.fd < 0 || directory.check == nil || directory.closeFD == nil {
		return nil, errExitLANBPF
	}
	if err := directory.check(directory.fd); err != nil {
		return nil, err
	}
	if !lockExitRuntime(ctx, &p.mu) {
		return nil, ctx.Err()
	}
	defer p.mu.Unlock()
	if p.outer < 0 || p.program < 0 || len(p.links) != 2 {
		return nil, errExitLANBPF
	}
	if err := p.revokeLocked(); err != nil {
		return nil, err
	}
	// No mutations to existing pins, even if they appear to name these objects.
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fd, err := p.pinCommand(exitLANBPFObjectGet, directory.fd, 0, name)
		if err == nil {
			if fd < 0 || fd > math.MaxInt32 {
				return nil, errExitLANBPF
			}
			return nil, errors.Join(errExitLANBPF, p.closeFD(fd))
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	fds := []int{p.outer, p.program, p.links[0].fd, p.links[1].fd}
	for i, fd := range fds {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		original, err := p.pinObjectIdentity(fd, i)
		if err != nil {
			return created, err
		}
		if err := ctx.Err(); err != nil {
			return created, err
		}
		if _, err = p.pinCommand(exitLANBPFObjectPin, directory.fd, fd, names[i]); err != nil {
			return created, err
		}
		created = append(created, names[i])
		if err := ctx.Err(); err != nil {
			return created, err
		}
		opened, err := p.pinCommand(exitLANBPFObjectGet, directory.fd, 0, names[i])
		if err != nil {
			return created, err
		}
		if opened < 0 || opened > math.MaxInt32 {
			return created, errExitLANBPF
		}
		actual, observeErr := p.pinObjectIdentity(opened, i)
		closeErr := p.closeFD(opened)
		if actual != original || observeErr != nil || closeErr != nil {
			return created, errors.Join(errExitLANBPF, observeErr, closeErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return created, err
	}
	if err := directory.check(directory.fd); err != nil {
		return created, err
	}
	return created, ctx.Err()
}

func (p *exitLANBPFPreparation) pinObjectIdentity(fd, index int) (exitLANBPFLinkIdentity, error) {
	if index >= 2 {
		identity, err := p.netfilterLinkIdentity(fd)
		if err != nil {
			return exitLANBPFLinkIdentity{}, err
		}
		if index > 3 || identity != p.links[index-2].identity {
			return exitLANBPFLinkIdentity{}, errExitLANBPF
		}
		return identity, nil
	}
	info, err := p.objectInfo(fd, 8)
	if err != nil {
		return exitLANBPFLinkIdentity{}, err
	}
	want := uint32(exitLANBPFArrayOfMaps)
	if index == 1 {
		want = 32
	}
	id := p.order.Uint32(info[4:])
	if p.order.Uint32(info) != want || id == 0 {
		return exitLANBPFLinkIdentity{}, errExitLANBPF
	}
	return exitLANBPFLinkIdentity{id: id}, nil
}
