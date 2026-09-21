package client

import (
	"context"
	"errors"
	"math"
)

const (
	exitLANBPFInfo          = 15
	exitLANBPFLinkCreate    = 28
	exitLANBPFNetfilterLink = 10
)

// Identity is deliberately not a live-attachment receipt: detached netfilter
// links retain these fields. Opening LAN also requires current-netns hook dump
// confirmation and durable pins, neither of which is performed here.
type exitLANBPFLinkIdentity struct {
	id, programID, family, hook uint32
	priority                    int32
}

type exitLANBPFLink struct {
	fd       int
	identity exitLANBPFLinkIdentity
}

func (p *exitLANBPFPreparation) objectInfo(fd, size int) ([]byte, error) {
	if fd < 0 || fd > math.MaxInt32 || (size != 8 && size != 32) {
		return nil, errExitLANBPF
	}
	info := make([]byte, size)
	attr := make([]byte, 16)
	p.order.PutUint32(attr, uint32(fd))
	p.order.PutUint32(attr[4:], uint32(size))
	p.order.PutUint64(attr[8:], exitLANBPFPointer(info))
	if _, err := p.invoke(exitLANBPFInfo, attr, info); err != nil {
		return nil, err
	}
	if p.order.Uint32(attr[4:]) < uint32(size) {
		return nil, errExitLANBPF
	}
	return info, nil
}

func (p *exitLANBPFPreparation) netfilterLinkIdentity(fd int) (exitLANBPFLinkIdentity, error) {
	info, err := p.objectInfo(fd, 32)
	if err != nil {
		return exitLANBPFLinkIdentity{}, err
	}
	u32 := func(at int) uint32 { return p.order.Uint32(info[at:]) }
	if u32(0) != exitLANBPFNetfilterLink || u32(4) == 0 || u32(8) == 0 || u32(12) != 0 || u32(28) != 0 {
		return exitLANBPFLinkIdentity{}, errExitLANBPF
	}
	return exitLANBPFLinkIdentity{u32(4), u32(8), u32(16), u32(20), int32(u32(24))}, nil
}

// attachClosed attaches both IP families with an empty lease. It neither pins
// links nor grants runtime authority to publish/open LAN. Before calling, the
// adapter must independently confirm nft BLOCK and retain that guard through
// pinning and live readback. This primitive owns only links it just created.
func (p *exitLANBPFPreparation) attachClosed(ctx context.Context, hook uint32, priority int32) (result error) {
	if p == nil || hook >= 5 || priority == math.MinInt32 || priority == math.MaxInt32 {
		return errExitLANBPF
	}
	if !lockExitRuntime(ctx, &p.mu) {
		return ctx.Err()
	}
	defer p.mu.Unlock()
	if p.outer < 0 || p.program < 0 || len(p.links) != 0 {
		return errExitLANBPF
	}
	if err := p.revokeLocked(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var created []exitLANBPFLink
	defer func() {
		if result != nil {
			for _, link := range created {
				result = errors.Join(result, p.closeFD(link.fd))
			}
		}
	}()
	program, err := p.objectInfo(p.program, 8)
	if err != nil {
		return err
	}
	programID := p.order.Uint32(program[4:])
	if p.order.Uint32(program) != 32 || programID == 0 {
		return errExitLANBPF
	}
	for _, family := range []uint32{2, 10} {
		if err := ctx.Err(); err != nil {
			return err
		}
		attr := make([]byte, 32)
		p.order.PutUint32(attr, uint32(p.program))
		p.order.PutUint32(attr[8:], 45)
		p.order.PutUint32(attr[16:], family)
		p.order.PutUint32(attr[20:], hook)
		p.order.PutUint32(attr[24:], uint32(priority))
		fd, err := p.invoke(exitLANBPFLinkCreate, attr)
		if err != nil {
			return err
		}
		if fd < 0 || fd > math.MaxInt32 {
			return errExitLANBPF
		}
		created = append(created, exitLANBPFLink{fd: fd})
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := p.netfilterLinkIdentity(fd)
		if err != nil {
			return err
		}
		if identity.programID != programID || identity.family != family || identity.hook != hook || identity.priority != priority {
			return errExitLANBPF
		}
		if len(created) == 2 && created[0].identity.id == identity.id {
			return errExitLANBPF
		}
		created[len(created)-1].identity = identity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.links = created
	return nil
}
