package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/tailscale/wireguard-go/conn"
)

type faultyPortBind struct {
	*inertBind
	mode          string
	opens, closes int
}

func (b *faultyPortBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.opens++
	fns, actual, err := b.inertBind.Open(port)
	if err != nil {
		return fns, actual, err
	}
	switch b.mode {
	case "different":
		actual++
	case "empty":
		fns = nil
	case "nil-receive":
		fns[0] = nil
	case "error":
		return fns, actual, errors.New("raw bind detail must not escape")
	}
	return fns, actual, nil
}
func (b *faultyPortBind) Close() error { b.closes++; return b.inertBind.Close() }

func TestEngineFixedListenPortIsAcknowledgedAndNotChangedByRefresh(t *testing.T) {
	m, private := engineMap(t)
	bind := &faultyPortBind{inertBind: &inertBind{}}
	e, err := NewEngine(newInertTUN(), bind, 4, 51821, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.BoundPort(); err != ErrEngine {
		t.Fatal("unopened socket called bound", err)
	}
	if err := e.apply(context.Background(), m, private); err != nil {
		t.Fatal(err)
	}
	if port, err := e.BoundPort(); err != nil || port != 51821 || bind.opens != 1 {
		t.Fatal("configured port not acknowledged", port, err)
	}
	m.Peers[0].Endpoint = "192.0.2.42:51823"
	if err := e.apply(context.Background(), m, private); err != nil || bind.opens != 1 {
		t.Fatal("peer refresh rebound local socket", err)
	}
	e.Close()
	if port, err := e.BoundPort(); err != ErrClosed || port != 0 {
		t.Fatal("closed socket retained receipt", port, err)
	}
}

func TestEngineRejectsUnacknowledgedBindWithoutPortFallback(t *testing.T) {
	for _, mode := range []string{"different", "empty", "nil-receive", "error"} {
		t.Run(mode, func(t *testing.T) {
			tunnel := newInertTUN()
			bind := &faultyPortBind{inertBind: &inertBind{}, mode: mode}
			e, err := NewEngine(tunnel, bind, 4, 51820, engineLease(t))
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			m, private := engineMap(t)
			if err := e.apply(context.Background(), m, private); err != ErrEngine || bind.opens != 1 || bind.closes == 0 {
				t.Fatal("invalid bind receipt accepted/retried", err)
			}
			if _, err := e.BoundPort(); err != ErrClosed {
				t.Fatal("failed bind called listening", err)
			}
			select {
			case <-tunnel.done:
			default:
				t.Fatal("failed bind leaked owned TUN")
			}
		})
	}
	tunnel := newInertTUN()
	defer func() { _ = tunnel.Close() }()
	if _, err := NewEngine(tunnel, &inertBind{}, 4, 0, engineLease(t)); err != ErrConfiguration {
		t.Fatal("random local port accepted", err)
	}
}
