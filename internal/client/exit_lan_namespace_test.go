package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func exitLANNamespaceTestIdentity() exitLANNamespaceIdentity {
	return exitLANNamespaceIdentity{BootID: "12345678-9abc-4def-8123-456789abcdef", Device: 4, Inode: 5}
}

func TestExitLANNamespaceConstructorOwnsFailedHandles(t *testing.T) {
	readErr, closeErr := errors.New("synthetic inspect"), errors.New("synthetic close")
	for _, scenario := range []string{"stable", "nil_inspect", "pre_cancel", "first_error", "second_error", "first_cancel", "second_cancel", "boot", "device", "inode", "changed", "close_error"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "pre_cancel" {
				cancel()
			}
			reads, closes := 0, 0
			inspect := func(context.Context) (exitLANNamespaceIdentity, error) {
				reads++
				id := exitLANNamespaceTestIdentity()
				if scenario == "first_error" || scenario == "close_error" || scenario == "second_error" && reads == 2 {
					return id, readErr
				}
				if scenario == "first_cancel" || scenario == "second_cancel" && reads == 2 {
					cancel()
				}
				switch scenario {
				case "boot":
					id.BootID = "00000000-0000-0000-0000-000000000000"
				case "device":
					id.Device = 0
				case "inode":
					id.Inode = 0
				case "changed":
					if reads == 2 {
						id.Inode++
					}
				}
				return id, nil
			}
			if scenario == "nil_inspect" {
				inspect = nil
			}
			n, err := newExitLANBootNamespace(ctx, inspect, func() error {
				closes++
				if scenario == "close_error" {
					return closeErr
				}
				return nil
			})
			if scenario == "stable" {
				if err != nil || n == nil || reads != 2 || closes != 0 || n.identity != exitLANNamespaceTestIdentity() {
					t.Fatal("stable capture failed", err)
				}
				if err := n.Close(); err != nil {
					t.Fatal(err)
				}
				if err := n.Close(); err != nil {
					t.Fatal(err)
				}
				if closes != 1 {
					t.Fatal("handle close not exactly once")
				}
				return
			}
			if err == nil || n != nil || closes != 1 {
				t.Fatal("failed capture leaked or returned ownership", err, closes)
			}
			if (scenario == "pre_cancel" || scenario == "first_cancel" || scenario == "second_cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("lost constructor cancellation", err)
			}
			if (scenario == "first_error" || scenario == "second_error" || scenario == "close_error") && !errors.Is(err, readErr) {
				t.Fatal("lost inspect error", err)
			}
			if scenario == "close_error" && !errors.Is(err, closeErr) {
				t.Fatal("lost failure cleanup error", err)
			}
			if (scenario == "nil_inspect" || scenario == "pre_cancel") && reads != 0 {
				t.Fatal("invalid setup inspected namespace")
			}
		})
	}
	reads := 0
	if n, err := newExitLANBootNamespace(t.Context(), func(context.Context) (exitLANNamespaceIdentity, error) {
		reads++
		return exitLANNamespaceTestIdentity(), nil
	}, nil); err == nil || n != nil || reads != 0 {
		t.Fatal("missing handle owner reached capture")
	}
}

