package client

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnderlayDNSLeaseLazyStartAndStickyFailure(t *testing.T) {
	ticks := make(chan time.Time, 1)
	var calls, starts, stops atomic.Int32
	lease := newUnderlayDNSLeaseWithTicks(func(context.Context) error {
		calls.Add(1)
		return errors.New("source changed")
	}, ticks, func() { stops.Add(1) })
	lease.ticks = func() (<-chan time.Time, func()) {
		starts.Add(1)
		return ticks, func() { stops.Add(1) }
	}
	t.Cleanup(lease.Close)
	if lease.Context().Err() != nil || starts.Load() != 0 || calls.Load() != 0 {
		t.Fatal("constructor or Context started observation")
	}
	lease.Start()
	lease.Start()
	ticks <- time.Now()
	awaitUnderlayLeaseSignal(t, lease.Context().Done())
	lease.Close()
	lease.Close()
	if starts.Load() != 1 || stops.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("starts=%d stops=%d observations=%d", starts.Load(), stops.Load(), calls.Load())
	}
	if err := lease.Current(context.Background()); !errors.Is(err, errUnderlayDNSLeaseRevoked) || calls.Load() != 1 {
		t.Fatalf("revocation was not sticky: %v, observations=%d", err, calls.Load())
	}
}

func TestUnderlayDNSLeaseCallerCancellationDoesNotRevoke(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	lease := newUnderlayDNSLeaseWithTicks(func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, nil, nil)
	t.Cleanup(lease.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	first := make(chan error, 1)
	go func() { first <- lease.Current(ctx) }()
	awaitUnderlayLeaseSignal(t, entered)
	waitCtx, waitCancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- lease.Current(waitCtx) }()
	waitCancel()
	if err := awaitUnderlayLeaseError(t, waiting); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting request cancellation: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent observer entered while another holds the permit")
	}
	cancel()
	if err := awaitUnderlayLeaseError(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("active request cancellation: %v", err)
	}
	if lease.Context().Err() != nil {
		t.Fatal("request cancellation revoked shared source")
	}
	if err := lease.Current(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("subsequent observation: %v, calls=%d", err, calls.Load())
	}
}

func TestUnderlayDNSLeaseCloseJoinsActiveMonitor(t *testing.T) {
	ticks := make(chan time.Time, 1)
	entered, exited := make(chan struct{}), make(chan struct{})
	lease := newUnderlayDNSLeaseWithTicks(func(ctx context.Context) error {
		defer close(exited)
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}, ticks, nil)
	t.Cleanup(lease.Close)
	lease.Start()
	ticks <- time.Now()
	awaitUnderlayLeaseSignal(t, entered)
	closed := make(chan struct{})
	go func() { lease.Close(); close(closed) }()
	awaitUnderlayLeaseSignal(t, closed)
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before the observer stopped")
	}
}

func TestUnderlayDNSLeaseRevocationClosesBlockedConnection(t *testing.T) {
	lease := newUnderlayDNSLeaseWithTicks(func(context.Context) error { return errors.New("source changed") }, nil, nil)
	t.Cleanup(lease.Close)
	left, right := net.Pipe()
	t.Cleanup(func() { _ = right.Close() })
	conn, err := lease.Wrap(left)
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan error, 1)
	go func() {
		n, readErr := conn.Read(make([]byte, 1))
		if n != 0 {
			read <- errors.New("revoked read returned data")
			return
		}
		read <- readErr
	}()
	if err := lease.Current(context.Background()); !errors.Is(err, errUnderlayDNSLeaseRevoked) {
		t.Fatalf("failed observation: %v", err)
	}
	if err := awaitUnderlayLeaseError(t, read); !errors.Is(err, errUnderlayDNSLeaseRevoked) {
		t.Fatalf("blocked read: %v", err)
	}
	if n, err := conn.Write([]byte("x")); n != 0 || !errors.Is(err, errUnderlayDNSLeaseRevoked) {
		t.Fatalf("revoked write: %d, %v", n, err)
	}
}

func TestUnderlayDNSLeaseDropsReadCompletedAfterRevocation(t *testing.T) {
	lease := newUnderlayDNSLeaseWithTicks(func(context.Context) error { return nil }, nil, nil)
	t.Cleanup(lease.Close)
	raw := &underlayLeaseTestConnection{}
	raw.read = func(p []byte) (int, error) {
		copy(p, "private")
		lease.revoke()
		return len("private"), nil
	}
	conn, err := lease.Wrap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := conn.Read(make([]byte, 16)); n != 0 || !errors.Is(err, errUnderlayDNSLeaseRevoked) {
		t.Fatalf("read completed after revocation: %d, %v", n, err)
	}
	if raw.closes.Load() != 1 {
		t.Fatal("revocation did not close connection")
	}
}

func TestUnderlayDNSLeaseConnectionLimitAndLateRegistration(t *testing.T) {
	lease := newUnderlayDNSLeaseWithTicks(func(context.Context) error { return nil }, nil, nil)
	t.Cleanup(lease.Close)
	var first net.Conn
	for i := 0; i < underlayDNSLeaseConnections; i++ {
		conn, err := lease.Wrap(&underlayLeaseTestConnection{})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = conn
		}
	}
	rejected := &underlayLeaseTestConnection{}
	if conn, err := lease.Wrap(rejected); err == nil || conn != nil || rejected.closes.Load() != 1 {
		t.Fatal("connection limit did not reject and close new connection")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := &underlayLeaseTestConnection{}
	if _, err := lease.Wrap(replacement); err != nil {
		t.Fatal("closed connection retained registry slot:", err)
	}
	lease.Close()
	if replacement.closes.Load() != 1 {
		t.Fatal("Close did not revoke live connection")
	}
	late := &underlayLeaseTestConnection{}
	if conn, err := lease.Wrap(late); !errors.Is(err, errUnderlayDNSLeaseRevoked) || conn != nil || late.closes.Load() != 1 {
		t.Fatal("late connection escaped revocation")
	}
}

type underlayLeaseTestConnection struct {
	net.Conn
	closes atomic.Int32
	read   func([]byte) (int, error)
}

func (c *underlayLeaseTestConnection) Read(p []byte) (int, error) { return c.read(p) }
func (c *underlayLeaseTestConnection) Close() error               { c.closes.Add(1); return nil }

func awaitUnderlayLeaseSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lease signal")
	}
}

func awaitUnderlayLeaseError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lease result")
		return nil
	}
}
