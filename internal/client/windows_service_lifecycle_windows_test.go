//go:build windows

package client

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

func TestWindowsServicePowerDelivery(t *testing.T) {
	requests := make(chan svc.ChangeRequest, 8)
	changes := make(chan svc.Status, 8)
	observed := make(chan RuntimeLifecycleEvent, 8)
	s := windowsService{run: func(ctx context.Context, events <-chan RuntimeLifecycleEvent) error {
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
	go func() { _, code := s.Execute(nil, requests, changes); done <- code }()
	if (<-changes).State != svc.StartPending {
		t.Fatal("missing start pending")
	}
	running := <-changes
	if running.State != svc.Running || running.Accepts&svc.AcceptPowerEvent == 0 {
		t.Fatal("power notifications not accepted")
	}
	requests <- svc.ChangeRequest{Cmd: svc.PowerEvent, EventType: 0x4, EventData: 1}
	requests <- svc.ChangeRequest{Cmd: svc.PowerEvent, EventType: 0x12, EventData: 1}
	for _, expected := range []RuntimeLifecycleEvent{RuntimeSuspend, RuntimeResume} {
		select {
		case got := <-observed:
			if got != expected {
				t.Fatalf("event %v, want %v", got, expected)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("power event not delivered")
		}
	}
	// User-interaction resume duplicates the automatic wake. Session data
	// deliberately has an invalid pointer: the service must never read it.
	requests <- svc.ChangeRequest{Cmd: svc.PowerEvent, EventType: 0x7}
	requests <- svc.ChangeRequest{Cmd: svc.SessionChange, EventType: 6, EventData: 1}
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	select {
	case code := <-done:
		if code != 0 || len(observed) != 0 {
			t.Fatal("unexpected event or shutdown failure", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop")
	}
}

func TestWindowsServicePowerOverflowCancelsRuntime(t *testing.T) {
	requests := make(chan svc.ChangeRequest, 65)
	changes := make(chan svc.Status, 8)
	s := windowsService{run: func(ctx context.Context, _ <-chan RuntimeLifecycleEvent) error {
		<-ctx.Done()
		return nil
	}}
	done := make(chan uint32, 1)
	go func() { _, code := s.Execute(nil, requests, changes); done <- code }()
	for range 65 {
		requests <- svc.ChangeRequest{Cmd: svc.PowerEvent, EventType: 0x4}
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
