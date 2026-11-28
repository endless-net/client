package connectivity

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
	"google.golang.org/protobuf/proto"
)

func TestFlowMonitorBoundsReplayAndOwnership(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	opening := mapTimeAuthority{serverTime: f.now, sent: sample}
	grant := &FlowPolicy{verifier: v, version: 1, start: f.now.Add(-time.Hour), end: f.now.Add(time.Minute), received: sample, opening: opening, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	m.activate(ctx, grant, 2)
	observe := func(value Map, port uint16) {
		f.now = f.now.Add(time.Millisecond)
		m.ObserveEgress(value, netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("100.70.0.2"), "tcp", port, 120)
	}
	foreign := value
	foreign.generation++
	observe(foreign, 123)
	if len(m.queue) != 0 {
		t.Fatal("accepted foreign connection")
	}
	observe(value, 443)
	observe(value, 443)
	f.now = f.now.Add(time.Nanosecond)
	_, first := m.next(ctx, v)
	if first == nil || first.GetPackets() != 2 || first.GetBytes() != 240 {
		t.Fatal("lost packet counters")
	}
	frozen := proto.Clone(first)
	observe(value, 443)
	observe(value, 80)
	if len(m.queue) != 2 || !proto.Equal(first, frozen) || m.queue[1].WindowId == first.WindowId {
		t.Fatal("capacity or sealed identity changed")
	}
	f.now = f.now.Add(time.Nanosecond)
	_, loss := m.nextCoverage(ctx, v)
	if loss == nil || loss.Coverage != wire.FlowCoverage_FLOW_COVERAGE_PARTIAL || loss.Direction != wire.FlowDirection_FLOW_DIRECTION_EGRESS || loss.Decision != "allow" || loss.NodeId != v.node || loss.ConsentVersion != grant.version {
		t.Fatal("queue overflow did not mark the observed class as partial")
	}
	original := proto.Clone(loss)
	observe(value, 81)
	if _, retry := m.nextCoverage(ctx, v); !proto.Equal(original, retry) || len(m.loss) == 0 || m.loss[0].ReportId == loss.ReportId {
		t.Fatal("later loss changed the pending report")
	}
	reportCount := 0
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		assertNodeRequest(t, r, f)
		if r.URL.Path == publicv1connect.FlowLogServiceReportFlowLogProcedure {
			return nil, io.ErrUnexpectedEOF
		}
		payload, err := io.ReadAll(r.Body)
		got := new(wire.ReportFlowCoverageRequest)
		if err != nil || proto.Unmarshal(payload, got) != nil || !proto.Equal(got, original) || r.URL.Path != publicv1connect.FlowLogServiceReportFlowCoverageProcedure {
			t.Fatal("loss report changed at node boundary")
		}
		reportCount++
		if reportCount == 1 {
			return nil, io.ErrUnexpectedEOF
		}
		return testwire.UnaryResponse(r, &wire.ReportFlowCoverageResponse{ReportId: got.ReportId})
	})
	m.flush(ctx, transport, v, time.Second)
	if reportCount != 1 || m.sendingCoverage != loss {
		t.Fatal("lost receipt cleared pending loss")
	}
	m.flush(ctx, transport, v, time.Second)
	if reportCount != 2 || m.sendingCoverage != nil || len(m.loss) == 0 || len(m.queue) != 2 {
		t.Fatal("receipt affected another loss or an unconfirmed window")
	}
	stop()
	// A policy response racing with stream rotation must not replace or erase the buffer.
	m.activate(ctx, &FlowPolicy{verifier: v, version: 2}, 2)
	observe(value, 443)
	if _, w := m.next(ctx, v); w != nil || m.queue[1].GetPackets() != 1 {
		t.Fatal("cancelled connection collected or delivered data")
	}
	replacement := *grant
	copyVerifier := *v
	replacement.verifier = &copyVerifier
	m.activate(t.Context(), &replacement, 2)
	// An outgoing loop can finish polling after the replacement stream has
	// inherited pending windows and coverage. Only the new owner may flush.
	foreignCtx, cancelForeign := context.WithCancel(t.Context())
	defer cancelForeign()
	m.flush(foreignCtx, transport, &copyVerifier, time.Second)
	m.flush(ctx, transport, v, time.Second)
	if !m.Collecting() || len(m.loss) == 0 || m.sendingCoverage != nil {
		t.Fatal("old stream consumed or discarded the replacement's coverage")
	}
	if _, w := m.next(t.Context(), &copyVerifier); !proto.Equal(w, frozen) {
		t.Fatal("routine rotation lost pending report")
	}
	m.acknowledge(grant, first)
	if len(m.queue) != 2 {
		t.Fatal("stale receipt consumed current queue")
	}
	m.acknowledge(&replacement, first)
	if len(m.queue) != 1 {
		t.Fatal("receipt failed to release capacity")
	}

	// A newly read policy is a distinct immutable object.
	updated := *grant
	updated.version = 3
	m.activate(t.Context(), &updated, 2)
	if len(m.queue) != 0 || len(m.loss) != 0 || m.sendingCoverage != nil {
		t.Fatal("new consent inherited old windows")
	}
	observe(value, 123)
	if len(m.queue) != 1 {
		t.Fatal("new consent never activated")
	}
	m.activate(t.Context(), &FlowPolicy{verifier: v, version: 4}, 2)
	if m.Collecting() || len(m.queue) != 0 {
		t.Fatal("disabled consent retained data")
	}
}

