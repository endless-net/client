package connectivity

import (
	"context"
	"crypto/rand"
	"errors"
	"math"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FlowMonitor is owned by one native connection session. Queue entries never
// reach disk. Once selected for delivery an observation becomes immutable.
type FlowMonitor struct {
	collecting atomic.Bool
	mu         sync.Mutex
	ctx        context.Context
	policy     *FlowPolicy
	timer      *time.Timer
	last       ClockSample
	capacity   int
	capture    *FilterInterval
	queue      []*wire.FlowWindow
	open       map[flowTuple]*wire.FlowWindow
	// Loss summaries have their own bounded storage even when queue is full.
	loss            []*wire.ReportFlowCoverageRequest // at most one pending entry per direction and decision
	sendingCoverage *wire.ReportFlowCoverageRequest
	complete        []pendingCoverage
}

// Delivery is FIFO. A receipt for the last queued window of the class also
// confirms all earlier windows belonging to the interval.
type pendingCoverage struct {
	report     *wire.ReportFlowCoverageRequest
	lastWindow *wire.FlowWindow
}

type flowClass struct {
	direction wire.FlowDirection
	decision  string
}

type flowTuple struct {
	flowClass
	source, destination netip.Addr
	protocol            string
	port                uint16
}

func (m *FlowMonitor) resetLocked() {
	m.collecting.Store(false)
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	clear(m.queue)
	m.queue = nil
	m.open = nil
	m.endCaptureLocked()
	m.loss, m.sendingCoverage, m.complete = nil, nil, nil
	m.policy = nil
	m.ctx = nil
}

func (m *FlowMonitor) Collecting() bool {
	if !m.collecting.Load() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.currentLocked()
	return ok
}

func (m *FlowMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resetLocked()
}

func (p *FlowPolicy) remaining(sample ClockSample) time.Duration {
	if !sample.valid() || !p.received.valid() || sample.Epoch != p.received.Epoch || sample.UpperUptime < p.received.Uptime || sample.Wall.Before(p.received.Wall) {
		return 0
	}
	remaining := min(p.end.Sub(sample.Wall), p.end.Sub(p.received.Wall)-(sample.UpperUptime-p.received.Uptime))
	if !p.opening.serverTime.IsZero() {
		upper, err := p.opening.upper(sample)
		if err != nil {
			return 0
		}
		remaining = min(remaining, p.end.Sub(upper))
	}
	return remaining
}

// activate discards old data before adopting a different consent or credential.
// A routine map stream rotation can keep the exact pending windows.
func (m *FlowMonitor) activate(ctx context.Context, p *FlowPolicy, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// The stream owner handles cancellation and may resume an unchanged consent.
	if ctx.Err() != nil {
		return
	}
	if p == nil || p.end.IsZero() || n < 1 {
		m.resetLocked()
		return
	}
	sample, err := p.verifier.config.Clock.Sample()
	if err != nil || p.remaining(sample) <= 0 {
		m.resetLocked()
		return
	}
	old := m.policy
	if old == nil || old.version != p.version || old.verifier.credential != p.verifier.credential || old.verifier.profileID != p.verifier.profileID || old.verifier.generation != p.verifier.generation || !old.start.Equal(p.start) || old.remaining(sample) <= 0 || sample.Wall.Before(m.last.Wall) || sample.Uptime < m.last.UpperUptime {
		m.resetLocked()
		m.open = make(map[flowTuple]*wire.FlowWindow)
	} else if m.ctx.Err() != nil || m.ctx.Done() != ctx.Done() || old.verifier != p.verifier || old.opening != p.opening || old.openingReceipt != p.openingReceipt || (m.capture != nil && p.remaining(sample) < m.capture.policy.remaining(sample)) {
		// Pending delivery can cross a stream rotation. Native counter
		// baselines cannot span a gap in continuous observation.
		m.endCaptureLocked()
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.ctx, m.policy, m.last, m.capacity = ctx, p, sample, n
	m.collecting.Store(true)
	m.timer = time.AfterFunc(p.remaining(sample), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.policy == p {
			m.resetLocked()
		}
	})
}

func (m *FlowMonitor) currentLocked() (ClockSample, bool) {
	if m.policy == nil || m.ctx.Err() != nil {
		return ClockSample{}, false
	}
	sample, err := m.policy.verifier.config.Clock.Sample()
	if err != nil || m.policy.remaining(sample) <= 0 || sample.Wall.Before(m.last.Wall) || sample.Uptime < m.last.UpperUptime {
		m.resetLocked()
		return ClockSample{}, false
	}
	m.last = sample
	return sample, true
}

// ObserveEgress receives tuple metadata after the native outbound filter. It
// never accepts packet bytes. A verified map bounds the observer's clock.
func (m *FlowMonitor) ObserveEgress(value Map, source, destination netip.Addr, protocol string, port uint16, bytes uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, ok := m.currentLocked()
	if !ok || !source.Is4() || !destination.Is4() || bytes == 0 {
		return
	}
	if m.mapRemainingLocked(value, now) <= 0 {
		return
	}
	m.recordLocked(flowTuple{flowClass{wire.FlowDirection_FLOW_DIRECTION_EGRESS, "allow"}, source, destination, protocol, port}, now.Wall, now.Wall.Add(time.Nanosecond), 1, bytes)
}

func (m *FlowMonitor) mapRemainingLocked(value Map, now ClockSample) time.Duration {
	v := m.policy.verifier
	if value.profileID != v.profileID || value.generation != v.generation || value.snapshot.Node.ID != v.node || value.snapshot.Network.ID != v.network || value.snapshot.Network.AccountID != v.account || value.denied || m.policy.opening.serverTime.IsZero() || value.verified.Epoch != now.Epoch || now.Uptime < value.verified.UpperUptime {
		return 0
	}
	upper, err := m.policy.opening.upper(now)
	lower := m.policy.opening.serverTime.Add(now.Uptime - m.policy.openingReceipt.UpperUptime)
	if err != nil || now.Wall.Before(m.policy.start) || lower.Before(m.policy.start) || !upper.Before(m.policy.end) || !upper.Before(value.ExpiresAt()) {
		return 0
	}
	return max(0, min(value.ExpiresAt().Sub(upper), value.ExpiresAt().Sub(now.Wall)))
}

// FilterCounter contains a delta between two native filter counter snapshots.
type FilterCounter struct {
	Source, Destination netip.Addr
	Protocol            string
	DestinationPort     uint16
	Direction           wire.FlowDirection
	Decision            string
	Packets, Bytes      uint64
}

// FilterInterval belongs to the consent and verified map at the first snapshot.
type FilterInterval struct {
	policy *FlowPolicy
	cursor state.MapCursor
	start  time.Time
	ctx    context.Context
	cancel context.CancelFunc
}

// Done wakes a native observer when the stream, map lifetime or consent ends.
func (c *FilterInterval) Done() <-chan struct{} { return c.ctx.Done() }

func (m *FlowMonitor) endCaptureLocked() {
	if m.capture != nil {
		m.capture.cancel()
		m.capture = nil
	}
}

func (m *FlowMonitor) BeginFilterInterval(value Map) *FilterInterval {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, ok := m.currentLocked()
	if !ok {
		return nil
	}
	left := min(m.mapRemainingLocked(value, now), m.policy.remaining(now))
	if left <= 0 {
		return nil
	}
	m.endCaptureLocked()
	ctx, cancel := context.WithTimeout(m.ctx, left)
	m.capture = &FilterInterval{policy: m.policy, start: now.Wall, cursor: value.Cursor(), ctx: ctx, cancel: cancel}
	return m.capture
}

// FilterIntervalRemaining bounds kernel observation by both grants and the
// signed map. Query it immediately before arming a native observer.
func (m *FlowMonitor) FilterIntervalRemaining(capture *FilterInterval, value Map) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if capture == nil || m.capture != capture || capture.ctx.Err() != nil || capture.cursor != value.Cursor() {
		return 0
	}
	now, ok := m.currentLocked()
	if !ok {
		return 0
	}
	return max(0, min(capture.policy.remaining(now), m.policy.remaining(now), m.mapRemainingLocked(value, now)))
}

