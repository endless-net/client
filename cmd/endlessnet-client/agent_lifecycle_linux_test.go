//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
	"github.com/godbus/dbus/v5"
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
	sessions       map[string]logindSessionIdentity
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
func (s *testLogindSession) sessionSnapshot() map[string]logindSessionIdentity {
	return cloneLogindSessions(s.sessions)
}

func TestLogindSessionIdentityAndRemoval(t *testing.T) {
	path := dbus.ObjectPath("/org/freedesktop/login1/session/_42")
	sessions, err := decodeLogindSessions([][]any{{"42", uint32(1001), "owner", "seat0", path}})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][][]any{{{"42", "1001", "owner", "seat0", path}}, {{"42", uint32(1001), "owner", "seat0", path}, {"42", uint32(1002), "other", "seat0", path}}, {{"", uint32(1001), "owner", "seat0", path}}} {
		if _, err := decodeLogindSessions(invalid); err == nil {
			t.Fatal("malformed or duplicate session inventory accepted", invalid)
		}
	}
	events := make(chan client.RuntimeLifecycleNotification, 1)
	if err := removeLogindSession(t.Context(), events, sessions, "42", dbus.ObjectPath("/wrong")); err == nil || len(events) != 0 || len(sessions) != 1 {
		t.Fatal("wrong session path altered owner binding", err)
	}
	if err := removeLogindSession(t.Context(), events, sessions, "42", path); err != nil {
		t.Fatal(err)
	}
	event := <-events
	if event.Event != client.RuntimeUserLogoff || event.SessionOwner != "uid:1001" || len(sessions) != 0 {
		t.Fatal("logoff did not bind the trusted UID", event)
	}
	if err := removeLogindSession(t.Context(), events, sessions, "42", path); err == nil {
		t.Fatal("duplicate logoff reused an expired session binding")
	}
}

func TestLogindReconnectionReplaysOnlyMissingBoundSessions(t *testing.T) {
	previous := map[string]logindSessionIdentity{
		"old":  {uid: 1001, path: dbus.ObjectPath("/session/old")},
		"same": {uid: 1002, path: dbus.ObjectPath("/session/same")},
	}
	current := map[string]logindSessionIdentity{"same": previous["same"]}
	events := make(chan client.RuntimeLifecycleNotification, 2)
	if err := deliverMissedLogoffs(t.Context(), events, previous, current); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || (<-events).SessionOwner != "uid:1001" {
		t.Fatal("reconnect missed or invented owner logoff")
	}
	current["renumbered"] = logindSessionIdentity{uid: 1001, path: dbus.ObjectPath("/session/new")}
	if err := deliverMissedLogoffs(t.Context(), events, previous, current); err != nil || len(events) != 0 {
		t.Fatal("renumbered active UID was mistaken for logoff", err)
	}
	delete(current, "renumbered")
	full := make(chan client.RuntimeLifecycleNotification, 1)
	full <- client.RuntimeLifecycleNotification{Event: client.RuntimeSuspend}
	if err := deliverMissedLogoffs(t.Context(), full, previous, current); err == nil {
		t.Fatal("overflow silently lost a logoff event")
	}
}

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
