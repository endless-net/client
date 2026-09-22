//go:build windows

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestWindowsServicePowerDelivery(t *testing.T) {
	requests := make(chan windowsServiceControl, 8)
	changes := make(chan svc.Status, 8)
	observed := make(chan RuntimeLifecycleNotification, 8)
	s := windowsService{enumerate: func() ([]uint32, error) { return nil, nil }, queryOwner: func(uint32) (string, error) { return "test-SID", nil }, run: func(ctx context.Context, events <-chan RuntimeLifecycleNotification) error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case event := <-events:
				observed <- event
			}
		}
	}}
	done := make(chan uint32, 1)
	go func() { _, code := s.execute(t.Context(), requests, changes); done <- code }()
	if (<-changes).State != svc.StartPending {
		t.Fatal("missing start pending")
	}
	running := <-changes
	if running.State != svc.Running || running.Accepts&svc.AcceptPowerEvent == 0 {
		t.Fatal("power notifications not accepted")
	}
	completion := make(chan error, 1)
	deadline := time.Now().Add(25 * time.Second)
	requests <- windowsServiceControl{Cmd: svc.PowerEvent, EventType: 0x4, Completion: completion, Deadline: deadline}
	requests <- windowsServiceControl{Cmd: svc.PowerEvent, EventType: 0x12}
	for _, expected := range []RuntimeLifecycleEvent{RuntimeSuspend, RuntimeResume} {
		select {
		case got := <-observed:
			if got.Event != expected || got.SessionOwner != "" {
				t.Fatalf("event %v, want %v", got, expected)
			}
			if expected == RuntimeSuspend && (got.Completion != completion || !got.Deadline.Equal(deadline)) {
				t.Fatal("SCM deadline/completion not delivered to executor")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("power event not delivered")
		}
	}
	// User-interaction resume duplicates the automatic wake. A session control
	// without a validated, copied session ID cannot dispatch logoff.
	requests <- windowsServiceControl{Cmd: svc.PowerEvent, EventType: 0x7}
	requests <- windowsServiceControl{Cmd: svc.SessionChange, EventType: 6}
	requests <- windowsServiceControl{Cmd: svc.Stop}
	select {
	case code := <-done:
		if code != 0 || len(observed) != 0 {
			t.Fatal("unexpected event or shutdown failure", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop")
	}
}

func TestWindowsServiceSessionOwnerDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	controls := make(chan windowsServiceControl, 8)
	changes := make(chan svc.Status, 8)
	observed := make(chan RuntimeLifecycleNotification, 8)
	queries := 0
	service := windowsService{
		enumerate: func() ([]uint32, error) { return []uint32{7}, nil },
		queryOwner: func(id uint32) (string, error) {
			if id != 7 {
				return "", errors.New("unavailable")
			}
			queries++
			if queries == 1 {
				return "seed-SID", nil
			}
			return "new-SID", nil
		},
		run: func(ctx context.Context, events <-chan RuntimeLifecycleNotification) error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case event := <-events:
					observed <- event
				}
			}
		},
	}
	done := make(chan uint32, 1)
	go func() { _, code := service.execute(ctx, controls, changes); done <- code }()
	<-changes
	if running := <-changes; running.Accepts&svc.AcceptSessionChange == 0 {
		t.Fatal("session change not accepted")
	}
	expect := func(event RuntimeLifecycleEvent, owner string) {
		t.Helper()
		select {
		case actual := <-observed:
			if actual.Event != event || actual.SessionOwner != owner {
				t.Fatal("wrong session owner delivered", actual)
			}
		case <-ctx.Done():
			t.Fatal("session event not delivered")
		}
	}
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGOFF, SessionID: 7, SessionValid: true}
	expect(RuntimeUserLogoff, "seed-SID")
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGOFF, SessionID: 7, SessionValid: true}
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGON, SessionID: 7, SessionValid: true}
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGOFF, SessionID: 7, SessionValid: true}
	expect(RuntimeUserLogoff, "new-SID")
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGON, SessionID: 8, SessionValid: true}
	controls <- windowsServiceControl{Cmd: svc.SessionChange, EventType: windows.WTS_SESSION_LOGOFF, SessionID: 8, SessionValid: true}
	controls <- windowsServiceControl{Cmd: svc.PowerEvent, EventType: 0x12}
	expect(RuntimeResume, "")
	controls <- windowsServiceControl{Cmd: svc.Stop}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-ctx.Done():
		t.Fatal("service did not stop")
	}
}

func TestWindowsServiceEnumerationFailureDoesNotStartRuntime(t *testing.T) {
	started := false
	service := windowsService{
		enumerate:  func() ([]uint32, error) { return nil, errors.New("unavailable") },
		queryOwner: func(uint32) (string, error) { t.Fatal("query after enumeration failure"); return "", nil },
		run:        func(context.Context, <-chan RuntimeLifecycleNotification) error { started = true; return nil },
	}
	_, code := service.execute(t.Context(), make(chan windowsServiceControl), make(chan svc.Status, 2))
	if code == 0 || started {
		t.Fatal("runtime started without session source initialization")
	}
}

func TestWindowsServicePowerOverflowCancelsRuntime(t *testing.T) {
	requests := make(chan windowsServiceControl, 65)
	changes := make(chan svc.Status, 8)
	s := windowsService{enumerate: func() ([]uint32, error) { return nil, nil }, queryOwner: func(uint32) (string, error) { return "test-SID", nil }, run: func(ctx context.Context, _ <-chan RuntimeLifecycleNotification) error {
		<-ctx.Done()
		return nil
	}}
	done := make(chan uint32, 1)
	go func() { _, code := s.execute(t.Context(), requests, changes); done <- code }()
	for range 65 {
		requests <- windowsServiceControl{Cmd: svc.PowerEvent, EventType: 0x4}
	}
	select {
	case code := <-done:
		if code != 1 {
			t.Fatal("overflow silently lost lifecycle event", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("overflow blocked control handler")
	}
}
