//go:build windows

package dataplane

import (
	"context"

	"github.com/endless-net/client/internal/connectivity"
)

// CheckReady fences local activation after the endpoint ACK. The daemon worker
// still owns completion of the operation under the final intent fence.
func (s *WindowsSession) CheckReady(ctx context.Context) (err error) {
	b, err := connectivity.OperationFromContext(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if b != s.operation || !b.Active() {
		return connectivity.ErrConfig
	}
	defer func() {
		if err != nil {
			err = s.failUpdate(err)
		}
	}()
	if err := s.packets.Err(); err != nil {
		return err
	}
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	if err := connectivity.CheckOperation(ctx); err != nil {
		return err
	}
	if !s.tunnelReady() {
		return ErrNoAuthority
	}
	return nil
}

// Caller holds s.mu. Current must be a verified map that passed native
// application, listener, and source fences.
func (s *WindowsSession) tunnelReady() bool {
	if !s.started || s.stopped || s.engine == nil || s.tunnel == nil || s.filters == nil || s.ip == nil || s.packets == nil || s.packets.Err() != nil || !s.lease.Permissions().NewFlows || s.current.Denied() {
		return false
	}
	m, err := s.current.Snapshot()
	if err != nil || m.Revision.Network == 0 || m.Node.AssignedIP == "" {
		return false
	}
	_, err = s.engine.BoundPort()
	return err == nil
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
