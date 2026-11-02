//go:build integration

package dataplane

import (
	"context"
	"net"
	"testing"

	"github.com/tailscale/wireguard-go/conn"
)

// Actual UDP bind failure with an inert TUN and no peers. It creates no native
// adapter/routes/filters, sends no remote traffic, and never loads Wintun.
func TestActualWireGuardBusyListenPortHasNoRandomFallback(t *testing.T) {
	reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reserved.Close() }()
	port := uint16(reserved.LocalAddr().(*net.UDPAddr).Port)
	tunnel := newInertTUN()
	e, err := NewEngine(tunnel, conn.NewStdNetBind(), 4, port, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	m, private := engineMap(t)
	m.Peers = nil
	if err := e.apply(context.Background(), m, private); err != ErrEngine {
		t.Fatal("busy actual port did not fail closed", err)
	}
	if p, err := e.BoundPort(); p != 0 || err != ErrClosed {
		t.Fatal("fallback listener exposed", p, err)
	}
	select {
	case <-tunnel.done:
	default:
		t.Fatal("failed socket left TUN alive")
	}
}

func TestActualWireGuardConfiguredPortRemainsBoundUntilClose(t *testing.T) {
	reservation, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(reservation.LocalAddr().(*net.UDPAddr).Port)
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(newInertTUN(), conn.NewStdNetBind(), 4, port, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	m, private := engineMap(t)
	m.Peers = nil // no remote endpoint or transmitted packet in this fixture
	if err := e.apply(context.Background(), m, private); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if actual, err := e.BoundPort(); err != nil || actual != port {
			t.Fatal("actual listener does not match configuration", actual, err)
		}
		other, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: int(port)})
		if err == nil {
			_ = other.Close()
			t.Fatal("acknowledged listener did not own requested UDP port")
		}
		if err := e.apply(context.Background(), m, private); err != nil {
			t.Fatal(err)
		}
	}
	e.Close()
	available, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: int(port)})
	if err != nil {
		t.Fatal("engine shutdown leaked actual listener", err)
	}
	_ = available.Close()
}