// FilterObservationTime samples the same clock used for reporting windows.
func (m *FlowMonitor) FilterObservationTime(capture *FilterInterval) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, ok := m.currentLocked()
	if !ok || capture == nil || capture != m.capture || capture.ctx.Err() != nil {
		return time.Time{}
	}
	return now.Wall
}

// FinishFilterInterval consumes a capture once. A changed policy or map,
// including a stream rotation, requires the observer to take a new baseline.
func (m *FlowMonitor) FinishFilterInterval(capture *FilterInterval, value Map, counters []FilterCounter) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishFilterIntervalLocked(capture, value, counters, nil, true)
}

// FinishFilterSnapshots consumes two coherent native counter snapshots. A
// discontinuity marks the interval partial for all classes under the same grant.
func (m *FlowMonitor) FinishFilterSnapshots(capture *FilterInterval, value Map, before, after FilterSnapshot, observedFrom, observedUntil time.Time) bool {
	counters, partial, valid := filterDeltas(before, after)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.finishFilterIntervalLocked(capture, value, counters, partial, valid) {
		return false
	}
	if before.TableHandle == 0 || observedFrom.Before(capture.start) || !observedUntil.After(observedFrom) || observedUntil.After(m.last.Wall) {
		return true
	}
	lastWindow := make(map[FilterClass]*wire.FlowWindow)
	for _, w := range m.queue {
		class := FilterClass{w.Direction, w.Decision}
		if before.Totals[class] != after.Totals[class] {
			lastWindow[class] = w
		}
	}
	for class := range after.Totals {
		if len(m.complete) >= m.capacity {
			break
		}
		if slices.ContainsFunc(m.loss, func(loss *wire.ReportFlowCoverageRequest) bool {
			return loss.Direction == class.Direction && loss.Decision == class.Decision &&
				loss.StartInclusive.AsTime().Before(observedUntil) && loss.EndExclusive.AsTime().After(observedFrom)
		}) {
			continue
		}
		report := &wire.ReportFlowCoverageRequest{
			NodeId: m.policy.verifier.node, ConsentVersion: m.policy.version, ReportId: rand.Text(),
			Direction: class.Direction, Decision: class.Decision, Coverage: wire.FlowCoverage_FLOW_COVERAGE_COMPLETE,
			StartInclusive: timestamppb.New(observedFrom), EndExclusive: timestamppb.New(observedUntil),
		}
		m.complete = append(m.complete, pendingCoverage{report: report, lastWindow: lastWindow[class]})
	}
	return true
}

