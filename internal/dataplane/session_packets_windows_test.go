//go:build windows

package dataplane

import (
	"context"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/tailscale/wireguard-go/tun"
)

type observedNativeReader struct {
	nativeTUNDevice
	entered, exited chan struct{}
}

func (d *observedNativeReader) Read(slab []byte, packets []tun.ReadPacket) (int, error) {
	close(d.entered) // a competing native reader would panic in this fixture
	defer close(d.exited)
	return d.nativeTUNDevice.Read(slab, packets)
}

func TestWindowsPrimaryUsesVirtualPortAndSharedReaderClosesAfterPermits(t *testing.T) {
	s, _, _, m, _, dynamic, _, native, _ := wiredSessionFixture(t)
	reader := &observedNativeReader{entered: make(chan struct{}), exited: make(chan struct{})}
	original := s.createTUN
	s.createTUN = func(ctx context.Context, record state.AdapterRecord, mtu int, writer AdapterJournalWriter) (*WindowsTUN, error) {
		value, err := original(ctx, record, mtu, writer)
		if err == nil {
			reader.nativeTUNDevice = value.nativeTUNDevice
			value.nativeTUNDevice = reader
		}
		return value, err
	}
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reader.entered:
	case <-time.After(time.Second):
		t.Fatal("native switch did not start its reader")
	}
	s.engine.Close()
	select {
	case <-reader.exited:
		t.Fatal("primary engine closed the shared native reader")
	default:
	}
	previousClose := native.afterClose
	native.afterClose = func() {
		if len(dynamic.live) != 0 {
			t.Error("native reader closed before removing dynamic permits")
		}
		previousClose()
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reader.exited:
	default:
		t.Fatal("native reader outlived completed Stop")
	}
	if native.closes != 1 {
		t.Fatal("native TUN closed more than once", native.closes)
	}
}
