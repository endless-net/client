package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type underlayTestConnection struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func newUnderlayTestConnection() *underlayTestConnection {
	return &underlayTestConnection{closed: make(chan struct{})}
}
func (c *underlayTestConnection) Close() error { c.once.Do(func() { close(c.closed) }); return nil }

func TestUnderlayConnectInterleavesFamiliesAndClosesLateSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addresses := []netip.Addr{netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2"), netip.MustParseAddr("192.0.2.1")}
	late, winner := newUnderlayTestConnection(), newUnderlayTestConnection()
	defer func() { _ = winner.Close() }()
	var attempts atomic.Int32
	started := time.Now()
	conn, err := dialUnderlayAddresses(ctx, "tcp", "443", addresses, func(ctx context.Context, _, address string) (net.Conn, error) {
		attempts.Add(1)
		if address == "[2001:db8::1]:443" {
			<-ctx.Done()
			return late, nil
		}
		if address == "192.0.2.1:443" {
			return winner, nil
		}
		return nil, errors.New("second IPv6 must not precede IPv4")
	}, nil)
	if err != nil || conn != winner {
		t.Fatalf("connect = %v, %v", conn, err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
	if time.Since(started) < 200*time.Millisecond {
		t.Fatal("second attempt was not staggered")
	}
	select {
	case <-late.closed:
	case <-time.After(time.Second):
		t.Fatal("late successful loser was not closed")
	}
}

func TestUnderlayConnectBoundsConcurrentAttemptsAndCandidateDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	addresses := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.3")}
	winner := newUnderlayTestConnection()
	defer func() { _ = winner.Close() }()
	var active, maximum atomic.Int32
	conn, err := dialUnderlayAddresses(ctx, "udp", "443", addresses, func(ctx context.Context, _, address string) (net.Conn, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); count > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, count) {
				break
			}
		}
		if address == "192.0.2.3:443" {
			return winner, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil)
	if err != nil || conn != winner {
		t.Fatalf("connect = %v, %v", conn, err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d", maximum.Load())
	}
}

func TestUnderlayConnectCancellationClosesLateSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	late := newUnderlayTestConnection()
	done := make(chan error, 1)
	go func() {
		_, err := dialUnderlayAddresses(ctx, "tcp", "80", []netip.Addr{netip.MustParseAddr("192.0.2.1")}, func(ctx context.Context, _, _ string) (net.Conn, error) {
			close(started)
			<-ctx.Done()
			return late, nil
		}, nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("dial did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not return")
	}
	select {
	case <-late.closed:
	case <-time.After(time.Second):
		t.Fatal("late socket leaked")
	}
}

func TestUnderlayConnectRejectsStaleSourceAndClosesWinner(t *testing.T) {
	stale := errors.New("source changed")
	conn := newUnderlayTestConnection()
	var checks int
	got, err := dialUnderlayAddresses(context.Background(), "tcp", "443", []netip.Addr{netip.MustParseAddr("192.0.2.1")}, func(context.Context, string, string) (net.Conn, error) { return conn, nil }, func(context.Context) error {
		checks++
		if checks == 2 {
			return stale
		}
		return nil
	})
	if got != nil || !errors.Is(err, stale) {
		t.Fatalf("connect = %v, %v", got, err)
	}
	select {
	case <-conn.closed:
	default:
		t.Fatal("stale socket leaked")
	}
	var called bool
	_, err = dialUnderlayAddresses(context.Background(), "tcp", "443", []netip.Addr{netip.MustParseAddr("192.0.2.1")}, func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil }, func(context.Context) error { return stale })
	if called || !errors.Is(err, stale) {
		t.Fatalf("stale source dialed: %v, %v", called, err)
	}
}

func TestUnderlayConnectPreservesFailureIdentityAndRedactsEndpoints(t *testing.T) {
	markError := errors.New("mark failed with private endpoint detail")
	_, err := dialUnderlayAddresses(context.Background(), "tcp4", "443", []netip.Addr{netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("192.0.2.1")}, func(_ context.Context, _, address string) (net.Conn, error) {
		if address != "192.0.2.1:443" {
			return nil, errors.New("family filter failed")
		}
		return nil, markError
	}, nil)
	if !errors.Is(err, markError) || strings.Contains(err.Error(), "private") {
		t.Fatalf("error identity/redaction = %v", err)
	}
}

func TestUnderlayConnectRejectsUnboundedCandidates(t *testing.T) {
	var called bool
	_, err := dialUnderlayAddresses(context.Background(), "tcp", "443", make([]netip.Addr, 257), func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil }, nil)
	if err == nil || called {
		t.Fatalf("unbounded candidates dialed: %v, %v", called, err)
	}
}
