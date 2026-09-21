package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

var errUnderlayDNSLeaseRevoked = errors.New("underlay DNS source lease is no longer current")

const underlayDNSLeaseConnections = 256

// One lease owns one immutable source. Its monitor never takes engine locks;
// revocation is permanent, and a new source requires a new lease.
type underlayDNSLease struct {
	ctx         context.Context
	cancel      context.CancelCauseFunc
	current     func(context.Context) error
	permit      chan struct{}
	done        chan struct{}
	mu          sync.Mutex
	connections map[*underlayDNSConnection]struct{}
	revokeOnce  sync.Once
	startOnce   sync.Once
	ticks       func() (<-chan time.Time, func())
}

func newUnderlayDNSLease(current func(context.Context) error) *underlayDNSLease {
	lease := newUnderlayDNSLeaseWithTicks(current, nil, nil)
	lease.ticks = func() (<-chan time.Time, func()) { ticker := time.NewTicker(time.Second); return ticker.C, ticker.Stop }
	return lease
}

func newUnderlayDNSLeaseWithTicks(current func(context.Context) error, ticks <-chan time.Time, stop func()) *underlayDNSLease {
	ctx, cancel := context.WithCancelCause(context.Background())
	lease := &underlayDNSLease{ctx: ctx, cancel: cancel, current: current, permit: make(chan struct{}, 1), done: make(chan struct{}), connections: make(map[*underlayDNSConnection]struct{})}
	lease.ticks = func() (<-chan time.Time, func()) { return ticks, stop }
	if current == nil {
		lease.revoke()
	}
	return lease
}

// Start is lazy and idempotent: captures and constructors do not spawn workers.
func (l *underlayDNSLease) Start() {
	l.startOnce.Do(func() {
		if l.ctx.Err() != nil {
			close(l.done)
			return
		}
		ticks, stop := l.ticks()
		go func() {
			defer close(l.done)
			if stop != nil {
				defer stop()
			}
			for {
				select {
				case <-l.ctx.Done():
					return
				case _, ok := <-ticks:
					if !ok {
						l.revoke()
						return
					}
					if l.Current(l.ctx) != nil {
						return
					}
				}
			}
		}()
	})
}

func (l *underlayDNSLease) Context() context.Context { return l.ctx }

// Explicit checks and monitor checks are serialized. Cancelling one request
// does not revoke the shared lease; an independently timed-out observation does.
func (l *underlayDNSLease) Current(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.ctx.Done():
		return context.Cause(l.ctx)
	case l.permit <- struct{}{}:
	}
	defer func() { <-l.permit }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	stop := context.AfterFunc(l.ctx, cancel)
	err := l.current(check)
	if check.Err() != nil {
		err = check.Err()
	}
	stop()
	cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if cause := context.Cause(l.ctx); cause != nil {
		return cause
	}
	if err != nil {
		l.revoke()
		return context.Cause(l.ctx)
	}
	return nil
}

// Wrap transfers connection ownership even on failure. Registration and
// revocation share the lock, so late successful dials cannot escape revocation.
func (l *underlayDNSLease) Wrap(conn net.Conn) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("underlay connection is required")
	}
	l.mu.Lock()
	err := context.Cause(l.ctx)
	if err == nil && len(l.connections) >= underlayDNSLeaseConnections {
		err = errors.New("underlay connection limit exceeded")
	}
	if err != nil {
		l.mu.Unlock()
		_ = conn.Close()
		return nil, err
	}
	wrapped := &underlayDNSConnection{Conn: conn, lease: l}
	l.connections[wrapped] = struct{}{}
	l.mu.Unlock()
	l.Start()
	return wrapped, nil
}

func (l *underlayDNSLease) revoke() {
	l.revokeOnce.Do(func() {
		l.mu.Lock()
		l.cancel(errUnderlayDNSLeaseRevoked)
		connections := make([]*underlayDNSConnection, 0, len(l.connections))
		for conn := range l.connections {
			connections = append(connections, conn)
		}
		l.mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
}

// Close may be called under an engine lock. The monitor only uses its captured
// observation callback and must never acquire that engine lock.
func (l *underlayDNSLease) Close() { l.revoke(); l.startOnce.Do(func() { close(l.done) }); <-l.done }

type underlayDNSConnection struct {
	net.Conn
	lease    *underlayDNSLease
	once     sync.Once
	closeErr error
}

func (c *underlayDNSConnection) Read(p []byte) (int, error) {
	if err := context.Cause(c.lease.ctx); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(p)
	if cause := context.Cause(c.lease.ctx); cause != nil {
		return 0, cause
	}
	return n, err
}

func (c *underlayDNSConnection) Write(p []byte) (int, error) {
	if err := context.Cause(c.lease.ctx); err != nil {
		return 0, err
	}
	n, err := c.Conn.Write(p)
	if cause := context.Cause(c.lease.ctx); cause != nil {
		return 0, cause
	}
	return n, err
}

func (c *underlayDNSConnection) Close() error {
	c.once.Do(func() {
		c.closeErr = c.Conn.Close()
		c.lease.mu.Lock()
		delete(c.lease.connections, c)
		c.lease.mu.Unlock()
	})
	return c.closeErr
}
