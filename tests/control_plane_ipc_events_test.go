package tests

import (
	"bytes"
	"context"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type nativeEventCursor struct {
	sequence, revision uint64
	instance           string
}

func TestNativeEventCursorRejectsBrokenStreams(t *testing.T) {
	for _, mode := range []string{"valid", "gap", "duplicate", "instance", "revision", "timestamp", "snapshot", "failure", "empty"} {
		t.Run(mode, func(t *testing.T) {
			cursor := nativeEventCursor{}
			first := &ipc.WatchEventsResponse{Sequence: 1, Metadata: &ipc.SnapshotMetadata{InstanceId: "host", Revision: 2, GeneratedAt: timestamppb.Now()},
				Event: &ipc.WatchEventsResponse_Snapshot{Snapshot: &ipc.SnapshotEvent{Runtime: &ipc.RuntimeInfo{InstanceId: "host"}, Status: &ipc.Status{}}}}
			if !cursor.accept(first) {
				t.Fatal("valid opening snapshot rejected")
			}
			next := &ipc.WatchEventsResponse{Sequence: 2, Metadata: proto.Clone(first.Metadata).(*ipc.SnapshotMetadata), Event: &ipc.WatchEventsResponse_StatusChanged{StatusChanged: &ipc.Status{}}}
			switch mode {
			case "gap":
				next.Sequence = 3
			case "duplicate":
				next.Sequence = 1
			case "instance":
				next.Metadata.InstanceId = "other"
			case "revision":
				next.Metadata.Revision = 1
			case "timestamp":
				next.Metadata.GeneratedAt = nil
			case "snapshot":
				next.Event = first.Event
			case "failure":
				next.Event = &ipc.WatchEventsResponse_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE}}
			case "empty":
				next.Event = nil
			}
			before := cursor
			if cursor.accept(next) != (mode == "valid") {
				t.Fatal("incorrect native event validation")
			}
			if mode != "valid" && cursor != before {
				t.Fatal("rejected event advanced stream cursor")
			}
		})
	}
}

func (c *nativeEventCursor) accept(event *ipc.WatchEventsResponse) bool {
	if event == nil || event.Event == nil || event.Sequence != c.sequence+1 || event.GetMetadata().GetInstanceId() == "" ||
		event.GetMetadata().GetRevision() < c.revision || event.GetMetadata().GetGeneratedAt() == nil || event.Metadata.GeneratedAt.CheckValid() != nil || event.GetFailure() != nil {
		return false
	}
	if c.sequence == 0 {
		if event.GetSnapshot() == nil || event.GetSnapshot().GetRuntime().GetInstanceId() != event.Metadata.InstanceId || event.GetSnapshot().GetStatus() == nil {
			return false
		}
	} else if event.GetSnapshot() != nil || event.Metadata.InstanceId != c.instance {
		return false
	}
	c.sequence, c.revision, c.instance = event.Sequence, event.Metadata.Revision, event.Metadata.InstanceId
	return true
}

