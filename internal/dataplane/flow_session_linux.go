package dataplane

import (
	"context"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

// One worker owns the observation sets until Stop removes the recorded table.
// Session mutations serialize with each kernel operation, but not the wait.
func (s *LinuxSession) observeFilter() {
	var cleanup bool
	for s.ctx.Err() == nil {
		if s.observeFilterInterval(&cleanup) {
			continue
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(s.config.RetryInterval):
		}
	}
}

func (s *LinuxSession) observeFilterInterval(cleanup *bool) bool {
	s.mu.Lock()
	if s.stopped || s.ctx.Err() != nil {
		s.mu.Unlock()
		return false
	}
	if *cleanup {
		*cleanup = !s.removeFlowSets()
	}
	epoch, changed := s.lease.SourceSignal()
	if *cleanup || !s.lease.Permissions().NewFlows {
		s.mu.Unlock()
		return false
	}
	value := s.current
	capture := s.flows.BeginFilterInterval(value)
	if capture == nil {
		s.mu.Unlock()
		return false
	}
	before, left, attempted := s.armFlowSets(capture, value)
	*cleanup = attempted
	// Only the time between completed installation and the start of sealing
	// is guaranteed to be covered by this counter generation.
	observedFrom := s.flows.FilterObservationTime(capture)
	s.mu.Unlock()

	if before.Generation != 0 {
		timer := time.NewTimer(min(s.config.ProbeInterval, left/2))
		select {
		case <-timer.C:
		case <-changed:
		case <-capture.Done():
		case <-s.ctx.Done():
		}
		timer.Stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	currentEpoch, _ := s.lease.SourceSignal()
	if !s.stopped && s.ctx.Err() == nil && currentEpoch == epoch && s.lease.Permissions().NewFlows && s.flows.FilterIntervalRemaining(capture, s.current) > 0 {
		observedUntil := s.flows.FilterObservationTime(capture)
		var after connectivity.FilterSnapshot
		if before.Generation != 0 {
			ctx, cancel := context.WithTimeout(s.ctx, s.config.ProbeInterval)
			after, _ = sealLinuxFlowSnapshot(ctx, s.receipt, before, s.config.FlowDumpBytes)
			cancel()
		}
		if latest, _ := s.lease.SourceSignal(); latest == epoch {
			s.flows.FinishFilterSnapshots(capture, s.current, before, after, observedFrom, observedUntil)
		}
	}
	if *cleanup && !s.stopped {
		*cleanup = !s.removeFlowSets()
	}
	return !*cleanup && before.Generation != 0
}

func (s *LinuxSession) armFlowSets(capture *connectivity.FilterInterval, value connectivity.Map) (connectivity.FilterSnapshot, time.Duration, bool) {
	var zero connectivity.FilterSnapshot
	ctx, cancel := context.WithTimeout(s.ctx, s.config.ProbeInterval)
	defer cancel()
	handle, err := linuxNFTLookup(ctx, s.receipt)
	if err != nil || handle == 0 {
		return zero, 0, false
	}
	left := s.flows.FilterIntervalRemaining(capture, value)
	rules, err := linuxFlowRules(s.identity, true, s.config.FlowCapacity, left)
	if err != nil {
		return zero, 0, false
	}
	// Even a timed-out command may have installed its batch.
	if linuxNFTApply(ctx, rules) != nil {
		return zero, 0, true
	}
	before, err := dumpLinuxFlowCounters(ctx, s.receipt, s.config.FlowDumpBytes, 0)
	if err != nil || before.TableHandle != handle {
		return zero, 0, true
	}
	// Exclusive creation started all counters at zero, including packets
	// observed by the time the generation handle was read.
	clear(before.Details)
	for class := range before.Totals {
		before.Totals[class] = connectivity.PacketCounts{}
	}
	return before, left, true
}

func (s *LinuxSession) removeFlowSets() bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), s.config.ProbeInterval)
	defer cancel()
	handle, err := linuxNFTLookup(ctx, s.receipt)
	if err != nil || handle == 0 {
		return err == nil
	}
	rules, err := linuxFlowRules(s.identity, false, 0, 0)
	return err == nil && linuxNFTApply(ctx, rules) == nil
}