func (m *FlowMonitor) finishFilterIntervalLocked(capture *FilterInterval, value Map, counters []FilterCounter, partial []FilterClass, valid bool) bool {
	if capture == nil || capture != m.capture {
		return false
	}
	active := capture.ctx.Err() == nil
	m.endCaptureLocked()
	now, ok := m.currentLocked()
	if !active || !ok || capture.policy.remaining(now) <= 0 || capture.cursor != value.Cursor() || !now.Wall.After(capture.start) || m.mapRemainingLocked(value, now) <= 0 {
		return false
	}
	if !valid {
		for _, direction := range []wire.FlowDirection{wire.FlowDirection_FLOW_DIRECTION_INGRESS, wire.FlowDirection_FLOW_DIRECTION_EGRESS} {
			for _, decision := range []string{"allow", "deny"} {
				m.recordLossLocked(flowClass{direction, decision}, capture.start, now.Wall)
			}
		}
		return false
	}
	for _, c := range counters {
		if !c.Source.Is4() || !c.Destination.Is4() || c.Packets == 0 || c.Bytes == 0 ||
			(c.Direction != wire.FlowDirection_FLOW_DIRECTION_INGRESS && c.Direction != wire.FlowDirection_FLOW_DIRECTION_EGRESS) ||
			(c.Decision != "allow" && c.Decision != "deny") {
			return false
		}
	}
	for _, c := range counters {
		key := flowTuple{flowClass{c.Direction, c.Decision}, c.Source, c.Destination, c.Protocol, c.DestinationPort}
		m.recordLocked(key, capture.start, now.Wall, c.Packets, c.Bytes)
	}
	for _, class := range partial {
		m.recordLossLocked(flowClass{class.Direction, class.Decision}, capture.start, now.Wall)
	}
	return true
}

