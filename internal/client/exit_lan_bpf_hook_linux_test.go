//go:build linux

package client

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type exitLANHookTestTransport struct {
	port                    uint32
	closes, sends, receives int
	send                    func(context.Context, []byte) error
	receive                 func(context.Context) ([]byte, bool, bool, error)
	closeErr                error
}

func (f *exitLANHookTestTransport) Port() uint32 { return f.port }
func (f *exitLANHookTestTransport) Close() error { f.closes++; return f.closeErr }
func (f *exitLANHookTestTransport) Send(ctx context.Context, raw []byte) error {
	f.sends++
	if f.send != nil {
		return f.send(ctx, raw)
	}
	return nil
}
func (f *exitLANHookTestTransport) Receive(ctx context.Context) ([]byte, bool, bool, error) {
	f.receives++
	return f.receive(ctx)
}

func TestExitLANHookTransportConfirmsFreshDump(t *testing.T) {
	identity := exitLANBPFLinkIdentity{17, 19, 2, 4, -100}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		f := &exitLANHookTestTransport{port: 42}
		f.send = func(ctx context.Context, raw []byte) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 2*time.Second || time.Until(deadline) <= 0 {
				t.Fatal("dump not bounded")
			}
			expected, err := newExitLANHookDump(order, 1, 42, identity)
			if err != nil || string(raw) != string(expected.request()) {
				t.Fatal("wrong request/sequence/port")
			}
			return nil
		}
		f.receive = func(context.Context) ([]byte, bool, bool, error) {
			if f.receives == 1 {
				return exitLANHookTestRecord(order, 1, identity), true, false, nil
			}
			return exitLANHookTestDone(order, 1, 42), true, false, nil
		}
		err := observeExitLANHookWith(t.Context(), identity, order, func(context.Context) (exitLANHookTransport, error) { return f, nil })
		if err != nil || f.sends != 1 || f.receives != 2 || f.closes != 1 {
			t.Fatalf("dump lifecycle: send=%d recv=%d close=%d err=%v", f.sends, f.receives, f.closes, err)
		}
	}
}

func TestExitLANHookTransportRejectsIncompleteOrLostEvidence(t *testing.T) {
	identity := exitLANBPFLinkIdentity{17, 19, 10, 4, -100}
	order := binary.LittleEndian
	for _, scenario := range []string{"send", "loss", "foreign_sender", "truncated", "missing_match", "missing_done", "empty", "oversized", "close", "bounded_datagrams"} {
		t.Run(scenario, func(t *testing.T) {
			f := &exitLANHookTestTransport{port: 42}
			if scenario == "send" {
				f.send = func(context.Context, []byte) error { return unix.EIO }
			}
			if scenario == "close" {
				f.closeErr = unix.EIO
			}
			f.receive = func(context.Context) ([]byte, bool, bool, error) {
				switch scenario {
				case "loss":
					return nil, false, false, unix.ENOBUFS
				case "foreign_sender":
					return exitLANHookTestRecord(order, 1, identity), false, false, nil
				case "truncated":
					return exitLANHookTestRecord(order, 1, identity), true, true, nil
				case "missing_match":
					return exitLANHookTestDone(order, 1, 42), true, false, nil
				case "empty":
					return nil, true, false, nil
				case "oversized":
					return make([]byte, (64<<10)+1), true, false, nil
				case "bounded_datagrams":
					other := identity
					other.id++
					other.programID++
					return exitLANHookTestRecord(order, 1, other), true, false, nil
				}
				if f.receives == 1 {
					return exitLANHookTestRecord(order, 1, identity), true, false, nil
				}
				if scenario == "missing_done" {
					return nil, false, false, unix.EIO
				}
				return exitLANHookTestDone(order, 1, 42), true, false, nil
			}
			err := observeExitLANHookWith(t.Context(), identity, order, func(context.Context) (exitLANHookTransport, error) { return f, nil })
			if err == nil || f.closes != 1 || f.receives > 128 {
				t.Fatal("uncertain dump accepted or transport leaked", err, f.closes, f.receives)
			}
			if scenario == "bounded_datagrams" && f.receives != 128 {
				t.Fatal("test did not reach transport work cap", f.receives)
			}
		})
	}
}

