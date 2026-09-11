package testwireguard

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"

	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
)

// NewTCP serves TCP and UDP 32-byte nonce echoes through a protocol stack whose
// address is never installed on the runner OS. Client uses its real OS stack.
func NewTCP(t *testing.T, clientPublic string, clientIP, peerIP, underlayIP netip.Addr) Peer {
	t.Helper()
	return newTCP(t, clientPublic, clientIP, peerIP, underlayIP, false)
}

// NewRoutedResource places the resource stack behind a separate IP-forwarding
// hop, rather than attaching WireGuard directly to that stack.
func NewRoutedResource(t *testing.T, clientPublic string, clientIP, resourceIP, underlayIP netip.Addr) Peer {
	t.Helper()
	return newTCP(t, clientPublic, clientIP, resourceIP, underlayIP, true)
}

func newTCP(t *testing.T, clientPublic string, clientIP, peerIP, underlayIP netip.Addr, routed bool) Peer {
	t.Helper()
	if !clientIP.IsValid() || !peerIP.IsValid() || clientIP.Is4() != peerIP.Is4() || !underlayIP.Is4() || !underlayIP.IsGlobalUnicast() {
		t.Fatal("invalid TCP reference peer address family")
	}
	stack, err := newReferenceStack(peerIP)
	if err != nil {
		t.Fatal("could not create reference TCP stack")
	}
	var tunnel tun.Device = stack
	private, err := wg.GeneratePrivateKey()
	if err != nil {
		_ = tunnel.Close()
		t.Fatal("could not create reference identity")
	}
	public, err := wg.PublicKey(private)
	if err != nil {
		_ = tunnel.Close()
		t.Fatal("could not derive reference identity")
	}
	toHex := func(value string) string {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			t.Fatal("invalid reference key material")
		}
		return hex.EncodeToString(decoded)
	}
	bind := &observedBind{Bind: conn.NewStdNetBind()}
	transport := tunnel
	var forwarded *trafficCounters
	stopLink, waitLink := func() {}, func() {}
	if routed {
		transport, forwarded, stopLink, waitLink = newResourceLink(tunnel, clientIP, peerIP)
	}
	engine := device.NewDevice(transport, bind, &device.Logger{Verbosef: device.DiscardLogf, Errorf: device.DiscardLogf})
	t.Cleanup(func() { stopLink(); engine.Close(); waitLink() })
	if err := engine.IpcSet(fmt.Sprintf("private_key=%s\nlisten_port=0\nreplace_peers=true\npublic_key=%s\nallowed_ip=%s\n\n", toHex(private), toHex(clientPublic), netip.PrefixFrom(clientIP, clientIP.BitLen()))); err != nil {
		t.Fatal("reference TCP peer rejected WireGuard configuration")
	}
	if err := engine.Up(); err != nil || bind.port.Load() == 0 {
		t.Fatal("reference TCP peer could not start")
	}
	traffic := &trafficCounters{}
	var mu sync.Mutex
	var workers sync.WaitGroup
	closed := false
	connections := map[net.Conn]struct{}{}
	var listeners []net.Listener
	var packets []net.PacketConn
	t.Cleanup(func() {
		mu.Lock()
		closed = true
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		for _, packet := range packets {
			_ = packet.Close()
		}
		workers.Wait()
	})
	for _, port := range []uint16{24001, 24002} {
		packet, err := stack.ListenUDPAddrPort(netip.AddrPortFrom(peerIP, port))
		if err != nil {
			t.Fatal("reference UDP listener could not start")
		}
		packets = append(packets, packet)
		workers.Add(1)
		go func() {
			defer workers.Done()
			var payload [2048]byte
			for {
				n, from, err := packet.ReadFrom(payload[:])
				if err != nil {
					return
				}
				if n != 32 {
					continue
				}
				traffic.received.Add(1)
				if sent, err := packet.WriteTo(payload[:n], from); err == nil && sent == n {
					traffic.echoed.Add(1)
				}
			}
		}()
		listener, err := stack.ListenTCPAddrPort(netip.AddrPortFrom(peerIP, port))
		if err != nil {
			t.Fatal("reference TCP listener could not start")
		}
		listeners = append(listeners, listener)
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				c, err := listener.Accept()
				if err != nil {
					return
				}
				mu.Lock()
				if closed {
					mu.Unlock()
					_ = c.Close()
					return
				}
				connections[c] = struct{}{}
				workers.Add(1)
				mu.Unlock()
				go func() {
					defer workers.Done()
					defer func() { _ = c.Close(); mu.Lock(); delete(connections, c); mu.Unlock() }()
					var nonce [32]byte
					for {
						if _, err := io.ReadFull(c, nonce[:]); err != nil {
							return
						}
						traffic.received.Add(1)
						if n, err := c.Write(nonce[:]); err != nil || n != len(nonce) {
							return
						}
						traffic.echoed.Add(1)
					}
				}()
			}
		}()
	}
	return Peer{
		PublicKey: public, Endpoint: netip.AddrPortFrom(underlayIP, uint16(bind.port.Load())).String(), traffic: traffic, bind: bind, forwarded: forwarded,
		setEndpoint: func(endpoint netip.AddrPort) error {
			return engine.IpcSet(fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\n\n", toHex(clientPublic), endpoint))
		},
	}
}
