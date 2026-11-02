package localapi

import (
	"context"
	"sync"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

const eventCapacity = 64
const eventByteCapacity = 8 << 20

// eventQueue counts the event currently blocked in Send as outstanding too.
// Its limits are the published contract limits, not an unbounded subscriber log.
type eventQueue struct {
	mu           sync.Mutex
	count, bytes int
	values       chan *clientv0.WatchEventsResponse
}

func newEventQueue() *eventQueue {
	return &eventQueue{values: make(chan *clientv0.WatchEventsResponse, eventCapacity)}
}

func eventLimit() error {
	return connectionFailure(codes.ResourceExhausted, clientv0.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, "client.events.limit")
}

func (q *eventQueue) push(event *clientv0.WatchEventsResponse) error {
	size := proto.Size(event)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count >= eventCapacity || size > eventByteCapacity-q.bytes {
		return eventLimit()
	}
	q.count++
	q.bytes += size
	q.values <- event // Single producer; count includes in-flight sends.
	return nil
}

func (q *eventQueue) sent(event *clientv0.WatchEventsResponse) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.count--
	q.bytes -= proto.Size(event)
}

func (c *Controller) WatchEvents(_ *clientv0.WatchEventsRequest, stream grpc.ServerStreamingServer[clientv0.WatchEventsResponse]) error {
	caller, err := localipc.Caller(stream.Context())
	if err != nil {
		return err
	}
	return c.watch(stream.Context(), caller, func(_ context.Context, event *clientv0.WatchEventsResponse) error { return stream.Send(event) })
}

// Sampling creates full current projections, not deltas or an event replay log.
// Each stream owns a finite queue and sequence; cancellation never changes intent.
func (c *Controller) watch(parent context.Context, caller localipc.Principal, send func(context.Context, *clientv0.WatchEventsResponse) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	queue := newEventQueue()
	sendErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-queue.values:
				if err := send(ctx, event); err != nil {
					sendErrors <- err
					cancel()
					return
				}
				queue.sent(event)
			}
		}
	}()
	var sequence uint64
	push := func(event *clientv0.WatchEventsResponse, status *clientv0.Status) error {
		if sequence == ^uint64(0) {
			return eventLimit()
		}
		sequence++
		event.Sequence, event.Metadata = sequence, proto.Clone(status.Metadata).(*clientv0.SnapshotMetadata)
		return queue.push(event)
	}
	var previousStatus *clientv0.Status
	var previousRuntime *clientv0.RuntimeInfo
	tick := time.NewTicker(c.config.WatchInterval)
	defer tick.Stop()
	for {
		current, err := c.snapshot(ctx, caller)
		if err != nil {
			return err
		}
		runtime, err := c.runtimeInfo(ctx, caller)
		if err != nil {
			return err
		}
		value := current.Status
		if previousRuntime == nil || !proto.Equal(previousRuntime, runtime.Runtime) {
			err = push(&clientv0.WatchEventsResponse{Event: &clientv0.WatchEventsResponse_Snapshot{Snapshot: &clientv0.SnapshotEvent{Runtime: runtime.Runtime, Status: value}}}, value)
		} else if !sameEventStatus(previousStatus, value) {
			err = push(&clientv0.WatchEventsResponse{Event: &clientv0.WatchEventsResponse_StatusChanged{StatusChanged: value}}, value)
		}
		if err != nil {
			return err
		}
		if previousStatus != nil && (previousStatus.MapRevision != value.MapRevision || previousStatus.PeerCount != value.PeerCount) {
			if err := push(&clientv0.WatchEventsResponse{Event: &clientv0.WatchEventsResponse_Invalidated{Invalidated: &clientv0.DomainInvalidated{Domain: clientv0.Domain_DOMAIN_PEERS, ProfileId: value.ActiveProfileId}}}, value); err != nil {
				return err
			}
		}
		if previousStatus != nil && previousStatus.ActiveProfileId != value.ActiveProfileId {
			// A profile switch invalidates all profile-scoped cached domains.
			for _, domain := range []clientv0.Domain{clientv0.Domain_DOMAIN_PROFILES, clientv0.Domain_DOMAIN_NETWORKS, clientv0.Domain_DOMAIN_PEERS, clientv0.Domain_DOMAIN_EXIT_NODE, clientv0.Domain_DOMAIN_PREFERENCES, clientv0.Domain_DOMAIN_MANAGED_SETTINGS, clientv0.Domain_DOMAIN_RESOURCES, clientv0.Domain_DOMAIN_SERVER_IDENTITY, clientv0.Domain_DOMAIN_SESSION} {
				if err := push(&clientv0.WatchEventsResponse{Event: &clientv0.WatchEventsResponse_Invalidated{Invalidated: &clientv0.DomainInvalidated{Domain: domain, ProfileId: value.ActiveProfileId}}}, value); err != nil {
					return err
				}
			}
		}
		previousStatus, previousRuntime = value, runtime.Runtime
		select {
		case err := <-sendErrors:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func sameEventStatus(a, b *clientv0.Status) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := proto.Clone(a).(*clientv0.Status), proto.Clone(b).(*clientv0.Status)
	// CAS revision remains the protected Store revision. Sequence orders volatile
	// observations between durable writes; wall time alone is not a state change.
	left.Metadata.GeneratedAt, right.Metadata.GeneratedAt = nil, nil
	return proto.Equal(left, right)
}