// HC-052/HC-053: snapshot-first native streams, independent subscribers,
// cancellation and fresh stream-local sequence after host restart.
func TestControlPlaneIPCEvents(t *testing.T) {
	_, n, id := nativeControlScenario(t)
	state := func(event *ipc.WatchEventsResponse, disconnected bool) bool {
		status := event.GetStatusChanged()
		if snapshot := event.GetSnapshot(); snapshot != nil {
			status = snapshot.Status
		}
		return status != nil && status.NodeId == id && status.GetStoredState().GetCachedMapValid() && status.UserDisconnected == disconnected
	}
	cliEvents := func(disconnected bool) {
		t.Helper()
		started := time.Now()
		output, err := n.ServiceCommand("events", "--timeout", "2s")
		if err != nil || time.Since(started) < 2*time.Second || time.Since(started) > 10*time.Second {
			t.Fatal("native CLI event timeout or output failed (output withheld)")
		}
		cursor, sawStatus := nativeEventCursor{}, false
		for _, line := range bytes.Split(bytes.TrimSpace(output), []byte("\n")) {
			event := &ipc.WatchEventsResponse{}
			if protojson.Unmarshal(line, event) != nil || !cursor.accept(event) {
				t.Fatal("CLI emitted invalid native event sequence or snapshot")
			}
			sawStatus = sawStatus || state(event, disconnected)
		}
		if !sawStatus {
			t.Fatal("CLI events omitted current enrolled status")
		}
		n.AwaitNativeStatus(func(v *ipc.Status) bool {
			return v.NodeId == id && v.GetStoredState().GetCachedMapValid() && v.UserDisconnected == disconnected
		})
	}
	cliEvents(false)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	type subscription struct {
		events <-chan *ipc.WatchEventsResponse
		done   <-chan struct{}
		cancel context.CancelFunc
		cursor nativeEventCursor
	}
	subscribe := func() *subscription {
		t.Helper()
		consumer, err := local.NewClient(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		events, done := make(chan *ipc.WatchEventsResponse, 32), make(chan struct{})
		go func() {
			defer close(done)
			defer close(events)
			defer consumer.Close()
			stream, err := consumer.WatchEvents(ctx, connect.NewRequest(&ipc.WatchEventsRequest{}))
			if err != nil {
				return
			}
			defer func() { _ = stream.Close() }()
			for stream.Receive() {
				event := proto.Clone(stream.Msg()).(*ipc.WatchEventsResponse)
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cancelled native stream did not terminate")
			}
		})
		return &subscription{events: events, done: done, cancel: cancel}
	}
	await := func(sub *subscription, match func(*ipc.WatchEventsResponse) bool) *ipc.WatchEventsResponse {
		t.Helper()
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case event, ok := <-sub.events:
				if !ok {
					t.Fatal("native stream ended before expected event")
				}
				if !sub.cursor.accept(event) {
					t.Fatal("native stream violated sequence, timestamp or instance binding")
				}
				if match(event) {
					return event
				}
			case <-timer.C:
				t.Fatal("native stream did not report expected event before deadline")
			}
		}
	}
	awaitState := func(sub *subscription, disconnected bool) *ipc.WatchEventsResponse {
		return await(sub, func(event *ipc.WatchEventsResponse) bool { return state(event, disconnected) })
	}
	stop := func(sub *subscription) {
		t.Helper()
		sub.cancel()
		select {
		case <-sub.done:
		case <-time.After(3 * time.Second):
			t.Fatal("independent stream cancellation did not terminate")
		}
	}
	concurrent := make([]*subscription, 8)
	for i := range concurrent {
		concurrent[i] = subscribe()
	}
	for _, sub := range concurrent {
		awaitState(sub, false)
	}
	for _, sub := range concurrent {
		stop(sub)
	}
	stream, second := subscribe(), subscribe()
	awaitState(stream, false)
	awaitState(second, false)
	runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000001")
	event := awaitState(stream, true)
	if event.GetStatusChanged().GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED {
		t.Fatal("stream lost disconnected intent")
	}
	awaitState(second, true)
	stop(second)
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000002")
	awaitState(stream, false)
	runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000003")
	awaitState(stream, true)
	previousInstance := stream.cursor.instance
	n.Stop()
	select {
	case <-stream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("native stream survived host termination")
	}
	n.Start()
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.GetStoredState().GetCachedMapValid() && v.UserDisconnected
	})
	cliEvents(true)
	stream = subscribe()
	first := awaitState(stream, true)
	if first.Sequence != 1 || stream.cursor.instance == previousInstance {
		t.Fatal("new host stream did not begin with a fresh snapshot and instance")
	}
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000004")
	event = awaitState(stream, false)
	if event.GetStatusChanged().GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED {
		t.Fatal("reconnected stream lost connected intent")
	}
	stop(stream)
}