func TestFlowMonitorCountsPacketsAtOneClockReading(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	p := &FlowPolicy{verifier: v, version: 1, start: f.now, end: f.now.Add(time.Minute), received: sample, opening: mapTimeAuthority{serverTime: f.now, sent: sample}, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	m.activate(t.Context(), p, 2)
	observe := func(port uint16) {
		m.ObserveEgress(value, netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("100.70.0.2"), "udp", port, 32)
	}
	at := f.now
	observe(53)
	observe(53)
	if _, w := m.next(t.Context(), v); w != nil {
		t.Fatal("sent an interval before its exclusive end")
	}
	observe(53)
	observe(443)
	if _, loss := m.nextCoverage(t.Context(), v); loss != nil {
		t.Fatal("sent loss before its exclusive end")
	}
	f.now = at.Add(time.Nanosecond)
	_, first := m.next(t.Context(), v)
	if first == nil || first.GetPackets() != 2 || first.GetBytes() != 64 || !first.WindowStart.AsTime().Equal(at) || !first.WindowEnd.AsTime().Equal(f.now) {
		t.Fatal("first interval did not preserve its packets")
	}
	m.acknowledge(p, first)
	_, second := m.next(t.Context(), v)
	if second == nil || second.GetPackets() != 1 || second.WindowId == first.WindowId {
		t.Fatal("packet after sealing was lost")
	}
	_, loss := m.nextCoverage(t.Context(), v)
	if loss == nil || !loss.StartInclusive.AsTime().Equal(at) || !loss.EndExclusive.AsTime().Equal(f.now) {
		t.Fatal("overflow at the same clock reading was not covered")
	}
}

func TestFilterIntervalKeepsClassesAndRejectsReplay(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	p := &FlowPolicy{verifier: v, version: 1, start: f.now, end: f.now.Add(40 * time.Second), received: sample, opening: mapTimeAuthority{serverTime: f.now, sent: sample}, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	m.activate(t.Context(), p, 4)
	capture := m.BeginFilterInterval(value)
	start := f.now
	f.now = f.now.Add(time.Second)
	// A policy refresh returns a new Go value even on the same map stream.
	refresh := *p
	refresh.received, _ = f.config.Clock.Sample()
	refresh.end = refresh.end.Add(time.Minute)
	m.activate(t.Context(), &refresh, 4)
	if left := m.FilterIntervalRemaining(capture, value); left != 39*time.Second {
		t.Fatal("refresh extended kernel observation beyond initial consent", left)
	}
	var rows []FilterCounter
	for _, dir := range []wire.FlowDirection{wire.FlowDirection_FLOW_DIRECTION_INGRESS, wire.FlowDirection_FLOW_DIRECTION_EGRESS} {
		for _, decision := range []string{"allow", "deny"} {
			rows = append(rows, FilterCounter{Source: netip.MustParseAddr("10.0.0.1"), Destination: netip.MustParseAddr("10.0.0.2"), Protocol: "tcp", DestinationPort: 443, Direction: dir, Decision: decision, Packets: 3, Bytes: 153})
		}
	}
	if !m.FinishFilterInterval(capture, value, rows) || m.FinishFilterInterval(capture, value, rows) || len(m.queue) != 4 {
		t.Fatal("wrong snapshot replay or queue size")
	}
	for i, w := range m.queue {
		if w.Direction != rows[i].Direction || w.Decision != rows[i].Decision || w.GetPackets() != 3 || w.GetBytes() != 153 || !w.WindowStart.AsTime().Equal(start) || !w.WindowEnd.AsTime().Equal(f.now) {
			t.Fatal("wrong observed counter interval", w)
		}
	}
	capture = m.BeginFilterInterval(value)
	for i := range rows {
		rows[i].DestinationPort = 22
	}
	f.now = f.now.Add(time.Second)
	if !m.FinishFilterInterval(capture, value, rows) || len(m.loss) != 4 {
		t.Fatal("queue overflow merged distinct classes")
	}
	for i, loss := range m.loss {
		if loss.Direction != rows[i].Direction || loss.Decision != rows[i].Decision {
			t.Fatal("wrong loss class", loss)
		}
	}
	capture = m.BeginFilterInterval(value)
	replacement := *p
	replacement.version++
	m.activate(t.Context(), &replacement, 4)
	select {
	case <-capture.Done():
	default:
		t.Fatal("old native observer still armed after consent replacement")
	}
	f.now = f.now.Add(time.Second)
	if m.FinishFilterInterval(capture, value, rows) || len(m.queue) != 0 {
		t.Fatal("changed consent inherited counter deltas")
	}
	stale := m.BeginFilterInterval(value)
	otherStream, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.activate(otherStream, &replacement, 4)
	select {
	case <-stale.Done():
	default:
		t.Fatal("old native observer still armed after stream rotation")
	}
	if m.FilterIntervalRemaining(stale, value) != 0 || m.FinishFilterInterval(stale, value, rows) {
		t.Fatal("stream rotation retained counter authority")
	}
	before := FilterSnapshot{TableHandle: 1, Generation: 7, Totals: make(map[FilterClass]PacketCounts)}
	after := FilterSnapshot{TableHandle: 1, Generation: 7, Totals: make(map[FilterClass]PacketCounts), Details: make(map[FilterTuple]PacketCounts)}
	for _, row := range rows {
		class := FilterClass{row.Direction, row.Decision}
		before.Totals[class] = PacketCounts{100, 8000}
		after.Totals[class] = PacketCounts{105, 8300}
		key := FilterTuple{class, row.Source, row.Destination, row.Protocol, row.DestinationPort}
		after.Details[key] = PacketCounts{5, 300}
		if class.Decision == "deny" {
			after.Details[key] = PacketCounts{3, 180}
		}
	}
	capture = m.BeginFilterInterval(value)
	f.now = f.now.Add(time.Second)
	if !m.FinishFilterSnapshots(capture, value, before, after, capture.start, f.now) || len(m.loss) != 2 || len(m.queue) != 4 {
		t.Fatal("failed to account real filter deltas by class")
	}
	if len(m.complete) != 2 {
		t.Fatal("only lossless native classes may report complete")
	}
	for _, pending := range m.complete {
		if pending.report.Decision != "allow" || pending.lastWindow == nil {
			t.Fatal("inconsistent class or missing receipt dependency", pending.report)
		}
	}
	for _, loss := range m.loss {
		if loss.Decision != "deny" {
			t.Fatal("reported loss in unaffected class", loss)
		}
	}
	for _, w := range m.queue {
		if w.Decision == "deny" && (w.GetPackets() != 3 || w.GetBytes() != 180) || w.Decision == "allow" && (w.GetPackets() != 5 || w.GetBytes() != 300) {
			t.Fatal("wrong observed counters", w)
		}
	}
	capture = m.BeginFilterInterval(value)
	f.now = f.now.Add(time.Second)
	after.Generation++
	if m.FinishFilterSnapshots(capture, value, before, after, capture.start, f.now) {
		t.Fatal("subtracted snapshots across ruleset replacement")
	}
	if len(m.loss) != 4 || len(m.queue) != 4 {
		t.Fatal("discontinuity did not preserve packets and mark missing observation")
	}
	after.Generation--
	if m.FinishFilterSnapshots(capture, value, before, after, capture.start, f.now) {
		t.Fatal("reused discarded capture")
	}
	for key := range after.Details {
		before.Details = map[FilterTuple]PacketCounts{key: {6, 400}}
		break
	}
	if _, _, ok := filterDeltas(before, after); ok {
		t.Fatal("decreasing detail accepted")
	}
	capture = m.BeginFilterInterval(value)
	extended := replacement
	extended.end = start.Add(2 * time.Minute)
	extended.received, _ = f.config.Clock.Sample()
	m.activate(otherStream, &extended, 4)
	f.now = replacement.end
	if left := m.FilterIntervalRemaining(capture, value); left != 0 || m.FinishFilterInterval(capture, value, rows) {
		t.Fatal("original consent deadline did not fence kernel counters", left)
	}
	capture = m.BeginFilterInterval(value)
	if left := m.FilterIntervalRemaining(capture, value); left != value.ExpiresAt().Sub(f.now) {
		t.Fatal("new baseline exceeded map deadline", left)
	}
	shortened := extended
	shortened.end = f.now.Add(5 * time.Second)
	m.activate(otherStream, &shortened, 4)
	select {
	case <-capture.Done():
	default:
		t.Fatal("shorter consent left the previous native timer armed")
	}
	capture = m.BeginFilterInterval(value)
	cancel()
	select {
	case <-capture.Done():
	default:
		t.Fatal("ending stream did not wake native observer")
	}
}

func TestQuietNativeIntervalReportsOnlyObservedTime(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	p := &FlowPolicy{verifier: v, version: 1, start: f.now, end: f.now.Add(time.Minute), received: sample, opening: mapTimeAuthority{serverTime: f.now, sent: sample}, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	m.activate(t.Context(), p, 4)
	snap := FilterSnapshot{TableHandle: 1, Generation: 7, Totals: map[FilterClass]PacketCounts{}}
	for _, dir := range []wire.FlowDirection{wire.FlowDirection_FLOW_DIRECTION_INGRESS, wire.FlowDirection_FLOW_DIRECTION_EGRESS} {
		for _, decision := range []string{"allow", "deny"} {
			snap.Totals[FilterClass{dir, decision}] = PacketCounts{}
		}
	}
	capture := m.BeginFilterInterval(value)
	f.now = f.now.Add(time.Second)
	from := m.FilterObservationTime(capture)
	f.now = f.now.Add(time.Second)
	until := m.FilterObservationTime(capture)
	f.now = f.now.Add(time.Second)
	if !m.FinishFilterSnapshots(capture, value, snap, snap, from, until) || len(m.queue) != 0 || len(m.complete) != 4 {
		t.Fatal("missing quiet interval or fabricated traffic")
	}
	seen := map[FilterClass]bool{}
	var lost *wire.ReportFlowCoverageRequest
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		assertNodeRequest(t, r, f)
		b, err := io.ReadAll(r.Body)
		report := new(wire.ReportFlowCoverageRequest)
		if err != nil || proto.Unmarshal(b, report) != nil || r.URL.Path != publicv1connect.FlowLogServiceReportFlowCoverageProcedure || report.Coverage != wire.FlowCoverage_FLOW_COVERAGE_COMPLETE || !report.StartInclusive.AsTime().Equal(from) || !report.EndExclusive.AsTime().Equal(until) {
			t.Fatal("incorrect quiet coverage")
		}
		if lost == nil {
			lost = proto.CloneOf(report)
			return nil, io.ErrUnexpectedEOF
		}
		if len(seen) == 0 && !proto.Equal(report, lost) {
			t.Fatal("retry changed interval or id")
		}
		seen[FilterClass{report.Direction, report.Decision}] = true
		return testwire.UnaryResponse(r, &wire.ReportFlowCoverageResponse{ReportId: report.ReportId})
	})
	m.flush(t.Context(), transport, v, time.Second)
	m.flush(t.Context(), transport, v, time.Second)
	if len(seen) != 4 || len(m.complete) != 0 || m.sendingCoverage != nil {
		t.Fatal("quiet reports were not acknowledged")
	}
}
func TestActiveCoverageWaitsForLastWindowReceipt(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	p := &FlowPolicy{verifier: v, version: 1, start: f.now, end: f.now.Add(time.Minute), received: sample, opening: mapTimeAuthority{serverTime: f.now, sent: sample}, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	ctx := t.Context()
	m.activate(ctx, p, 4)
	before := FilterSnapshot{TableHandle: 1, Generation: 1, Totals: map[FilterClass]PacketCounts{}}
	for _, dir := range []wire.FlowDirection{wire.FlowDirection_FLOW_DIRECTION_INGRESS, wire.FlowDirection_FLOW_DIRECTION_EGRESS} {
		for _, decision := range []string{"allow", "deny"} {
			before.Totals[FilterClass{dir, decision}] = PacketCounts{}
		}
	}
	after := before
	after.Totals = maps.Clone(before.Totals)
	after.Details = map[FilterTuple]PacketCounts{}
	active := FilterClass{wire.FlowDirection_FLOW_DIRECTION_EGRESS, "allow"}
	after.Totals[active] = PacketCounts{2, 120}
	for _, port := range []uint16{80, 443} {
		key := FilterTuple{active, netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2"), "tcp", port}
		after.Details[key] = PacketCounts{1, 60}
	}
	c := m.BeginFilterInterval(value)
	f.now = f.now.Add(time.Second)
	if !m.FinishFilterSnapshots(c, value, before, after, c.start, f.now) {
		t.Fatal("snapshot rejected")
	}
	for range 3 {
		_, r := m.nextCoverage(ctx, v)
		if r == nil || (r.Direction == active.Direction && r.Decision == active.Decision) {
			t.Fatal("unexpected coverage before delivery", r)
		}
		m.sendingCoverage = nil
	}
	policy, w := m.next(ctx, v)
	m.acknowledge(policy, w)
	old, last := m.next(ctx, v)
	if last == nil || last == w {
		t.Fatal("missing second window")
	}
	rotated := *p
	m.activate(ctx, &rotated, 4)
	m.acknowledge(old, last)
	if _, r := m.nextCoverage(ctx, v); r != nil {
		t.Fatal("stale receipt released coverage")
	}
	policy, retry := m.next(ctx, v)
	if retry != last {
		t.Fatal("rotation changed pending window")
	}
	m.acknowledge(policy, retry)
	_, r := m.nextCoverage(ctx, v)
	if r == nil || r.Direction != active.Direction || r.Decision != active.Decision || r.Coverage != wire.FlowCoverage_FLOW_COVERAGE_COMPLETE {
		t.Fatal("missing acknowledged coverage", r)
	}
	// The kernel tracked both tuples. Dropping one locally must still make
	// this class partial, whichever tuple the bounded queue accepted.
	m.Reset()
	m.activate(ctx, &rotated, 1)
	c = m.BeginFilterInterval(value)
	f.now = f.now.Add(time.Second)
	if !m.FinishFilterSnapshots(c, value, before, after, c.start, f.now) {
		t.Fatal("overflow snapshot rejected")
	}
	_, loss := m.nextCoverage(ctx, v)
	if loss == nil || loss.Coverage != wire.FlowCoverage_FLOW_COVERAGE_PARTIAL || loss.Direction != active.Direction || loss.Decision != active.Decision {
		t.Fatal("missing local overflow report", loss)
	}
	for _, pending := range m.complete {
		if pending.report.Direction == active.Direction && pending.report.Decision == active.Decision {
			t.Fatal("local overflow advertised full coverage")
		}
	}
}
func TestFlowMonitorExpiresWhileIdleAndAcrossSuspend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newMapFixture(t, coordinator.NodeFlowScope)
		v, err := NewVerifier(f.profile, f.config)
		if err != nil {
			t.Fatal(err)
		}
		now, _ := f.config.Clock.Sample()
		f.sign(t, nil, f.now, f.now.Add(time.Minute))
		value := Map{profileID: v.profileID, generation: v.generation, verified: now, snapshot: f.value}
		p := &FlowPolicy{verifier: v, version: 7, start: now.Wall, end: now.Wall.Add(time.Second), received: now, opening: mapTimeAuthority{serverTime: f.now, sent: now}, openingReceipt: now}
		m := new(FlowMonitor)
		defer m.Reset()
		m.activate(t.Context(), p, 1)
		if !m.Collecting() {
			t.Fatal("missing valid consent")
		}
		f.now = f.now.Add(time.Millisecond)
		m.ObserveEgress(value, netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("100.70.0.2"), "udp", 53, 36)
		f.now = f.now.Add(time.Nanosecond)
		_, pending := m.next(t.Context(), v)
		if pending == nil {
			t.Fatal("fixture did not retain an unconfirmed window")
		}
		started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
			close(started)
			<-release
			return testwire.UnaryResponse(r, &wire.ReportFlowLogResponse{WindowId: pending.WindowId})
		})
		go func() { defer close(finished); m.flush(t.Context(), transport, v, time.Second) }()
		<-started
		time.Sleep(time.Second)
		synctest.Wait()
		if m.Collecting() || m.policy != nil || len(m.queue) != 0 || len(m.open) != 0 {
			t.Fatal("expiry required traffic or refresh")
		}
		close(release)
		<-finished
		if m.Collecting() || len(m.queue) != 0 {
			t.Fatal("late receipt revived expired consent")
		}
		m.activate(t.Context(), p, 1)
		next := now
		next.Uptime += time.Second
		next.UpperUptime += time.Second
		v.config.Clock = clockFunc(func() (ClockSample, error) { return next, nil })
		if p, w := m.next(t.Context(), v); p != nil || w != nil || m.Collecting() {
			t.Fatal("suspend extended consent")
		}
	})
}

