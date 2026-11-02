package localapi

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type observationFunction func() connectivity.Observation

func (f observationFunction) ConnectionSnapshot() connectivity.Observation { return f() }

func nextWatchEvent(t *testing.T, values <-chan *clientv0.WatchEventsResponse) *clientv0.WatchEventsResponse {
	t.Helper()
	select {
	case event := <-values:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("event missing")
		return nil
	}
}

func TestWatchEventsStartsWithCallerFilteredSnapshotAndOrdersVolatileStatus(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	view := connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseConnecting}
	c.config.ConnectionObservations = observationFunction(func() connectivity.Observation { mu.Lock(); defer mu.Unlock(); return view })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	values := make(chan *clientv0.WatchEventsResponse, 16)
	done := make(chan error, 1)
	go func() {
		done <- c.watch(ctx, caller, func(ctx context.Context, event *clientv0.WatchEventsResponse) error {
			select {
			case values <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	first := nextWatchEvent(t, values)
	if first.Sequence != 1 || first.GetSnapshot() == nil || first.GetSnapshot().Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_CONNECTING || first.Metadata.Revision != 4 || len(first.GetSnapshot().Runtime.Capabilities) != 2 {
		t.Fatal("initial snapshot/identity/capability missing", first)
	}
	mu.Lock()
	view.Phase = connectivity.PhaseConnected
	view.Local = connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{NewFlows: true, ExistingFlows: true}, DirectPaths: 1}
	mu.Unlock()
	second := nextWatchEvent(t, values)
	if second.Sequence != 2 || second.GetStatusChanged() == nil || second.GetStatusChanged().ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED || second.Metadata.Revision != first.Metadata.Revision {
		t.Fatal("volatile state lost, reordered or changed durable CAS", second)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch cancellation did not finish")
	}
	s, err := c.store.Load(context.Background())
	if err != nil || s.Revision != 4 || s.Profiles[defaultID].Connection.Intent != state.IntentConnected {
		t.Fatal("watch cancellation changed durable intent", err)
	}
	// Reconnect has its own sequence and current full snapshot, not replay.
	again, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		done <- c.watch(again, caller, func(_ context.Context, event *clientv0.WatchEventsResponse) error {
			values <- event
			stop()
			return nil
		})
	}()
	reconnected := nextWatchEvent(t, values)
	if reconnected.Sequence != 1 || reconnected.GetSnapshot() == nil || reconnected.GetSnapshot().Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED {
		t.Fatal("reconnect resumed stale stream", reconnected)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconnected stream did not stop")
	}
}

func TestEventQueueCountsInFlightAndRejectsCountAndByteOverflow(t *testing.T) {
	q := newEventQueue()
	for range eventCapacity {
		if err := q.push(&clientv0.WatchEventsResponse{Sequence: 1}); err != nil {
			t.Fatal(err)
		}
	}
	inflight := <-q.values
	if status.Code(q.push(&clientv0.WatchEventsResponse{Sequence: 2})) != codes.ResourceExhausted {
		t.Fatal("in-flight event escaped count bound")
	}
	q.sent(inflight)
	if err := q.push(&clientv0.WatchEventsResponse{Sequence: 2}); err != nil {
		t.Fatal("acknowledged event did not release capacity", err)
	}
	oversize := &clientv0.WatchEventsResponse{Event: &clientv0.WatchEventsResponse_Failure{Failure: &clientv0.Failure{ReasonKey: strings.Repeat("x", eventByteCapacity)}}}
	if status.Code(newEventQueue().push(oversize)) != codes.ResourceExhausted {
		t.Fatal("byte limit ignored")
	}
}

func TestSlowEventConsumerGetsTypedLimitInsteadOfHiddenDrops(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	if _, err := c.connect(context.Background(), caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	var revision atomic.Uint64
	c.config.ConnectionObservations = observationFunction(func() connectivity.Observation {
		return connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseConnected, Local: connectivity.LocalObservation{Known: true, Applied: true, MapRevision: revision.Add(1)}}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	senderStopped := make(chan struct{})
	err := c.watch(ctx, caller, func(ctx context.Context, _ *clientv0.WatchEventsResponse) error {
		defer close(senderStopped)
		<-ctx.Done()
		return ctx.Err()
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatal("slow consumer hung or dropped queued state", err)
	}
	details := status.Convert(err).Details()
	if len(details) != 1 || details[0].(*clientv0.Failure).Code != clientv0.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED {
		t.Fatal("overflow missing typed failure")
	}
	select {
	case <-senderStopped:
	case <-time.After(time.Second):
		t.Fatal("cancelled event sender leaked")
	}
}
