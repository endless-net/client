//go:build windows

package clientapp

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"go.uber.org/fx"
	"google.golang.org/grpc"
)

var ErrIPC = errors.New("client IPC lifecycle failed")

// IPC owns the local listener, not the controller or protected state. Register
// this hook before Workers and supply Run as its mandatory IPC loop.
type IPC struct {
	config           localipc.PipeConfig
	allowed          []string
	server           *grpc.Server
	drain            time.Duration
	maxConnections   int
	mu               sync.Mutex
	listener         *ipcListener
	started, stopped bool
	served, closed   chan struct{}
	closeOnce        sync.Once
	forceOnce        sync.Once
	timedOut         chan struct{}
	closeErr         error
}

// NewIPC validates the transport without opening a pipe. Explicit allowed SIDs
// and the service SID come from installation configuration, never RPC metadata.
func NewIPC(lifecycle fx.Lifecycle, signals *Signals, config localipc.PipeConfig, allowed []string, owner localipc.OwnerReader, service clientv0.ClientServiceServer, maxMessageBytes, maxConnections int, maxConcurrent uint32, drain time.Duration) (*IPC, error) {
	if lifecycle == nil || !signals.valid() || len(allowed) == 0 || maxConnections <= 0 || maxConcurrent == 0 || drain <= 0 || drain > 30*time.Second {
		return nil, ErrIPC
	}
	server, err := localipc.NewServer(config, owner, service, maxMessageBytes, maxConcurrent, localipc.Observation{Unary: signals.Unary, Stream: signals.Stream})
	if err != nil {
		return nil, ErrIPC
	}
	i := &IPC{config: config, allowed: append([]string(nil), allowed...), server: server, drain: drain, maxConnections: maxConnections, served: make(chan struct{}), closed: make(chan struct{}), timedOut: make(chan struct{})}
	lifecycle.Append(signals.hook("ipc", i.start, i.stop))
	return i, nil
}

func (i *IPC) start(ctx context.Context) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil {
		return ErrIPC
	}
	i.mu.Lock()
	if i.started || i.stopped {
		i.mu.Unlock()
		return ErrIPC
	}
	listener, err := localipc.Listen(i.config, i.allowed)
	if err != nil {
		i.mu.Unlock()
		return ErrIPC
	}
	i.listener = &ipcListener{Listener: listener, connections: make(map[*ipcConnection]struct{}), slots: make(chan struct{}, i.maxConnections), done: make(chan struct{})}
	i.started = true
	i.mu.Unlock()
	go func() { _ = i.server.Serve(i.listener); close(i.served) }()
	connection, err := localipc.Connect(ctx, i.config)
	if err == nil {
		err = connection.Close()
	}
	if err == nil && ctx.Err() == nil {
		select {
		case <-i.served:
		default:
			return nil
		}
	}
	// Fx does not call OnStop for a hook whose OnStart failed. Close resources
	// here as well; cleanup has its own explicit bounded budget.
	cleanup, cancel := context.WithTimeout(context.Background(), i.drain)
	defer cancel()
	_ = i.stop(cleanup)
	return ErrIPC
}

// Run must be supervised even though OnStart already served the bootstrap RPC.
// Unexpected Serve return is always failure, including a nil return.
func (i *IPC) Run(ctx context.Context) error {
	i.mu.Lock()
	started, stopped := i.started, i.stopped
	i.mu.Unlock()
	if !started || stopped {
		return ErrIPC
	}
	select {
	case <-i.served:
		return ErrIPC
	case <-ctx.Done():
		cleanup, cancel := context.WithTimeout(context.Background(), i.drain)
		defer cancel()
		if err := i.stop(cleanup); err != nil {
			return err
		}
		return ctx.Err()
	}
}

func (i *IPC) stop(ctx context.Context) error {
	i.mu.Lock()
	i.stopped = true
	started := i.started
	i.mu.Unlock()
	if !started {
		return nil
	}
	i.closeOnce.Do(func() {
		_ = i.listener.Close() // Stop admission before waiting for any RPC.
		go func() {
			// Do not call Stop synchronously: gRPC may hold its mutex while
			// GracefulStop waits for a handler that ignores cancellation.
			graceful := make(chan struct{})
			go func() { i.server.GracefulStop(); close(graceful) }()
			timer := time.NewTimer(i.drain)
			defer timer.Stop()
			select {
			case <-graceful:
			case <-timer.C:
				i.force()
				<-graceful
			}
			<-i.served
			close(i.closed)
		}()
	})
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrIPC
	}
	select {
	case <-ctx.Done():
		i.force()
		return ErrIPC
	case <-i.timedOut:
		return ErrIPC
	case <-i.closed:
		if ctx.Err() != nil {
			return ErrIPC
		}
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.closeErr
	}
}

func (i *IPC) force() {
	i.forceOnce.Do(func() {
		i.mu.Lock()
		i.closeErr = ErrIPC
		i.mu.Unlock()
		i.listener.abort()
		close(i.timedOut)
		go i.server.Stop()
	})
}

// Track at most the explicit connection limit so timeout cleanup does not
// depend on a gRPC mutex or on the cooperation of a domain handler.
type ipcListener struct {
	net.Listener
	mu          sync.Mutex
	connections map[*ipcConnection]struct{}
	closing     bool
	slots       chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

func (l *ipcListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	if _, ok := connection.(interface{ Fd() uintptr }); !ok {
		_ = connection.Close()
		<-l.slots
		return nil, ErrIPC
	}
	c := &ipcConnection{Conn: connection, listener: l}
	l.mu.Lock()
	if l.closing {
		l.mu.Unlock()
		_ = connection.Close()
		<-l.slots
		return nil, net.ErrClosed
	}
	l.connections[c] = struct{}{}
	l.mu.Unlock()
	return c, nil
}

func (l *ipcListener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closing = true
		l.mu.Unlock()
		close(l.done)
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

func (l *ipcListener) abort() {
	_ = l.Close()
	l.mu.Lock()
	connections := make([]*ipcConnection, 0, len(l.connections))
	for c := range l.connections {
		connections = append(connections, c)
	}
	l.mu.Unlock()
	for _, c := range connections {
		_ = c.Close()
	}
}

type ipcConnection struct {
	net.Conn
	listener *ipcListener
	once     sync.Once
	err      error
}

func (c *ipcConnection) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.listener.mu.Lock()
		delete(c.listener.connections, c)
		c.listener.mu.Unlock()
		<-c.listener.slots
	})
	return c.err
}

// The Windows credentials must inspect the original pipe handle. A generic
// net.Conn wrapper that drops Fd would disable OS peer authentication entirely.
func (c *ipcConnection) Fd() uintptr { return c.Conn.(interface{ Fd() uintptr }).Fd() }