func (m *FlowMonitor) recordLocked(key flowTuple, start, end time.Time, packets, bytes uint64) {
	if key.protocol != "tcp" && key.protocol != "udp" {
		m.recordLossLocked(key.flowClass, start, end)
		return
	}
	w := m.open[key]
	if w == nil {
		if len(m.queue) >= m.capacity {
			m.recordLossLocked(key.flowClass, start, end)
			return
		}
		w = &wire.FlowWindow{WindowId: rand.Text(), Source: key.source.String(), Destination: key.destination.String(), Protocol: key.protocol, DestinationPort: uint32(key.port), Decision: key.decision, Direction: key.direction, WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(end)}
		m.open[key] = w
		m.queue = append(m.queue, w)
	}
	if w.GetBytes() > math.MaxUint64-bytes || w.GetPackets() > math.MaxUint64-packets {
		m.recordLossLocked(key.flowClass, start, end)
		return
	}
	if packets > 0 {
		if w.Packets == nil {
			w.Bytes, w.Packets = new(uint64), new(uint64)
		}
		*w.Bytes += bytes
		*w.Packets += packets
	}
	if start.Before(w.WindowStart.AsTime()) {
		w.WindowStart = timestamppb.New(start)
	}
	if end.After(w.WindowEnd.AsTime()) {
		w.WindowEnd = timestamppb.New(end)
	}
}

// ObserveClassification records an ALE decision. WFP supplies no byte or
// packet counters. Packet observations of this tuple can supply them later.
func (m *FlowMonitor) ObserveClassification(capture *FilterInterval, value Map, at time.Time, local, remote netip.Addr, port uint16, protocol string, ingress, allowed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, ok := m.currentLocked()
	if !ok || capture == nil || m.capture != capture || capture.ctx.Err() != nil || capture.cursor != value.Cursor() ||
		at.Before(capture.start) || at.After(now.Wall) || m.mapRemainingLocked(value, now) <= 0 || port == 0 ||
		local.String() != value.snapshot.Node.AssignedIP || !remote.Is4() || (protocol != "tcp" && protocol != "udp") {
		return
	}
	prefix, err := netip.ParsePrefix(value.snapshot.Network.CIDR)
	if err != nil || !prefix.Contains(remote) {
		return
	}
	key := flowTuple{flowClass{wire.FlowDirection_FLOW_DIRECTION_EGRESS, "deny"}, local, remote, protocol, port}
	if ingress {
		key.direction, key.source, key.destination = wire.FlowDirection_FLOW_DIRECTION_INGRESS, remote, local
	}
	if allowed {
		key.decision = "allow"
	}
	m.recordLocked(key, at, at.Add(time.Nanosecond), 0, 0)
}

func (m *FlowMonitor) recordLossLocked(class flowClass, start, end time.Time) {
	for _, report := range m.loss {
		if report.Direction == class.direction && report.Decision == class.decision {
			if start.Before(report.StartInclusive.AsTime()) {
				report.StartInclusive = timestamppb.New(start)
			}
			if end.After(report.EndExclusive.AsTime()) {
				report.EndExclusive = timestamppb.New(end)
			}
			return
		}
	}
	m.loss = append(m.loss, &wire.ReportFlowCoverageRequest{
		NodeId: m.policy.verifier.node, ConsentVersion: m.policy.version, ReportId: rand.Text(),
		Direction: class.direction, Decision: class.decision, Coverage: wire.FlowCoverage_FLOW_COVERAGE_PARTIAL,
		StartInclusive: timestamppb.New(start), EndExclusive: timestamppb.New(end),
	})
}

// Delivery shares a queue across routine stream rotations. Check the caller
// under the same lock used to freeze a window or claim a coverage report.
func (m *FlowMonitor) ownsDeliveryLocked(ctx context.Context, v *Verifier) bool {
	return m.policy != nil && m.policy.verifier == v && m.ctx.Done() == ctx.Done() && ctx.Err() == nil
}

func (m *FlowMonitor) nextCoverage(ctx context.Context, v *Verifier) (*FlowPolicy, *wire.ReportFlowCoverageRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ownsDeliveryLocked(ctx, v) {
		return nil, nil
	}
	if _, ok := m.currentLocked(); !ok {
		return nil, nil
	}
	if m.sendingCoverage == nil && len(m.loss) > 0 {
		m.sendingCoverage = m.loss[0]
		m.loss = slices.Delete(m.loss, 0, 1)
	}
	if m.sendingCoverage == nil {
		for i, pending := range m.complete {
			if pending.lastWindow == nil {
				m.sendingCoverage = pending.report
				m.complete = slices.Delete(m.complete, i, i+1)
				break
			}
		}
	}
	if m.sendingCoverage != nil && m.sendingCoverage.EndExclusive.AsTime().After(m.last.Wall) {
		return nil, nil
	}
	return m.policy, m.sendingCoverage
}