func TestExitLANHookTransportCancellationCloses(t *testing.T) {
	identity := exitLANBPFLinkIdentity{17, 19, 2, 4, -100}
	for _, stage := range []string{"before_open", "open", "send", "receive", "done"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stage == "before_open" {
				cancel()
			}
			f := &exitLANHookTestTransport{port: 42}
			f.send = func(context.Context, []byte) error {
				if stage == "send" {
					cancel()
				}
				return nil
			}
			f.receive = func(context.Context) ([]byte, bool, bool, error) {
				if stage == "receive" || stage == "done" && f.receives == 2 {
					cancel()
				}
				if f.receives == 1 {
					return exitLANHookTestRecord(binary.LittleEndian, 1, identity), true, false, nil
				}
				return exitLANHookTestDone(binary.LittleEndian, 1, 42), true, false, nil
			}
			opened := 0
			err := observeExitLANHookWith(ctx, identity, binary.LittleEndian, func(context.Context) (exitLANHookTransport, error) {
				opened++
				if stage == "open" {
					cancel()
				}
				return f, nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if stage == "before_open" {
				if opened != 0 || f.closes != 0 {
					t.Fatal("pre-cancel opened transport")
				}
			} else if opened != 1 || f.closes != 1 {
				t.Fatal("cancelled transport not closed")
			}
		})
	}
}

func TestExitLANHookTransportCancelsBlockedReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &exitLANHookTestTransport{port: 42}
	entered := make(chan struct{})
	f.receive = func(ctx context.Context) ([]byte, bool, bool, error) {
		close(entered)
		<-ctx.Done()
		return nil, false, false, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		done <- observeExitLANHookWith(ctx, exitLANBPFLinkIdentity{17, 19, 2, 4, 1}, binary.LittleEndian, func(context.Context) (exitLANHookTransport, error) { return f, nil })
	}()
	select {
	case <-entered:
		cancel()
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("receive not entered")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || f.closes != 1 {
			t.Fatal("blocked cancellation lost cleanup", err)
		}
	case <-time.After(time.Second):
		<-done
		t.Fatal("receive failed to stop on cancellation")
	}
}

func TestNativeExitLANHookPrecancelDoesNotOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := newNativeExitLANHookObservation(ctx, exitLANBPFLinkIdentity{17, 19, 2, 4, 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestExitLANHookAuthenticatesSocketSender(t *testing.T) {
	for _, test := range []struct {
		sender            unix.Sockaddr
		flags             int
		kernel, truncated bool
	}{
		{&unix.SockaddrNetlink{Family: unix.AF_NETLINK}, 0, true, false},
		{&unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: 42}, 0, false, false},
		{&unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}, 0, false, false},
		{&unix.SockaddrNetlink{Family: unix.AF_INET}, 0, false, false},
		{&unix.SockaddrInet4{}, 0, false, false},
		{nil, 0, false, false},
		{(*unix.SockaddrNetlink)(nil), 0, false, false},
		{&unix.SockaddrNetlink{Family: unix.AF_NETLINK}, unix.MSG_TRUNC, true, true},
		{&unix.SockaddrNetlink{Family: unix.AF_NETLINK}, unix.MSG_CTRUNC, true, true},
	} {
		kernel, truncated := exitLANHookSender(test.sender, test.flags)
		if kernel != test.kernel || truncated != test.truncated {
			t.Fatal("socket sender/truncation misclassified")
		}
	}
}

func TestExitLANHookTransportSetupFailureClosesReturnedObject(t *testing.T) {
	f := &exitLANHookTestTransport{port: 42}
	err := observeExitLANHookWith(t.Context(), exitLANBPFLinkIdentity{17, 19, 2, 4, 1}, binary.LittleEndian, func(context.Context) (exitLANHookTransport, error) { return f, unix.EIO })
	if err == nil || f.closes != 1 || f.sends != 0 || f.receives != 0 {
		t.Fatal("partial setup escaped cleanup")
	}
}
