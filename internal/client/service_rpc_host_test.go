package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

type rpcHostTestListener struct {
	closed bool
	err    error
}

func (l *rpcHostTestListener) Accept() (net.Conn, error) { return nil, l.err }
func (l *rpcHostTestListener) Close() error              { l.closed = true; return nil }
func (*rpcHostTestListener) Addr() net.Addr              { return &net.UnixAddr{Name: "test", Net: "unix"} }

type rpcHostBlockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func (l *rpcHostBlockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}
func (l *rpcHostBlockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
func (*rpcHostBlockingListener) Addr() net.Addr { return &net.UnixAddr{Name: "test", Net: "unix"} }

func TestRPCHostDrainsWorkersAfterListenerFailure(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	failure := errors.New("synthetic listener failure")
	l := &rpcHostTestListener{err: failure}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{},
		Start: func(context.Context, Config) error { return nil },
		Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
		},
	}
	provider := func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error) {
		return nil, nil
	}
	if err := s.Serve(t.Context(), l, driver, provider); !errors.Is(err, failure) {
		t.Fatal("listener failure lost", err)
	}
	if !l.closed || s.profileWorker != nil || s.enrollmentWorker != nil {
		t.Fatal("host returned before draining workers and closing listener")
	}
	// Enrollment startup failure must also drain the already-started profile worker.
	l = &rpcHostTestListener{err: failure}
	if err := s.Serve(t.Context(), l, driver, nil); err == nil {
		t.Fatal("missing provider accepted")
	}
	if !l.closed || s.profileWorker != nil || s.enrollmentWorker != nil {
		t.Fatal("partial startup leaked listener or worker")
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState = &ClientRPCState{Enrollment: &clientRPCEnrollment{OperationID: "missing-operation"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	blocking := &rpcHostBlockingListener{closed: make(chan struct{})}
	if err := s.Serve(t.Context(), blocking, driver, provider); err == nil || errors.Is(err, net.ErrClosed) {
		t.Fatal("worker failure not propagated", err)
	}
	if s.profileWorker != nil || s.enrollmentWorker != nil {
		t.Fatal("worker failure did not drain sibling")
	}
	select {
	case <-blocking.closed:
	default:
		t.Fatal("worker failure left listener open")
	}
}
