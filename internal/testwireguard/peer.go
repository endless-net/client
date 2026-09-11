// Package testwireguard provides a protocol peer for real-client CI. It uses
// the pinned third-party WireGuard implementation, never Client runtime code.
package testwireguard

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"

	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/tailscale/wireguard-go/conn"
	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

type observedBind struct {
	conn.Bind
	port atomic.Uint32
}

func (b *observedBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	readers, actual, err := b.Bind.Open(port)
	if err == nil {
		b.port.Store(uint32(actual))
	}
	return readers, actual, err
}

type Peer struct {
	PublicKey, Endpoint string
	traffic             *trafficCounters
}

type trafficCounters struct{ received, echoed atomic.Uint64 }

func (p Peer) PacketCounts() (uint64, uint64) {
	return p.traffic.received.Load(), p.traffic.echoed.Load()
}

// NewUDP creates an encrypted UDP echo peer for ports 24001 and 24002. Its
// overlay address exists only in the channel TUN, never on the runner OS.
func NewUDP(t *testing.T, clientPublic string, clientIP, peerIP, underlayIP netip.Addr) Peer {
	t.Helper()
	if !clientIP.Is4() || !peerIP.Is4() {
		t.Fatal("reference peer requires IPv4 addresses")
	}
	if !underlayIP.Is4() || !underlayIP.IsGlobalUnicast() || underlayIP.IsLinkLocalUnicast() {
		t.Fatal("reference peer requires a usable direct underlay address")
	}
	private, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	toHex := func(value string) string {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			t.Fatal("invalid reference peer key material")
		}
		return hex.EncodeToString(decoded)
	}
	tunnel := tuntest.NewChannelTUN()
	bind := &observedBind{Bind: conn.NewStdNetBind()}
	engine := device.NewDevice(tunnel.TUN(), bind, &device.Logger{Verbosef: device.DiscardLogf, Errorf: device.DiscardLogf})
	done, exited := make(chan struct{}), make(chan struct{})
	traffic := &trafficCounters{}
	t.Cleanup(func() { close(done); engine.Close(); <-exited })
	go func() {
		defer close(exited)
		for {
			select {
			case <-done:
				return
			case packet, ok := <-tunnel.Inbound:
				if !ok {
					return
				}
				traffic.received.Add(1)
				reply := echoUDP(packet, clientIP.As4(), peerIP.As4())
				if reply == nil {
					continue
				}
				select {
				case tunnel.Outbound <- reply:
					traffic.echoed.Add(1)
				case <-done:
					return
				}
			}
		}
	}()
	uapi := fmt.Sprintf("private_key=%s\nlisten_port=0\nreplace_peers=true\npublic_key=%s\nallowed_ip=%s/32\n", toHex(private), toHex(clientPublic), clientIP)
	if err := engine.IpcSet(uapi); err != nil {
		t.Fatal("reference WireGuard peer rejected configuration")
	}
	if err := engine.Up(); err != nil {
		t.Fatal("reference WireGuard peer could not start")
	}
	port := bind.port.Load()
	if port == 0 {
		t.Fatal("reference peer has no UDP listener")
	}
	return Peer{PublicKey: public, Endpoint: net.JoinHostPort(underlayIP.String(), strconv.Itoa(int(port))), traffic: traffic}
}

func echoUDP(packet []byte, clientIP, peerIP [4]byte) []byte {
	// The application protocol is exactly one 32-byte nonce, without IP
	// options or fragmentation. Ignore all other traffic, including probes.
	if len(packet) != 60 || packet[0] != 0x45 || packet[9] != 17 || binary.BigEndian.Uint16(packet[2:4]) != 60 || binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0 || binary.BigEndian.Uint16(packet[24:26]) != 40 {
		return nil
	}
	if !bytes.Equal(packet[12:16], clientIP[:]) || !bytes.Equal(packet[16:20], peerIP[:]) {
		return nil
	}
	port := binary.BigEndian.Uint16(packet[22:24])
	if port != 24001 && port != 24002 {
		return nil
	}
	reply := append([]byte(nil), packet...)
	copy(reply[12:16], packet[16:20])
	copy(reply[16:20], packet[12:16])
	copy(reply[20:22], packet[22:24])
	copy(reply[22:24], packet[20:22])
	// Swapping addresses and ports preserves the IPv4 and UDP one's-complement
	// checksum sums, including the UDP pseudoheader; the payload is unchanged.
	return reply
}
