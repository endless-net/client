//go:build windows

package dataplane

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

// WaitPaths does not hold the native owner mutex across a network wait: fresh
// map/offline/revoke updates and Stop must be able to preempt it. Completion of
// the operation remains with the daemon worker after its final intent fence.
func (s *WindowsSession) WaitPaths(parent context.Context, probeInterval time.Duration) (err error) {
	if probeInterval < time.Millisecond || probeInterval > 30*time.Second {
		return ErrConfiguration
	}
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
	if err := s.packets.Err(); err != nil {
		s.mu.Unlock()
		return err
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
	poll := time.NewTicker(min(probeInterval, negotiationRetry))
	defer poll.Stop()
	for {
		if err := connectivity.CheckOperation(ctx); err != nil {
			return err
		}
		_, sourceChanged := s.lease.SourceSignal()
		_, changed, closed := e.direct.snapshot()
		if closed {
			return ErrClosed
		}
		if err := e.ProbeDirect(ctx); err != nil && !errors.Is(err, ErrNoAuthority) {
			return err
		}
		if s.lease.Permissions().NewFlows && s.pathReady(e) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sourceChanged:
		case <-changed:
		case <-poll.C:
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
	if err := s.packets.Err(); err != nil {
		return err
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
	if !s.pathReady(e) {
		return ErrNoAuthority
	}
	return nil
}

func (s *WindowsSession) pathReady(e *Engine) bool {
	choices := s.packets.outputChoices()
	for _, kind := range choices {
		if kind == peerRouteRelay || kind == peerRouteDirect {
			return true
		}
	}
	paths, _, closed := e.direct.snapshot()
	if closed {
		return false
	}
	for _, path := range paths {
		if _, overridden := choices[path.PeerID]; path.Authenticated && !overridden {
			return true
		}
	}
	return false
}

func (s *WindowsSession) ProbePaths(parent context.Context) error {
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
	if err := s.packets.Err(); err != nil {
		return err
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
