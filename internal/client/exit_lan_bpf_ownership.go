package client

import (
	"context"
	"math"
	"reflect"
)

// Caller-supplied boot/namespace identity must come from trusted observation.
// This describes held objects with a revoked lease; it does not inspect pins or
// live hooks, and therefore grants no adoption or publication authority.
func (p *exitLANBPFPreparation) describeOwnership(ctx context.Context, scope, bootID string, namespaceDevice, namespaceInode uint64) (*exitLANOwnership, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := exitLANBPFPinNames(scope); err != nil {
		return nil, err
	}
	if p == nil || !exitLANOwnershipBootID(bootID) || namespaceDevice == 0 || namespaceInode == 0 {
		return nil, errExitLANBPF
	}
	if !lockExitRuntime(ctx, &p.mu) {
		return nil, ctx.Err()
	}
	defer p.mu.Unlock()
	if p.call == nil || p.order == nil || p.outer < 0 || p.outer > math.MaxInt32 || p.program < 0 || p.program > math.MaxInt32 || p.outer == p.program {
		return nil, errExitLANBPF
	}
	if err := p.revokeLocked(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	read := func() (*exitLANOwnership, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		families, err := exitLANBPFFamilies(p.family)
		if err != nil || len(p.links) != len(families) {
			return nil, errExitLANBPF
		}
		owned := &exitLANOwnership{Scope: scope, BootID: bootID, NamespaceDevice: namespaceDevice, NamespaceInode: namespaceInode, Family: p.family}
		for i, fd := range []int{p.outer, p.program} {
			info, err := p.objectInfo(fd, 8)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				return nil, err
			}
			kind := uint32(exitLANBPFArrayOfMaps)
			if i == 1 {
				kind = 32
			}
			if p.order.Uint32(info) != kind || p.order.Uint32(info[4:]) == 0 {
				return nil, errExitLANBPF
			}
			if i == 0 {
				owned.MapID = p.order.Uint32(info[4:])
			} else {
				owned.ProgramID = p.order.Uint32(info[4:])
			}
		}
		fds := map[int]bool{p.outer: true, p.program: true}
		for _, link := range p.links {
			if fds[link.fd] {
				return nil, errExitLANBPF
			}
			fds[link.fd] = true
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, err := p.netfilterLinkIdentity(link.fd)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				return nil, err
			}
			if id != link.identity {
				return nil, errExitLANBPF
			}
			owned.Links = append(owned.Links, exitLANOwnedLink{ID: id.id, ProgramID: id.programID, Family: id.family, Hook: id.hook, Priority: id.priority})
		}
		if err := validateExitLANOwnership(owned); err != nil {
			return nil, err
		}
		return owned, nil
	}
	before, err := read()
	if err != nil {
		return nil, err
	}
	after, err := read()
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(before, after) {
		return nil, errExitLANBPF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return after, nil
}
