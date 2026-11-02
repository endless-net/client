//go:build integration

package dataplane

import (
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tailscale/wireguard-go/conn"
)

type observedDirectSocket struct {
	conn.Bind
	sent, received, sendErrors, unexpected atomic.Int64
	expected                               string
}

func (b *observedDirectSocket) Send(bufs [][]byte, endpoint conn.Endpoint, offset int) error {
	b.sent.Add(int64(len(bufs)))
	err := b.Bind.Send(bufs, endpoint, offset)
	if err != nil {
		b.sendErrors.Add(1)
	}
	return err
}
func (b *observedDirectSocket) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actual, err := b.Bind.Open(port)
	if err != nil {
		return fns, actual, err
	}
	for i, receive := range fns {
		fns[i] = func(slab []byte, packets []conn.ReceivedPacket) (int, error) {
			n, err := receive(slab, packets)
			b.received.Add(int64(n))
			for _, p := range packets[:n] {
				if p.Endpoint.DstToString() != b.expected {
					b.unexpected.Add(1)
				}
			}
			return n, err
		}
	}
	return fns, actual, nil
}

// Traffic is addressed only to this host's own existing physical IPv4 address.
// The fixture never loads Wintun or changes adapters, routes, DNS or WFP. Actual
// socket/crypto evidence is not two-device/application or native ACL acceptance.
func TestActualUDPWireGuardDirectConfirmation(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit local UDP transport/cryptography fixture")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var address netip.Addr
	for _, i := range interfaces {
		name := strings.ToLower(i.Name)
		if i.Flags&net.FlagUp == 0 || i.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 || strings.Contains(name, "endless") || strings.Contains(name, "wireguard") || strings.Contains(name, "wintun") || strings.Contains(name, "tailscale") {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr().Unmap()
			if ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLoopback() {
				address = ip
				break
			}
		}
		if address.IsValid() {
			break
		}
	}
	if !address.IsValid() {
		t.Skip("not-verified: no existing physical non-loopback IPv4 for local-only socket fixture")
	}
	firstReservation, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(address.AsSlice())})
	if err != nil {
		t.Fatal(err)
	}
	secondReservation, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP(address.AsSlice())})
	if err != nil {
		_ = firstReservation.Close()
		t.Fatal(err)
	}
	firstEndpoint := netip.AddrPortFrom(address, uint16(firstReservation.LocalAddr().(*net.UDPAddr).Port)).String()
	secondEndpoint := netip.AddrPortFrom(address, uint16(secondReservation.LocalAddr().(*net.UDPAddr).Port)).String()
	if err := firstReservation.Close(); err != nil {
		_ = secondReservation.Close()
		t.Fatal(err)
	}
	if err := secondReservation.Close(); err != nil {
		t.Fatal(err)
	}
	firstBind := &observedDirectSocket{Bind: conn.NewStdNetBind(), expected: secondEndpoint}
	secondBind := &observedDirectSocket{Bind: conn.NewStdNetBind(), expected: firstEndpoint}
	first, second, _ := directEnginePair(t, firstBind, secondBind, firstEndpoint, secondEndpoint)
	for _, err := range waitDirectPair(t, first, second, 15*time.Second) {
		if err != nil {
			t.Fatal("actual local UDP crypto path not confirmed", err, "first tx/rx/errors/foreign", firstBind.sent.Load(), firstBind.received.Load(), firstBind.sendErrors.Load(), firstBind.unexpected.Load(), "second tx/rx/errors/foreign", secondBind.sent.Load(), secondBind.received.Load(), secondBind.sendErrors.Load(), secondBind.unexpected.Load())
		}
	}
	for _, e := range []*Engine{first, second} {
		paths, err := e.DirectPaths()
		if err != nil || len(paths) != 1 || !paths[0].Authenticated || paths[0].ConfirmedAt.IsZero() {
			t.Fatal("actual transport evidence missing", err)
		}
	}
	first.Close()
	second.Close()
	for _, endpoint := range []string{firstEndpoint, secondEndpoint} {
		addr, err := net.ResolveUDPAddr("udp4", endpoint)
		if err != nil {
			t.Fatal(err)
		}
		probe, err := net.ListenUDP("udp4", addr)
		if err != nil {
			t.Fatal("owned socket leaked", err)
		}
		if err := probe.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
