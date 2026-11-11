package dataplane

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wgconn "github.com/tailscale/wireguard-go/conn"
)

type cipherFixtureBind struct {
	*memoryWireBind
	opens      atomic.Uint32
	mu         sync.Mutex
	last       []byte
	first      []byte
	beforeOpen func()
}

func (b *cipherFixtureBind) Open(port uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	if b.beforeOpen != nil {
		b.beforeOpen()
	}
	if port == 0 {
		port = 51820
	}
	b.opens.Add(1)
	functions, actual, err := b.memoryWireBind.Open(port)
	if err != nil {
		return nil, 0, err
	}
	out := make([]awgconn.ReceiveFunc, len(functions))
	for i, receive := range functions {
		out[i] = func(bufs [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
			if len(bufs) < 1 || len(sizes) < 1 || len(endpoints) < 1 {
				return 0, ErrPacketIO
			}
			packets := make([]wgconn.ReceivedPacket, 1)
			n, err := receive(bufs[0], packets)
			if n == 1 {
				sizes[0] = packets[0].Size
				endpoints[0] = packets[0].Endpoint
			}
			return n, err
		}
	}
	return out, actual, nil
}

func TestCipherOwnerPublishedBeforeReceiverStarts(t *testing.T) {
	for _, engine := range []string{transport.WireGuard, transport.AmneziaWG} {
		t.Run(engine, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			snapshot, private := engineMap(t)
			defer clear(private)
			permission := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
			port, err := newPacketPort(ctx, 1420, permission, func([]byte) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			var owner *peerCipher
			bind := &cipherFixtureBind{memoryWireBind: &memoryWireBind{address: "192.0.2.1:51820"}}
			bind.beforeOpen = func() {
				if owner == nil || owner.device == nil || owner.bind == nil || owner.port != port || owner.done == nil || owner.profileDigest == "" {
					t.Error("receiver started before owner could answer its first authenticated packet")
				}
			}
			cipher, err := buildPeerCipher(ctx, snapshot.Node, snapshot.Peers[0], private, cipherProfile(engine), port, bind, nil, func(value *peerCipher) { owner = value })
			if err != nil {
				t.Fatal(err)
			}
			defer cipher.Close()
			if owner != cipher || bind.opens.Load() != 1 {
				t.Fatal("published owner differs from live channel")
			}
		})
	}
}
func (b *cipherFixtureBind) ParseEndpoint(value string) (awgconn.Endpoint, error) {
	return b.memoryWireBind.ParseEndpoint(value)
}
func (b *cipherFixtureBind) Send(bufs [][]byte, endpoint awgconn.Endpoint) error {
	for _, raw := range bufs {
		b.mu.Lock()
		if b.first == nil {
			b.first = bytes.Clone(raw)
		}
		b.last = bytes.Clone(raw)
		b.mu.Unlock()
	}
	return b.memoryWireBind.Send(bufs, endpoint, 0)
}

func TestPeerCipherMetadataCarriesActualWireSizeAndDropsOnlyAnnotatedPackets(t *testing.T) {
	profiles := []transport.Profile{cipherProfile(transport.WireGuard), cipherProfile(transport.AmneziaWG), cipherProfile(transport.AmneziaWG)}
	profiles[2].ID = "awg-zero-content-padding"
	profiles[2].AWG.ContentPadding = transport.Range{}
	for _, profile := range profiles {
		t.Run(profile.ID+"/"+profile.Engine, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			snapshot, private := engineMap(t)
			defer clear(private)
			local, peer := snapshot.Node, snapshot.Peers[0]
			remote := coordinator.Node{PublicKey: peer.PublicKey, AssignedIP: "100.64.0.2"}
			reverse := coordinator.Peer{PublicKey: local.PublicKey, Endpoint: "192.0.2.1:51820", AllowedIPs: []string{local.AssignedIP + "/32"}, ACLRestricted: true}
			firstWire := &memoryWireBind{address: reverse.Endpoint}
			secondWire := &memoryWireBind{address: peer.Endpoint}
			firstWire.peer, secondWire.peer = secondWire, firstWire
			permission := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
			output := make(chan []byte, 4)
			firstPort, err := newPacketPort(ctx, 1420, permission, func([]byte) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			secondPort, err := newPacketPort(ctx, 1420, permission, func(raw []byte) error { output <- bytes.Clone(raw); return nil })
			if err != nil {
				t.Fatal(err)
			}
			ready, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var sentBytes, receivedBytes atomic.Int64
			first, err := buildPeerCipher(ctx, local, peer, private, profile, firstPort, &cipherFixtureBind{memoryWireBind: firstWire}, nil, func(c *peerCipher) {
				c.setPacketHooks(func(raw []byte) func(int) error {
					if len(raw) != 29 {
						return nil
					}
					switch raw[28] {
					case 1:
						return func(int) error {
							once.Do(func() { close(ready) })
							select {
							case <-release:
								return ErrNoAuthority
							case <-ctx.Done():
								return ctx.Err()
							}
						}
					case 2:
						return func(size int) error { sentBytes.Store(int64(size)); return nil }
					}
					return nil
				}, nil)
			})
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := buildPeerCipher(ctx, remote, reverse, bytes.Repeat([]byte{12}, 32), profile, secondPort, &cipherFixtureBind{memoryWireBind: secondWire}, nil, func(c *peerCipher) {
				c.setPacketHooks(nil, func(raw []byte, size int) error {
					if len(raw) == 29 {
						if raw[28] == 3 {
							return ErrNoAuthority
						}
						if raw[28] == 2 {
							receivedBytes.Store(int64(size))
						}
					}
					return nil
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if err := first.Queue(cipherIPv4("100.64.0.1", "100.64.0.2", 1), true); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("annotated staged packet never reached actual send", ctx.Err())
			}
			close(release) // A late failure after staging/encryption drops only packet1.
			for _, payload := range []byte{3, 2, 4} {
				if err := first.Queue(cipherIPv4("100.64.0.1", "100.64.0.2", payload), false); err != nil {
					t.Fatal(err)
				}
			}
			for _, payload := range []byte{2, 4} {
				select {
				case raw := <-output:
					if !bytes.Equal(raw, cipherIPv4("100.64.0.1", "100.64.0.2", payload)) {
						t.Fatal("guard admitted a rejected packet or disturbed application ordering")
					}
				case <-ctx.Done():
					t.Fatal("packet rejection stopped neighboring application traffic", ctx.Err())
				}
			}
			if sentBytes.Load() <= 29 || sentBytes.Load() != receivedBytes.Load() {
				t.Fatal("post-AEAD metadata lost actual padding/header wire size", sentBytes.Load(), receivedBytes.Load())
			}
		})
	}
}

func cipherProfile(engine string) transport.Profile {
	profile := transport.Profile{Version: 1, ID: "fixture-profile", Revision: 1, Engine: engine, ProtocolVersion: "1", Prelude: []transport.Datagram{}}
	if engine == transport.AmneziaWG {
		profile.ProtocolVersion = "3.1"
		profile.AWG = &transport.AWG{Headers: []transport.Range{{Min: 100, Max: 200}, {Min: 300, Max: 400}, {Min: 500, Max: 600}, {Min: 700, Max: 800}}, Padding: []uint16{12, 16, 20, 24},
			HeaderProtectionKey: bytes.Repeat([]byte{0x8a}, 32), ContentPadding: transport.Range{Min: 0, Max: 32}, RandomTrailers: true}
	}
	return profile
}

func cipherIPv4(source, destination string, payload byte) []byte {
	packet := make([]byte, 29)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	src, dst := netip.MustParseAddr(source).As4(), netip.MustParseAddr(destination).As4()
	copy(packet[12:16], src[:])
	copy(packet[16:20], dst[:])
	binary.BigEndian.PutUint16(packet[20:22], 12000)
	binary.BigEndian.PutUint16(packet[22:24], 443)
	binary.BigEndian.PutUint16(packet[24:26], 9)
	packet[28] = payload
	sum := uint32(0)
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	binary.BigEndian.PutUint16(packet[10:12], ^uint16(sum))
	return packet
}

// Actual pinned cryptography runs over bounded memory datagrams. The injected
// permission/sink are transport fixtures, not Node authority or native ACL proof.
func TestIsolatedWireGuardAndAWGEncryptSourceFenceAndRejectReplay(t *testing.T) {
	for _, engine := range []string{transport.WireGuard, transport.AmneziaWG} {
		t.Run(engine, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			snapshot, private := engineMap(t)
			defer clear(private)
			local := snapshot.Node
			peer := snapshot.Peers[0]
			reverse := coordinator.Peer{PublicKey: local.PublicKey, Endpoint: "192.0.2.1:51820", AllowedIPs: []string{local.AssignedIP + "/32"}, ACLRestricted: true}
			peerLocal := coordinator.Node{PublicKey: peer.PublicKey, AssignedIP: "100.64.0.2"}
			firstWire := &memoryWireBind{address: "192.0.2.1:51820"}
			secondWire := &memoryWireBind{address: peer.Endpoint}
			firstWire.peer, secondWire.peer = secondWire, firstWire
			firstBind, secondBind := &cipherFixtureBind{memoryWireBind: firstWire}, &cipherFixtureBind{memoryWireBind: secondWire}
			permission := func() connectivity.Permission { return connectivity.Permission{NewFlows: true, ExistingFlows: true} }
			received := make(chan []byte, 16)
			firstPort, err := newPacketPort(ctx, 1420, permission, func([]byte) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			secondPort, err := newPacketPort(ctx, 1420, permission, func(raw []byte) error {
				select {
				case received <- bytes.Clone(raw):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			first, err := newPeerCipher(ctx, local, peer, private, cipherProfile(engine), firstPort, firstBind)
			if err != nil {
				t.Fatal("first cipher rejected", err)
			}
			defer first.Close()
			second, err := newPeerCipher(ctx, peerLocal, reverse, bytes.Repeat([]byte{12}, 32), cipherProfile(engine), secondPort, secondBind)
			if err != nil {
				t.Fatal("second cipher rejected", err)
			}
			defer second.Close()
			changed := cipherProfile(engine)
			changed.Revision++
			if !first.matchesProfile(cipherProfile(engine)) || first.matchesProfile(changed) {
				t.Fatal("profile revision reused old engine parameters")
			}
			expected := cipherIPv4("100.64.0.1", "100.64.0.2", 1)
			if err := first.Queue(expected, false); err != nil {
				t.Fatal(err)
			}
			select {
			case raw := <-received:
				if !bytes.Equal(raw, expected) {
					t.Fatal("cipher plaintext changed")
				}
			case <-ctx.Done():
				firstBind.mu.Lock()
				firstSize := len(firstBind.last)
				firstBind.mu.Unlock()
				secondBind.mu.Lock()
				secondSize := len(secondBind.last)
				secondBind.mu.Unlock()
				t.Fatal("actual engine roundtrip failed", ctx.Err(), "last encrypted sizes", firstSize, secondSize, "opens", firstBind.opens.Load(), secondBind.opens.Load())
			}
			if err := first.SetEndpoint(ctx, peer.Endpoint); err != nil {
				t.Fatal(err)
			}
			if port, err := first.BoundPort(); err != nil || port != 51820 || firstBind.opens.Load() != 1 {
				t.Fatal("endpoint refresh recreated prepared listener", err)
			}
			firstBind.mu.Lock()
			replay := bytes.Clone(firstBind.last)
			handshake := bytes.Clone(firstBind.first)
			firstBind.mu.Unlock()
			if !first.handshakePacket(handshake) || first.handshakePacket(replay) || first.handshakePacket([]byte{1}) {
				t.Fatal("upstream handshake classification confused initial/data/malformed frames")
			}
			secondWire.socket.Load().packets <- memoryWirePacket{data: replay, source: firstWire.address}
			if err := first.Queue(cipherIPv4("100.64.0.9", "100.64.0.2", 2), false); err != nil {
				t.Fatal(err)
			}
			barrier := cipherIPv4("100.64.0.1", "100.64.0.2", 3)
			if err := first.Queue(barrier, false); err != nil {
				t.Fatal(err)
			}
			select {
			case raw := <-received:
				if !bytes.Equal(raw, barrier) {
					t.Fatal("replay or forged source passed authenticated engine")
				}
			case <-ctx.Done():
				t.Fatal("valid packet after rejected input was lost")
			}
		})
	}
}

func TestCipherConfigurationRejectsUnknownProfileWithoutRawConfiguration(t *testing.T) {
	snapshot, private := engineMap(t)
	defer clear(private)
	profile := cipherProfile(transport.AmneziaWG)
	if _, err := cipherConfiguration(snapshot.Node, snapshot.Peers[0], private, profile); err != nil {
		t.Fatal(err)
	}
	profile.ProtocolVersion = "future"
	if _, err := cipherConfiguration(snapshot.Node, snapshot.Peers[0], private, profile); err == nil {
		t.Fatal("unknown engine semantics accepted")
	}
	profile = cipherProfile(transport.WireGuard)
	profile.AWG = cipherProfile(transport.AmneziaWG).AWG
	if _, err := profileUAPI(profile); err == nil {
		t.Fatal("partial cross-engine parameters accepted")
	}
}