func TestClassificationRequiresCurrentCaptureAndLeavesCountersUnmeasured(t *testing.T) {
	f := newMapFixture(t, coordinator.NodeFlowScope)
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := f.config.Clock.Sample()
	f.sign(t, nil, f.now, f.now.Add(time.Minute))
	p := &FlowPolicy{verifier: v, version: 1, start: f.now, end: f.now.Add(40 * time.Second), received: sample, opening: mapTimeAuthority{serverTime: f.now, sent: sample}, openingReceipt: sample}
	value := Map{profileID: v.profileID, generation: v.generation, verified: sample, snapshot: f.value}
	m := new(FlowMonitor)
	defer m.Reset()
	m.activate(t.Context(), p, 4)
	capture := m.BeginFilterInterval(value)
	local, remote := netip.MustParseAddr("100.70.0.1"), netip.MustParseAddr("100.70.0.2")
	f.now = f.now.Add(time.Second)
	m.ObserveClassification(capture, value, f.now, local, remote, 53, "udp", true, false)
	m.ObserveClassification(capture, value, f.now, local, remote, 443, "tcp", false, true)
	if len(m.queue) != 2 || m.queue[0].Bytes != nil || m.queue[0].Packets != nil || m.queue[0].Direction != wire.FlowDirection_FLOW_DIRECTION_INGRESS || m.queue[0].Source != remote.String() || m.queue[0].Decision != "deny" {
		t.Fatal("classification changed tuple or invented counters", m.queue)
	}
	m.ObserveEgress(value, local, remote, "tcp", 443, 120)
	m.ObserveClassification(capture, value, f.now, local, remote, 443, "tcp", false, true)
	if len(m.queue) != 2 || m.queue[1].GetPackets() != 1 || m.queue[1].GetBytes() != 120 {
		t.Fatal("ALE reauthorization changed measured counts", m.queue)
	}
	next := m.BeginFilterInterval(value)
	m.ObserveClassification(capture, value, f.now, local, remote, 80, "tcp", false, true)
	m.ObserveClassification(next, value, f.now.Add(-time.Second), local, remote, 80, "tcp", false, true)
	m.ObserveClassification(next, value, f.now, local, netip.MustParseAddr("192.0.2.10"), 80, "tcp", false, true)
	if len(m.queue) != 2 {
		t.Fatal("stale or out-of-scope event accepted")
	}
}
