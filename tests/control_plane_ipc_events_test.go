package tests

import (
	"bytes"
	"context"
	"encoding/json"
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
	// Exercise the shipping CLI as well as SDK subscriptions. A normal listening
	// timeout must flush complete NDJSON records and exit successfully without
	// changing the agent's connection intent.
	cliEvents := func(disconnected bool) {
		t.Helper()
		started := time.Now()
		output, err := n.ServiceCommand("events", "--timeout", "2s")
		elapsed := time.Since(started)
		if err != nil || elapsed < 2*time.Second || elapsed > 10*time.Second {
			t.Fatal("CLI event subscription failed or did not respect its listening timeout with process-startup allowance (output withheld)")
		}
		sequence, sawStatus := 0, false
		for _, line := range bytes.Split(bytes.TrimSpace(output), []byte("\n")) {
			var event ipc.Event
			if err := json.Unmarshal(line, &event); err != nil {
				t.Fatal("CLI event subscription emitted an incomplete or invalid JSON record")
			}
			if event.Sequence <= sequence || event.IPCProtocol != ipc.Protocol || event.IPCNegotiatedVersion != ipc.Version || event.EventType == ipc.EventTypeError {
				t.Fatal("CLI event subscription emitted invalid metadata or an error event")
			}
			if sequence == 0 && event.EventType != ipc.EventTypeHello {
				t.Fatal("CLI event subscription did not begin with hello")
			}
			sequence = event.Sequence
			if event.EventType == ipc.EventTypeStatusChanged && event.Status != nil && event.Status.NodeID == id && event.Status.CachedMapValid && event.Status.UserDisconnected == disconnected {
				sawStatus = true
			}
		}
		if !sawStatus {
			t.Fatal("CLI event subscription omitted the current enrolled status")
		}
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.NodeID == id && v.CachedMapValid && v.UserDisconnected == disconnected
		})
	}
	cliEvents(false)
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	type subscription struct {
		events       <-chan ipc.Event
		done         <-chan struct{}
		cancel       context.CancelFunc
		lastSequence int
	}
	subscribe := func() *subscription {
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
		return &subscription{events: events, done: done, cancel: cancel}
	}
	await := func(stream *subscription, match func(ipc.Event) bool) ipc.Event {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		for {
			select {
			case event := <-stream.events:
				if event.Sequence <= stream.lastSequence || event.IPCProtocol != ipc.Protocol || event.IPCNegotiatedVersion != ipc.Version {
					t.Fatal("IPC event sequence or protocol metadata is invalid")
				}
				if _, err := time.Parse(time.RFC3339Nano, event.GeneratedAt); err != nil {
					t.Fatal("IPC event has an invalid generation timestamp")
				}
				if stream.lastSequence == 0 && event.EventType != ipc.EventTypeHello {
					t.Fatal("IPC subscription did not begin with hello")
				}
				stream.lastSequence = event.Sequence
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
	observer := subscribe()
	await(observer, func(event ipc.Event) bool { return event.EventType == ipc.EventTypeHello })
	await(observer, state(false))
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	event := await(stream, state(true))
	if event.Status.DesiredState != ipc.DesiredDisconnected {
		t.Fatal("event stream lost disconnected intent")
	}
	await(observer, state(true))
	observer.cancel()
	select {
	case <-observer.done:
	case <-time.After(3 * time.Second):
		t.Fatal("independent observer cancellation did not terminate")
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	await(stream, state(false))
	n.Service("disconnect", &disconnected)
	await(stream, state(true))
	n.Stop()
	select {
	case <-stream.done:
	case <-time.After(5 * time.Second):
		t.Fatal("IPC subscription survived agent termination")
	}
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid && v.UserDisconnected })
	cliEvents(true)
	stream = subscribe()
	await(stream, func(event ipc.Event) bool { return event.EventType == ipc.EventTypeHello })
	await(stream, state(true))
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
