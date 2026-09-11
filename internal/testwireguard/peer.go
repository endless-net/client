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
	port                          atomic.Uint32
	initiations, responses, other atomic.Uint64
}

func (b *observedBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	readers, actual, err := b.Bind.Open(port)
	if err == nil {
		b.port.Store(uint32(actual))
		for i, reader := range readers {
			readers[i] = func(packets [][]byte, sizes []int, endpoints []conn.Endpoint) (int, error) {
				n, err := reader(packets, sizes, endpoints)
				for j := 0; j < n; j++ {
					if sizes[j] == 148 && binary.LittleEndian.Uint32(packets[j][:4]) == 1 {
						b.initiations.Add(1)
					} else {
						b.other.Add(1)
					}
				}
				return n, err
			}
		}
	}
	return readers, actual, err
}

func (b *observedBind) Send(packets [][]byte, endpoint conn.Endpoint, offset int) error {
	err := b.Bind.Send(packets, endpoint, offset)
	if err == nil {
		for _, packet := range packets {
			if len(packet)-offset == 92 && binary.LittleEndian.Uint32(packet[offset:offset+4]) == 2 {
				b.responses.Add(1)
			}
		}
	}
	return err
}

type Peer struct {
	PublicKey, Endpoint string
	traffic             *trafficCounters
	forwarded           *trafficCounters
	bind                *observedBind
	setEndpoint         func(netip.AddrPort) error
}

// SetClientEndpoint supplies the endpoint observed through public Client IPC.
// The pinned Tailscale WireGuard engine does not learn roaming endpoints from
// incoming handshakes, so the reference peer must configure its return path.
func (p Peer) SetClientEndpoint(t *testing.T, endpoint netip.AddrPort) {
	t.Helper()
	if !endpoint.IsValid() || endpoint.Port() == 0 {
		t.Fatal("reference peer requires the client's current UDP endpoint")
	}
	if err := p.setEndpoint(endpoint); err != nil {
		t.Fatal("reference peer could not configure the client endpoint")
	}
}

func (p Peer) HandshakeCounts() (uint64, uint64, uint64) {
	return p.bind.initiations.Load(), p.bind.responses.Load(), p.bind.other.Load()
}

type trafficCounters struct{ received, echoed atomic.Uint64 }

func (p Peer) PacketCounts() (uint64, uint64) {
	return p.traffic.received.Load(), p.traffic.echoed.Load()
}

// NewUDP creates an encrypted UDP echo peer for ports 24001 and 24002. Its
// overlay address exists only in the channel TUN, never on the runner OS.
func NewUDP(t *testing.T, clientPublic string, clientIP, peerIP, underlayIP netip.Addr) Peer {
	t.Helper()
	if !clientIP.IsValid() || !peerIP.IsValid() || clientIP.Is4() != peerIP.Is4() || clientIP.Is4In6() || peerIP.Is4In6() {
		t.Fatal("reference peer requires addresses of the same IP family")
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
				var reply []byte
				if clientIP.Is4() {
					reply = echoUDP(packet, clientIP.As4(), peerIP.As4())
				} else {
					reply = echoUDPv6(packet, clientIP.As16(), peerIP.As16())
				}
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
	uapi := fmt.Sprintf("private_key=%s\nlisten_port=0\nreplace_peers=true\npublic_key=%s\nallowed_ip=%s\n", toHex(private), toHex(clientPublic), netip.PrefixFrom(clientIP, clientIP.BitLen()))
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
	return Peer{
		PublicKey: public, Endpoint: net.JoinHostPort(underlayIP.String(), strconv.Itoa(int(port))), traffic: traffic, bind: bind,
		setEndpoint: func(endpoint netip.AddrPort) error {
			return engine.IpcSet(fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\n\n", toHex(clientPublic), endpoint))
		},
	}
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

func echoUDPv6(packet []byte, clientIP, peerIP [16]byte) []byte {
	// Only fixed-header IPv6 UDP carrying the 32-byte application nonce is
	// part of this fixture. Extension headers and fragments are not echoed.
	if len(packet) != 80 || packet[0]>>4 != 6 || packet[6] != 17 || binary.BigEndian.Uint16(packet[4:6]) != 40 || binary.BigEndian.Uint16(packet[44:46]) != 40 || binary.BigEndian.Uint16(packet[46:48]) == 0 {
		return nil
	}
	if !bytes.Equal(packet[8:24], clientIP[:]) || !bytes.Equal(packet[24:40], peerIP[:]) {
		return nil
	}
	port := binary.BigEndian.Uint16(packet[42:44])
	if port != 24001 && port != 24002 {
		return nil
	}
	reply := append([]byte(nil), packet...)
	copy(reply[8:24], packet[24:40])
	copy(reply[24:40], packet[8:24])
	copy(reply[40:42], packet[42:44])
	copy(reply[42:44], packet[40:42])
	// Address and port swaps preserve the UDP checksum's one's-complement
	// sum, including its IPv6 pseudoheader. IPv6 has no IP header checksum.
	return reply
}

// ForwardedPacketCounts observes IP packets crossing the router/resource link.
func (p Peer) ForwardedPacketCounts() (uint64, uint64) {
	if p.forwarded == nil {
		return 0, 0
	}
	return p.forwarded.received.Load(), p.forwarded.echoed.Load()
}
