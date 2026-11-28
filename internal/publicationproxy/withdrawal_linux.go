package publicationproxy

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	"google.golang.org/protobuf/proto"
)

// Called under mu. An interrupted drain cannot attest to absence of traffic.
func (p *LinuxRuntime) drained() bool {
	if p.worker == nil {
		return true
	}
	select {
	case <-p.worker.done:
		return p.worker.err == nil || errors.Is(p.worker.err, context.Canceled)
	default:
		return false
	}
}

// After session revocation the workload identity can still acknowledge an
// empty epoch. The caller holds the exclusive Client process lock.
func (p *LinuxRuntime) ReportIdleWithdrawals(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		_ = p.reportIdle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (p *LinuxRuntime) reportIdle(parent context.Context) error {
	p.mu.Lock()
	prior := p.worker
	idle := p.drained()
	p.mu.Unlock()
	if !idle {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	res, err := p.Management.GetPublicationRoutes(ctx, connect.NewRequest(&api.GetPublicationRoutesRequest{Executor: p.Executor}))
	if err != nil {
		return err
	}
	if res == nil || res.Msg == nil {
		return connectivity.ErrPublication
	}
	s := res.Msg
	now := time.Now()
	if !proto.Equal(s.Executor, p.Executor) || s.Generation == 0 || s.LeaseId == "" || s.IssuedAt.CheckValid() != nil || s.ValidUntil.CheckValid() != nil || now.Before(s.IssuedAt.AsTime()) || !now.Before(s.ValidUntil.AsTime()) {
		return connectivity.ErrPublication
	}
	if len(s.Routes) > 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if prior != p.worker || !p.drained() {
		return nil
	}
	ack, err := p.Management.ReportPublicationRoutes(ctx, connect.NewRequest(&api.ReportPublicationRoutesRequest{Executor: p.Executor, Generation: s.Generation, LeaseId: s.LeaseId}))
	if err != nil {
		return err
	}
	if ack == nil || ack.Msg.GetAcknowledgedGeneration() != s.Generation || ack.Msg.GetLeaseId() != s.LeaseId {
		return connectivity.ErrPublication
	}
	return nil
}