func TestExitLANNamespaceCurrentChecksAndLatchesInvalidity(t *testing.T) {
	readErr, callbackErr := errors.New("synthetic observation"), errors.New("synthetic callback")
	for _, scenario := range []string{"stable", "callback_error", "pre_error", "post_error", "pre_change", "post_change", "invalid_boot", "callback_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			reads, closes, callbacks := 0, 0, 0
			active := false
			n, err := newExitLANBootNamespace(t.Context(), func(context.Context) (exitLANNamespaceIdentity, error) {
				reads++
				id := exitLANNamespaceTestIdentity()
				if active {
					post := callbacks != 0
					if scenario == "pre_error" || scenario == "post_error" && post {
						return id, readErr
					}
					if scenario == "pre_change" || scenario == "post_change" && post {
						id.Inode++
					}
					if scenario == "invalid_boot" {
						id.BootID = "invalid"
					}
				}
				return id, nil
			}, func() error { closes++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			active = true
			ctx, cancel := context.WithCancel(t.Context())
			err = n.withCurrent(ctx, func(_ context.Context, id exitLANNamespaceIdentity) error {
				callbacks++
				if id != exitLANNamespaceTestIdentity() {
					t.Fatal("callback received substituted identity")
				}
				if n.mu.TryLock() {
					n.mu.Unlock()
					t.Fatal("callback ran outside namespace lock")
				}
				if scenario == "callback_cancel" {
					cancel()
				}
				if scenario == "callback_error" {
					return callbackErr
				}
				return nil
			})
			cancel()
			valid := scenario == "stable" || scenario == "callback_error"
			if scenario == "stable" && err != nil || scenario != "stable" && err == nil {
				t.Fatal("unexpected current result", err)
			}
			if scenario == "callback_error" && !errors.Is(err, callbackErr) {
				t.Fatal("callback error lost", err)
			}
			if scenario == "callback_cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("late cancellation lost", err)
			}
			preFailure := scenario == "pre_error" || scenario == "pre_change" || scenario == "invalid_boot"
			wantReads := 4
			if preFailure {
				wantReads = 3
			}
			if reads != wantReads || preFailure && callbacks != 0 || !preFailure && callbacks != 1 || n.invalid == valid {
				t.Fatal("pre/post observation or invalid latch missing")
			}
			active = false
			before := reads
			retried := false
			err = n.withCurrent(t.Context(), func(context.Context, exitLANNamespaceIdentity) error { retried = true; return nil })
			if valid {
				if err != nil || !retried || reads != before+2 {
					t.Fatal("callback-only failure invalidated stable namespace")
				}
			} else if err == nil || retried || reads != before {
				t.Fatal("invalid namespace revived")
			}
			if err := n.Close(); err != nil || closes != 1 {
				t.Fatal("current failure leaked handle", err)
			}
		})
	}
}

func TestExitLANNamespaceCloseSerializesCallback(t *testing.T) {
	closes := 0
	n, err := newExitLANBootNamespace(t.Context(), func(context.Context) (exitLANNamespaceIdentity, error) { return exitLANNamespaceTestIdentity(), nil }, func() error { closes++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	entered, release, closeStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	done, closed := make(chan error, 1), make(chan error, 1)
	go func() {
		done <- n.withCurrent(t.Context(), func(context.Context, exitLANNamespaceIdentity) error { close(entered); <-release; return nil })
	}()
	<-entered
	go func() { close(closeStarted); closed <- n.Close() }()
	<-closeStarted
	lockFree := n.mu.TryLock()
	if lockFree {
		n.mu.Unlock()
	}
	select {
	case <-closed:
		t.Fatal("Close crossed active callback")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	if currentErr, closeErr := <-done, <-closed; currentErr != nil || closeErr != nil || lockFree {
		t.Fatal("callback/Close serialization failed", currentErr, closeErr)
	}
	if err := n.Close(); err != nil || closes != 1 {
		t.Fatal("Close repeated handle release", err)
	}
	called := false
	if err := n.withCurrent(t.Context(), func(context.Context, exitLANNamespaceIdentity) error { called = true; return nil }); err == nil || called {
		t.Fatal("closed namespace admitted callback")
	}
}

func TestExitLANNamespaceOwnershipBindingBeforeCleanup(t *testing.T) {
	for _, scenario := range []string{"matching", "boot", "device", "inode", "invalid", "nil_callback"} {
		t.Run(scenario, func(t *testing.T) {
			reads := 0
			n, err := newExitLANBootNamespace(t.Context(), func(context.Context) (exitLANNamespaceIdentity, error) {
				reads++
				return exitLANNamespaceTestIdentity(), nil
			}, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := n.Close(); err != nil {
					t.Error(err)
				}
			}()
			owned := exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
			switch scenario {
			case "boot":
				owned.BootID = "22345678-9abc-4def-8123-456789abcdef"
			case "device":
				owned.NamespaceDevice++
			case "inode":
				owned.NamespaceInode++
			case "invalid":
				owned.Scope = "../foreign"
			}
			called := false
			fn := func(context.Context) error { called = true; return nil }
			if scenario == "nil_callback" {
				fn = nil
			}
			before := reads
			err = n.withOwnership(t.Context(), owned, fn)
			if scenario == "matching" {
				if err != nil || !called || reads != before+2 {
					t.Fatal("matching namespace not reobserved", err)
				}
			} else if err == nil || called {
				t.Fatal("foreign or invalid manifest reached cleanup")
			}
			if (scenario == "invalid" || scenario == "nil_callback") && reads != before {
				t.Fatal("invalid input reached namespace inspection")
			}
			// Rejecting another manifest must not damage this held namespace.
			if n.invalid {
				t.Fatal("foreign ownership invalidated unchanged namespace")
			}
		})
	}
}
