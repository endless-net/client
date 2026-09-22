//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
)

func TestLogindInhibitorClosesOnlyAfterSuspendConfirmation(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	events := make(chan client.RuntimeLifecycleNotification)
	session := &logindSession{inhibitor: write, delay: time.Second}
	finished := make(chan error, 1)
	go func() { finished <- session.suspend(t.Context(), events) }()
	event := <-events
	if event.Event != client.RuntimeSuspend || event.Completion == nil || event.Deadline.IsZero() {
		t.Fatal("suspend was not delivered with a completion deadline")
	}
	if _, err := write.Write([]byte{1}); err != nil {
		t.Fatal("inhibitor closed before teardown confirmation", err)
	}
	event.Completion <- nil
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if _, err := write.Write([]byte{1}); err == nil {
		t.Fatal("inhibitor remained open after confirmed teardown")
	}
}

func TestLogindUnconfirmedSuspendExpiresAndClosesInhibitor(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	events := make(chan client.RuntimeLifecycleNotification, 1)
	session := &logindSession{inhibitor: write, delay: 10 * time.Millisecond}
	err = session.suspend(t.Context(), events)
	if err == nil || !session.failedSuspend || session.inhibitor != nil {
		t.Fatal("unconfirmed transition did not fail closed", err)
	}
}

func TestLogindDeliveryHonorsCancellationAndCompletion(t *testing.T) {
	events := make(chan client.RuntimeLifecycleNotification)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sendLogindEvent(ctx, events, client.RuntimeSuspend, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled source queued a transition", err)
	}
	completed := make(chan error, 1)
	go func() { completed <- sendLogindEvent(t.Context(), events, client.RuntimeResume, time.Time{}) }()
	event := <-events
	want := errors.New("resume rejected")
	event.Completion <- want
	if err := <-completed; !errors.Is(err, want) {
		t.Fatal("source lost runtime completion", err)
	}
}

type testLogindSession struct {
	preparingValue bool
	listenResult   error
	entered        chan struct{}
}

func (s *testLogindSession) preparing(context.Context) (bool, error) { return s.preparingValue, nil }
func (s *testLogindSession) suspend(context.Context, chan<- client.RuntimeLifecycleNotification) error {
	return nil
}
func (s *testLogindSession) resume(ctx context.Context, events chan<- client.RuntimeLifecycleNotification, event client.RuntimeLifecycleEvent) error {
	return sendLogindEvent(ctx, events, event, time.Time{})
}
func (s *testLogindSession) listen(ctx context.Context, _ chan<- client.RuntimeLifecycleNotification) error {
	if s.entered != nil {
		close(s.entered)
		<-ctx.Done()
		return ctx.Err()
	}
	return s.listenResult
}
func (s *testLogindSession) close()              {}
func (s *testLogindSession) suspendFailed() bool { return false }
func (s *testLogindSession) markSleeping()       {}
func (s *testLogindSession) isSleeping() bool    { return false }
func (s *testLogindSession) hasResumed() bool    { return false }

func TestLogindSourceLossClosesGateBeforeRecoveredResume(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := make(chan client.RuntimeLifecycleNotification)
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runLogindSourceWith(ctx, events, &testLogindSession{listenResult: errors.New("bus lost")}, func(context.Context) (logindLifecycleSession, error) {
			return &testLogindSession{entered: entered}, nil
		})
	}()
	for _, want := range []client.RuntimeLifecycleEvent{client.RuntimeSourceLost, client.RuntimeSourceRecovered} {
		select {
		case event := <-events:
			if event.Event != want {
				t.Fatalf("got %v, want %v", event.Event, want)
			}
			event.Completion <- nil
		case <-time.After(5 * time.Second):
			t.Fatal("source did not recover")
		}
	}
	<-entered
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
