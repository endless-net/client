package client

import (
	"context"
	"math"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Caller holds p.mu throughout observation and publication. The callback must
// honor ctx and must not acquire preparation or directory locks. Holding FDs
// prevents local Close/replacement, but metadata alone cannot prove attachment.
// Fresh hook dumps remain point-in-time evidence, not namespace/pin ownership.
func (p *exitLANBPFPreparation) observeHeldLinksLocked(ctx context.Context, mode api.ExitFamilyMode, hook uint32, priority int32, observe func(context.Context, exitLANBPFLinkIdentity) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	families, err := exitLANBPFFamilies(mode)
	if err != nil || p == nil || observe == nil || p.call == nil || p.order == nil || p.family != mode || hook >= 5 || priority == math.MinInt32 || priority == math.MaxInt32 {
		return errExitLANBPF
	}
	if p.outer < 0 || p.outer > math.MaxInt32 || p.program < 0 || p.program > math.MaxInt32 || p.outer == p.program || len(p.links) != len(families) {
		return errExitLANBPF
	}
	seenFD := map[int]bool{p.outer: true, p.program: true}
	seenID := map[uint32]bool{}
	for i, link := range p.links {
		id := link.identity
		if link.fd < 0 || link.fd > math.MaxInt32 || seenFD[link.fd] || id.id == 0 || seenID[id.id] || id.programID == 0 || id.family != families[i] || id.hook != hook || id.priority != priority {
			return errExitLANBPF
		}
		seenFD[link.fd], seenID[id.id] = true, true
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	metadata := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := p.objectInfo(p.program, 8)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		programID := p.order.Uint32(info[4:])
		if p.order.Uint32(info) != 32 || programID == 0 {
			return errExitLANBPF
		}
		for _, link := range p.links {
			if err := ctx.Err(); err != nil {
				return err
			}
			actual, err := p.netfilterLinkIdentity(link.fd)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if actual != link.identity || actual.programID != programID {
				return errExitLANBPF
			}
		}
		return nil
	}
	if err := metadata(); err != nil {
		return err
	}
	for _, link := range p.links {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := observe(ctx, link.identity)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	}
	return metadata()
}
