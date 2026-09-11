package tests

import (
	"context"
	"net/http"
	"runtime"
	"testing"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-052/HC-053: subscriptions expose current state and recover with a new
// snapshot after EOF, using the published stream-local sequence contract.
func TestControlPlaneIPCEvents(t *testing.T) {
	_, n, id := controlScenario(t)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	type subscription struct {
		events <-chan ipc.Event
		done   <-chan struct{}
		cancel context.CancelFunc
	}
	subscribe := func() subscription {
		t.Helper()
		local, err := ipc.NewLocalClient(endpoint)
		if err != nil {
			t.Fatal("could not open native IPC event transport")
		}
		ctx, cancel := context.WithCancel(t.Context())
		events := make(chan ipc.Event, 32)
		done := make(chan struct{})
		go func() {
			defer close(done)
			// Either EOF or a transport error ends a subscription. The caller
			// verifies when it ends and reconnects through a new public client.
			_ = local.Stream(ctx, http.MethodGet, ipc.PathEvents, nil, func(event ipc.Event) error {
				select {
				case events <- event:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("cancelled IPC subscription did not terminate")
			}
			local.HTTPClient.CloseIdleConnections()
		})
		return subscription{events: events, done: done, cancel: cancel}
	}
	lastSequence := 0
	await := func(stream subscription, match func(ipc.Event) bool) ipc.Event {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		for {
			select {
			case event := <-stream.events:
				if event.Sequence <= lastSequence || event.IPCProtocol != ipc.Protocol || event.IPCNegotiatedVersion != ipc.Version {
					t.Fatal("IPC event sequence or protocol metadata is invalid")
				}
				if _, err := time.Parse(time.RFC3339Nano, event.GeneratedAt); err != nil {
					t.Fatal("IPC event has an invalid generation timestamp")
				}
				if lastSequence == 0 && event.EventType != ipc.EventTypeHello {
					t.Fatal("IPC subscription did not begin with hello")
				}
				lastSequence = event.Sequence
				if event.EventType == ipc.EventTypeError {
					t.Fatal("healthy IPC subscription returned an error event")
				}
				if match(event) {
					return event
				}
			case <-stream.done:
				t.Fatal("IPC subscription ended before the expected event")
			case <-ctx.Done():
				t.Fatal("IPC subscription did not report the expected state within the deadline")
			}
		}
	}
	state := func(disconnected bool) func(ipc.Event) bool {
		return func(event ipc.Event) bool {
			return event.EventType == ipc.EventTypeStatusChanged && event.Status != nil && event.Status.NodeID == id && event.Status.CachedMapValid && event.Status.UserDisconnected == disconnected
		}
	}
	stream := subscribe()
	await(stream, func(event ipc.Event) bool { return event.EventType == ipc.EventTypeHello })
	await(stream, state(false))
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	event := await(stream, state(true))
	if event.Status.DesiredState != ipc.DesiredDisconnected {
		t.Fatal("event stream lost disconnected intent")
	}
	n.Stop()
	select {
	case <-stream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("IPC subscription survived agent termination")
	}
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid && v.UserDisconnected })
	lastSequence = 0
	stream = subscribe()
	await(stream, func(event ipc.Event) bool { return event.EventType == ipc.EventTypeHello })
	await(stream, state(true))
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	event = await(stream, state(false))
	if event.Status.DesiredState != ipc.DesiredConnected {
		t.Fatal("reconnected event stream lost connected intent")
	}
	stream.cancel()
	select {
	case <-stream.done:
	case <-time.After(3 * time.Second):
		t.Fatal("explicit subscription cancellation did not terminate")
	}
}
