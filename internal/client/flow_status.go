package client

import (
	"log/slog"
	"time"
)

// FlowLogStatus contains only bounded operational values, never flow identities
// or payload metadata. Counters are cumulative for this engine's lifetime.
type FlowLogStatus struct {
	Enabled                                                             bool
	ActiveWindows, PendingWindows                                       int
	DroppedPackets, DiscardedWindows                                    uint64
	UnsupportedPackets, CapacityDrops, ClockDrops                       uint64
	ReportAttempts, AcknowledgedWindows, ReportFailures, PolicyFailures uint64
}

func (e *WireGuardEngine) FlowLogStatus() FlowLogStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.flows.status(time.Now())
}

func (c *flowCollector) status(now time.Time) FlowLogStatus {
	if c == nil {
		return FlowLogStatus{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version != 0 && !now.Before(c.expires) {
		c.clearLocked()
	}
	return FlowLogStatus{Enabled: c.version != 0, ActiveWindows: len(c.active), PendingWindows: len(c.pending), DroppedPackets: c.droppedPackets, DiscardedWindows: c.droppedWindows, UnsupportedPackets: c.unsupportedPackets, CapacityDrops: c.capacityDrops, ClockDrops: c.clockDrops, ReportAttempts: c.reportAttempts, AcknowledgedWindows: c.acknowledgedWindows, ReportFailures: c.reportFailures, PolicyFailures: c.policyFailures}
}

func (c *flowCollector) reportResult(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reportAttempts++
	if !success {
		c.reportFailures++
	}
}
func (c *flowCollector) policyFailed() { c.mu.Lock(); defer c.mu.Unlock(); c.policyFailures++ }

func logFlowStatus(logger *slog.Logger, s FlowLogStatus) {
	logger.Info("flow log runtime", "enabled", s.Enabled, "active_windows", s.ActiveWindows, "pending_windows", s.PendingWindows,
		"dropped_packets_total", s.DroppedPackets, "discarded_windows_total", s.DiscardedWindows,
		"unsupported_packets_total", s.UnsupportedPackets, "capacity_drops_total", s.CapacityDrops, "clock_drops_total", s.ClockDrops,
		"report_attempts_total", s.ReportAttempts, "acknowledged_windows_total", s.AcknowledgedWindows, "report_failures_total", s.ReportFailures, "policy_failures_total", s.PolicyFailures)
}
