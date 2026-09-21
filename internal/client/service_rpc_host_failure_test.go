package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

type rpcHostFailureAfterDriver struct {
	rpcHostBlockingListener
	started <-chan struct{}
	failure error
}

func (l *rpcHostFailureAfterDriver) Accept() (net.Conn, error) {
	select {
	case <-l.started:
		return nil, l.failure
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func TestRPCHostReportsFailureBeforeWaitingForRuntimeOwner(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unlock()
	want := errors.New("listener failed while runtime owner is active")
	listener := &rpcHostFailureAfterDriver{rpcHostBlockingListener: rpcHostBlockingListener{closed: make(chan struct{})}, started: started, failure: want}
	s := NewClientRPCService(m, nil)
	reported := make(chan error, 1)
	s.RuntimeFailure = func(err error) {
		reported <- err
		unlock()
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		close(started)
		// The host cannot finish joining until the external runtime owner is
		// notified. Cancellation of the worker alone does not release this gate.
		<-release
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		done <- s.Serve(ctx, listener, driver, func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error) {
			return nil, nil
		})
	}()
	select {
	case err := <-reported:
		if !errors.Is(err, want) {
			t.Fatal("failure callback lost original cause", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host waited for worker before notifying runtime owner")
	}
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatal("host return lost failure", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host did not drain workers")
	}
}
