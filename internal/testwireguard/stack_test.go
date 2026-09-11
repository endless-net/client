package testwireguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

func TestReferenceStackCloseWithActiveTCP(t *testing.T) {
	for _, addresses := range [][2]string{
		{"192.0.2.1", "192.0.2.2"}, {"fd00::1", "fd00::2"},
	} {
		t.Run(addresses[0], func(t *testing.T) {
			left, err := newReferenceStack(netip.MustParseAddr(addresses[0]))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = left.Close() })
			right, err := newReferenceStack(netip.MustParseAddr(addresses[1]))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = right.Close() })
			var forward atomic.Bool
			forward.Store(true)
			var workers sync.WaitGroup
			for _, pair := range [][2]*referenceStack{{left, right}, {right, left}} {
				workers.Add(1)
				go func() {
					defer workers.Done()
					buffers, sizes := [][]byte{make([]byte, 65535)}, []int{0}
					for {
						if _, err := pair[0].Read(buffers, sizes, 0); err != nil {
							return
						}
						if forward.Load() {
							_, _ = pair[1].Write([][]byte{buffers[0][:sizes[0]]}, 0)
						}
					}
				}()
			}
			address := netip.AddrPortFrom(netip.MustParseAddr(addresses[1]), 24001)
			listener, err := right.ListenTCPAddrPort(address)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stopAccept := context.AfterFunc(ctx, func() { _ = listener.Close() })
			defer stopAccept()
			remote, protocol := referenceAddress(address)
			client, err := gonet.DialContextTCP(ctx, left.stack, remote, protocol)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			server, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			for _, connection := range []net.Conn{client, server} {
				if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := client.Write([]byte("live")); err != nil {
				t.Fatal(err)
			}
			var payload [4]byte
			if _, err := io.ReadFull(server, payload[:]); err != nil || string(payload[:]) != "live" {
				t.Fatal("reference TCP did not deliver payload before teardown")
			}
			// Leave data unacknowledged, as happens when Client withdraws a route
			// or stops before its peer. Close must join the TCP reset workers.
			forward.Store(false)
			if _, err := server.Write([]byte("pending")); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				_ = server.Close()
				_ = right.Close()
				_ = client.Close()
				_ = left.Close()
				workers.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("reference stack teardown did not finish")
			}
			for _, peer := range []*referenceStack{left, right} {
				if _, err := peer.Write([][]byte{{0x45}}, 0); !errors.Is(err, os.ErrClosed) {
					t.Fatal("closed reference stack accepted packet injection")
				}
				if _, err := peer.Read([][]byte{make([]byte, 1280)}, []int{0}, 0); !errors.Is(err, os.ErrClosed) {
					t.Fatal("closed reference stack did not release packet reader")
				}
			}
		})
	}
}
