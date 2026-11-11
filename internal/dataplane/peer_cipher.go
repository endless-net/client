package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wgconn "github.com/tailscale/wireguard-go/conn"
	wgdevice "github.com/tailscale/wireguard-go/device"
)

type cipherBind struct {
	awgconn.Bind
	port atomic.Uint32
}

func (b *cipherBind) Open(port uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	b.port.Store(0)
	functions, actual, err := b.Bind.Open(port)
	if err != nil || actual == 0 || len(functions) == 0 {
		_ = b.Bind.Close()
		return nil, 0, ErrEngine
	}
	for _, function := range functions {
		if function == nil {
			_ = b.Bind.Close()
			return nil, 0, ErrEngine
		}
	}
	b.port.Store(uint32(actual))
	return functions, actual, nil
}
func (b *cipherBind) Close() error { b.port.Store(0); return b.Bind.Close() }
func (*cipherBind) BatchSize() int { return 1 }

type guardedCipherBind interface {
	SendGuarded([][]byte, awgconn.Endpoint, []func(int) error) error
}

func sendCipherGuarded(bind awgconn.Bind, packets [][]byte, endpoint awgconn.Endpoint, guards []func(int) error) error {
	if len(guards) != len(packets) {
		return ErrPacketIO
	}
	if annotated, ok := bind.(guardedCipherBind); ok {
		return annotated.SendGuarded(packets, endpoint, guards)
	}
	for i, packet := range packets {
		if guards[i] != nil {
			if err := guards[i](len(packet)); err != nil {
				return err
			}
		}
		if err := bind.Send([][]byte{packet}, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func (b *cipherBind) SendGuarded(packets [][]byte, endpoint awgconn.Endpoint, guards []func(int) error) error {
	return sendCipherGuarded(b.Bind, packets, endpoint, guards)
}

// wireGuardBind translates only batching/offset conventions. Endpoint identity
// remains owned by the supplied bind; the adapter does not add sockets or routes.
type wireGuardBind struct{ awgconn.Bind }

func (*wireGuardBind) BatchSize() int { return 1 }
func (b *wireGuardBind) ParseEndpoint(value string) (wgconn.Endpoint, error) {
	return b.Bind.ParseEndpoint(value)
}
func (b *wireGuardBind) Open(port uint16) ([]wgconn.ReceiveFunc, uint16, error) {
	functions, actual, err := b.Bind.Open(port)
	if err != nil {
		return nil, 0, err
	}
	converted := make([]wgconn.ReceiveFunc, len(functions))
	for i, function := range functions {
		converted[i] = func(slab []byte, packets []wgconn.ReceivedPacket) (int, error) {
			if len(slab) == 0 || len(packets) < 1 {
				return 0, ErrPacketIO
			}
			sizes := []int{0}
			endpoints := make([]awgconn.Endpoint, 1)
			n, err := function([][]byte{slab}, sizes, endpoints)
			if n < 0 || n > 1 || n == 1 && (sizes[0] < 0 || sizes[0] > len(slab) || endpoints[0] == nil) {
				return 0, ErrPacketIO
			}
			if n == 1 {
				packets[0] = wgconn.ReceivedPacket{Size: sizes[0], Endpoint: endpoints[0]}
			}
			return n, err
		}
	}
	return converted, actual, nil
}
func (b *wireGuardBind) Send(packets [][]byte, endpoint wgconn.Endpoint, offset int) error {
	if offset < 0 {
		return ErrPacketIO
	}
	for _, packet := range packets {
		if offset > len(packet) {
			return ErrPacketIO
		}
		if err := b.Bind.Send([][]byte{packet[offset:]}, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func (b *wireGuardBind) SendGuarded(packets [][]byte, endpoint wgconn.Endpoint, offset int, guards []func(int) error) error {
	if offset < 0 || len(guards) != len(packets) {
		return ErrPacketIO
	}
	for i, packet := range packets {
		if offset > len(packet) {
			return ErrPacketIO
		}
		if err := sendCipherGuarded(b.Bind, [][]byte{packet[offset:]}, endpoint, guards[i:i+1]); err != nil {
			return err
		}
	}
	return nil
}

// peerCipher owns one isolated device and a virtual port. It never creates or
// closes the native TUN/routes/filters. All I/O policy remains in its port/bind.
type peerCipher struct {
	mu            sync.Mutex
	device        wireGuardDevice
	bind          *cipherBind
	port          *packetPort
	profileDigest string
	peer          coordinator.Peer
	closed        bool
	done          chan struct{}
	closeOnce     sync.Once
}

func (*peerCipher) String() string   { return "[isolated encrypted peer channel]" }
func (*peerCipher) GoString() string { return "[isolated encrypted peer channel]" }

func (c *peerCipher) setPacketHooks(outbound func([]byte) func(int) error, inbound func([]byte, int) error) {
	switch device := c.device.(type) {
	case *wgdevice.Device:
		device.SetPacketHooks(&wgdevice.PacketHooks{Outbound: outbound, Inbound: inbound})
	case *awgdevice.Device:
		device.SetPacketHooks(&awgdevice.PacketHooks{Outbound: outbound, Inbound: inbound})
	}
}

func profileUAPI(profile transport.Profile) ([]byte, error) {
	if transport.ValidateProfile(profile) != nil {
		return nil, ErrConfiguration
	}
	var output bytes.Buffer
	if profile.Engine == transport.AmneziaWG {
		awg := profile.AWG
		// The declarative prelude is emitted by the bounded bind, not upstream CPS.
		// Disable implicit junk and retain the pinned crypto timer defaults.
		output.WriteString("jc=0\njmin=0\njmax=0\n")
		for i, value := range awg.Padding {
			_, _ = fmt.Fprintf(&output, "s%d=%d\n", i+1, value)
		}
		for i, value := range awg.Headers {
			_, _ = fmt.Fprintf(&output, "h%d=%d-%d\n", i+1, value.Min, value.Max)
		}
		_, _ = fmt.Fprintf(&output, "header_protection_key=%s\ncontent_padding_addition=%d-%d\nrandom_trailers=%t\ndisable_cookies=%t\n",
			hex.EncodeToString(awg.HeaderProtectionKey), awg.ContentPadding.Min, awg.ContentPadding.Max, awg.RandomTrailers, awg.DisableCookies)
	}
	return output.Bytes(), nil
}

func cipherConfiguration(local coordinator.Node, peer coordinator.Peer, private []byte, profile transport.Profile) ([]byte, error) {
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil || bytes.Equal(private, make([]byte, 32)) || base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()) != local.PublicKey || peer.PublicKey == local.PublicKey || !peer.ACLRestricted || len(peer.AllowedIPs) != 1 {
		return nil, ErrConfiguration
	}
	public, err := base64.StdEncoding.Strict().DecodeString(peer.PublicKey)
	if err != nil || len(public) != 32 || bytes.Equal(public, make([]byte, 32)) || base64.StdEncoding.EncodeToString(public) != peer.PublicKey {
		return nil, ErrConfiguration
	}
	prefix, err := netip.ParsePrefix(peer.AllowedIPs[0])
	localIP, localErr := netip.ParseAddr(local.AssignedIP)
	if err != nil || prefix.Bits() != 32 || !prefix.Addr().Is4() || prefix.String() != peer.AllowedIPs[0] || localErr != nil || !localIP.Is4() || prefix.Addr() == localIP {
		return nil, ErrConfiguration
	}
	if peer.Endpoint != "" {
		endpoint, err := netip.ParseAddrPort(peer.Endpoint)
		if err != nil || !endpoint.Addr().Is4() || endpoint.Port() == 0 || endpoint.String() != peer.Endpoint {
			return nil, ErrConfiguration
		}
	}
	parameters, err := profileUAPI(profile)
	if err != nil {
		return nil, err
	}
	defer clear(parameters)
	var output bytes.Buffer
	_, _ = fmt.Fprintf(&output, "private_key=%s\nlisten_port=0\n", hex.EncodeToString(private))
	output.Write(parameters)
	_, _ = fmt.Fprintf(&output, "public_key=%s\nreplace_allowed_ips=true\nallowed_ip=%s\npersistent_keepalive_interval=0\n", hex.EncodeToString(public), peer.AllowedIPs[0])
	if peer.Endpoint != "" {
		_, _ = fmt.Fprintf(&output, "endpoint=%s\n", peer.Endpoint)
	}
	output.WriteByte('\n')
	return output.Bytes(), nil
}

func newPeerCipher(ctx context.Context, local coordinator.Node, peer coordinator.Peer, private []byte, profile transport.Profile, port *packetPort, bind awgconn.Bind) (*peerCipher, error) {
	return buildPeerCipher(ctx, local, peer, private, profile, port, bind, nil, nil)
}

func buildPeerCipher(ctx context.Context, local coordinator.Node, peer coordinator.Peer, private []byte, profile transport.Profile, port *packetPort, bind awgconn.Bind, observe wgdevice.PeerSessionStateFunc, publish func(*peerCipher)) (*peerCipher, error) {
	if ctx == nil || ctx.Err() != nil || port == nil || bind == nil {
		return nil, ErrConfiguration
	}
	raw, err := cipherConfiguration(local, peer, private, profile)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	digest, err := transport.ProfileDigest(profile)
	if err != nil {
		return nil, ErrConfiguration
	}
	guarded := &cipherBind{Bind: bind}
	var device wireGuardDevice
	configured := make(chan struct{})
	switch profile.Engine {
	case transport.WireGuard:
		d := wgdevice.NewDevice(wireGuardPort{port}, &wireGuardBind{Bind: guarded}, wgdevice.NewLogger(wgdevice.LogLevelSilent, ""))
		// Upstream does not replay current state to a newly installed observer.
		// Register before IpcSet/Up can start peers or receive a handshake.
		d.SetSessionStateFunc(observe)
		device = d
	case transport.AmneziaWG:
		device = awgdevice.NewDevice(&amneziaPort{packetPort: port, configured: configured, expectedOffset: awgdevice.MessageTransportHeaderSize + int(profile.AWG.Padding[3])}, guarded, awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	default:
		return nil, ErrConfiguration
	}
	cipher := &peerCipher{device: device, bind: guarded, port: port, profileDigest: digest, peer: peer, done: make(chan struct{})}
	if device.IpcSetOperation(bytes.NewReader(raw)) != nil || ctx.Err() != nil {
		cipher.Close()
		return nil, ErrEngine
	}
	close(configured)
	// Up may immediately deliver authenticated plaintext. Publish the fully
	// configured channel before its receiver can answer through the owner.
	if publish != nil {
		publish(cipher)
	}
	if device.Up() != nil || ctx.Err() != nil || guarded.port.Load() == 0 {
		cipher.Close()
		return nil, ErrEngine
	}
	return cipher, nil
}

func (c *peerCipher) Queue(raw []byte, control bool) error {
	return c.queueChecked(raw, control, nil)
}
func (c *peerCipher) queueChecked(raw []byte, control bool, guard func() bool) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	return c.port.enqueueChecked(raw, control, guard)
}
func (c *peerCipher) matchesProfile(profile transport.Profile) bool {
	digest, err := transport.ProfileDigest(profile)
	return err == nil && digest == c.profileDigest
}

// Recognize only upstream-generated handshake output for bounded retransmit.
// Classification grants no authentication; the receiving engine still verifies
// MAC, replay and session state. AWG header protection is parsed by upstream.
func (c *peerCipher) handshakePacket(raw []byte) bool {
	if d, ok := c.device.(*awgdevice.Device); ok {
		if len(raw) < awgdevice.MinMessageSize {
			return false
		}
		cipher, err := d.HeaderProtectionCipher(raw[:awgdevice.HeaderCipherNonceSize])
		if err != nil {
			return false
		}
		var hash [4]byte
		if cipher != nil {
			cipher.XORKeyStream(hash[:], hash[:])
		}
		_, kind, _ := d.DeterminePacketTypeAndPadding(raw, hash[:])
		return kind == awgdevice.MessageInitiationType || kind == awgdevice.MessageResponseType
	}
	if len(raw) < 4 {
		return false
	}
	kind := binary.LittleEndian.Uint32(raw[:4])
	return kind == wgdevice.MessageInitiationType && len(raw) == wgdevice.MessageInitiationSize || kind == wgdevice.MessageResponseType && len(raw) == wgdevice.MessageResponseSize
}

func (c *peerCipher) BoundPort() (uint16, error) {
	port := c.bind.port.Load()
	if port == 0 {
		return 0, ErrClosed
	}
	return uint16(port), nil
}

func (c *peerCipher) SetEndpoint(ctx context.Context, endpoint string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrConfiguration
	}
	address, err := netip.ParseAddrPort(endpoint)
	if err != nil || !address.Addr().Is4() || address.Addr().IsUnspecified() || address.Addr().IsMulticast() || address.Addr().IsLoopback() || address.Port() == 0 || address.String() != endpoint {
		return ErrConfiguration
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	public, err := base64.StdEncoding.Strict().DecodeString(c.peer.PublicKey)
	if err != nil {
		c.mu.Unlock()
		return ErrConfiguration
	}
	// Never set listen_port on refresh: the prepared socket must keep its port.
	raw := []byte(fmt.Sprintf("public_key=%s\nendpoint=%s\n\n", hex.EncodeToString(public), endpoint))
	err = c.device.IpcSetOperation(bytes.NewReader(raw))
	clear(raw)
	if err != nil || ctx.Err() != nil {
		c.closed = true
		c.mu.Unlock()
		c.closeDevice()
		return ErrEngine
	}
	c.peer.Endpoint = endpoint
	c.mu.Unlock()
	return nil
}

func (c *peerCipher) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeDevice()
}

func (c *peerCipher) closeDevice() {
	c.closeOnce.Do(func() { c.device.Close(); close(c.done) })
	<-c.done
}
