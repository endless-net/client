//go:build windows

package dataplane

import (
	"context"

	"github.com/endless-net/client/internal/connectivity"
)

// CheckReady fences local activation after the endpoint ACK. The daemon worker
// still owns completion of the operation under the final intent fence.
func (s *WindowsSession) CheckReady(ctx context.Context) (err error) {
	report := func(stage string, err error) error {
		if s.config.ReportFailure != nil {
			s.config.ReportFailure(s.generation, stage, err)
		}
		return err
	}
	b, err := connectivity.OperationFromContext(ctx)
	if err != nil {
		return report("apply_readiness_budget", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return report("apply_readiness_resources", ErrClosed)
	}
	if b != s.operation || !b.Active() {
		return report("apply_readiness_budget", connectivity.ErrConfig)
	}
	stage := "apply_readiness_packets"
	defer func() {
		if err != nil && err != connectivity.ErrSourceChanged {
			err = s.failUpdate(report(stage, err))
		}
	}()
	if err := s.packets.Err(); err != nil {
		return err
	}
	stage = "apply_readiness_address"
	if err := s.activationFence(ctx, s.current); err != nil {
		return err
	}
	stage = "apply_readiness_budget"
	if err := connectivity.CheckOperation(ctx); err != nil {
		return err
	}
	stage = s.tunnelReadinessIssue()
	if expected, ok := ctx.Value(sourceEpochKey{}).(uint64); ok {
		if current, _ := s.lease.SourceSignal(); current != expected {
			return report("apply_readiness_source", connectivity.ErrSourceChanged)
		}
	}
	if stage != "" {
		return ErrNoAuthority
	}
	return nil
}

// Caller holds s.mu. Current must be a verified map that passed native
// application, listener, and source fences.
func (s *WindowsSession) tunnelReady() bool {
	return s.tunnelReadinessIssue() == ""
}

func (s *WindowsSession) tunnelReadinessIssue() string {
	if !s.started || s.stopped || s.engine == nil || s.tunnel == nil || s.filters == nil || s.ip == nil || s.packets == nil {
		return "apply_readiness_resources"
	}
	if s.packets.Err() != nil {
		return "apply_readiness_packets"
	}
	if !s.lease.Permissions().NewFlows {
		return "apply_readiness_lease"
	}
	if s.current.Denied() {
		return "apply_readiness_map"
	}
	m, err := s.current.Snapshot()
	if err != nil || m.Revision.Network == 0 || m.Node.AssignedIP == "" {
		return "apply_readiness_map"
	}
	_, err = s.engine.BoundPort()
	if err != nil {
		return "apply_readiness_listener"
	}
	return ""
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