func (m *FlowMonitor) next(ctx context.Context, v *Verifier) (p *FlowPolicy, w *wire.FlowWindow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ownsDeliveryLocked(ctx, v) {
		return nil, nil
	}
	if _, ok := m.currentLocked(); !ok || len(m.queue) == 0 {
		return nil, nil
	}
	w = m.queue[0]
	for key, active := range m.open {
		if active == w {
			delete(m.open, key)
			break
		}
	}
	if w.WindowEnd.AsTime().After(m.last.Wall) {
		return nil, nil
	}
	return m.policy, w
}

func (m *FlowMonitor) acknowledge(p *FlowPolicy, w *wire.FlowWindow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.policy == p && len(m.queue) > 0 && m.queue[0] == w {
		m.queue = slices.Delete(m.queue, 0, 1)
		for i := range m.complete {
			if m.complete[i].lastWindow == w {
				m.complete[i].lastWindow = nil
			}
		}
	}
}

// Run uses one serial refresh/delivery loop for one map stream. A cancellation
// suspends observations immediately; the connection owner decides if a normal
// stream rotation may resume the buffer before its independent expiry timer.
func (m *FlowMonitor) Run(ctx context.Context, t *Transport, v *Verifier, opening Map, interval time.Duration) {
	if t == nil || v == nil || interval <= 0 {
		return
	}
	claims, err := coordinator.DecodeNodeCredential(v.credential, v.config.MaxCredentialBytes)
	if err != nil || !slices.Contains(claims.Scopes, coordinator.NodeFlowScope) {
		m.Reset()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		policy, err := t.ReadFlowPolicy(ctx, v)
		if err == nil {
			policy.opening, policy.openingReceipt = opening.timeProof, opening.verified
			if policy.opening.serverTime.IsZero() {
				m.Reset()
				return
			}
			m.activate(ctx, policy, t.config.MaxRecords)
		} else if ctx.Err() == nil && !errors.Is(err, ErrUnavailable) && !errors.Is(err, context.DeadlineExceeded) {
			m.Reset()
		}
		m.flush(ctx, t, v, interval)
		// A short consent can expire before the normal refresh tick. Give
		// observations made under it a delivery opportunity before expiry.
		if policy != nil && !policy.end.IsZero() {
			sample, e := v.config.Clock.Sample()
			if left := policy.remaining(sample); e == nil && left > 0 && left <= interval {
				timer := time.NewTimer(left / 2)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				m.flush(ctx, t, v, interval)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *FlowMonitor) flush(ctx context.Context, t *Transport, v *Verifier, interval time.Duration) {
	until := time.Now().Add(interval)
	for i := 0; i < 4 && time.Now().Before(until); i++ {
		p, report := m.nextCoverage(ctx, v)
		if report == nil {
			break
		}
		err := t.reportCoverage(ctx, v, p, report)
		m.mu.Lock()
		if p == m.policy && m.sendingCoverage == report {
			if err == nil {
				m.sendingCoverage = nil
			} else if terminalFlowError(err) {
				m.resetLocked()
			}
		}
		m.mu.Unlock()
		if err != nil || report.Coverage != wire.FlowCoverage_FLOW_COVERAGE_COMPLETE {
			break
		}
	}
	for i := 0; i < t.config.MaxRecords && ctx.Err() == nil && time.Now().Before(until); i++ {
		p, w := m.next(ctx, v)
		if w == nil {
			break
		}
		err := t.ReportFlowWindow(ctx, v, p, w)
		if err != nil {
			if terminalFlowError(err) {
				m.mu.Lock()
				if m.policy == p {
					m.resetLocked()
				}
				m.mu.Unlock()
			}
			break
		}
		m.acknowledge(p, w)
	}
}

func terminalFlowError(err error) bool {
	return errors.Is(err, ErrDenied) || errors.Is(err, ErrExpired) || errors.Is(err, ErrCredentialInvalid) || errors.Is(err, ErrProtocol) || errors.Is(err, ErrClock)
}
