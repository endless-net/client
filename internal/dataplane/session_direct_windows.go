//go:build windows

package dataplane

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

// WaitDirect does not hold the native owner mutex across a network wait: fresh
// map/offline/revoke updates and Stop must be able to preempt it. Completion of
// the operation remains with the daemon worker after its final intent fence.
func (s *WindowsSession) WaitDirect(parent context.Context, probeInterval time.Duration) (err error) {
	b, err := connectivity.OperationFromContext(parent)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.lifetime, cancel)
	defer func() { stop(); cancel() }()
	s.mu.Lock()
	if s.stopped || s.engine == nil {
		s.mu.Unlock()
		return ErrClosed
	}
	if b != s.operation || !b.Active() {
		s.mu.Unlock()
		return connectivity.ErrConfig
	}
	e := s.engine
	s.mu.Unlock()
	defer func() {
		if err != nil {
			s.lease.Revoke()
			s.cancel()
			s.mu.Lock()
			defer s.mu.Unlock()
			err = s.failUpdate(err)
		}
	}()
	if err := connectivity.CheckOperation(ctx); err != nil {
		return err
	}
	for {
		_, sourceChanged := s.lease.SourceSignal()
		err = e.WaitDirect(ctx, probeInterval)
		if !errors.Is(err, ErrNoAuthority) {
			break
		}
		if err := connectivity.CheckOperation(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(probeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-sourceChanged:
			timer.Stop()
		case <-timer.C:
		}
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine != e {
		return ErrClosed
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	if err := connectivity.CheckOperation(ctx); err != nil {
		return err
	}
	if !s.lease.Permissions().NewFlows {
		return ErrNoAuthority
	}
	return nil
}

func (s *WindowsSession) ProbeDirect(parent context.Context) error {
	ctx, finish, err := s.updateContext(parent)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	return s.engine.ProbeDirect(ctx)
}

func (s *WindowsSession) DirectPaths() ([]DirectPath, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return nil, ErrClosed
	}
	return s.engine.DirectPaths()
}
