package dataplane

import (
	"bytes"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/endless-net/client/api/transport"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// The packet socket is a bounded in-memory datagram fixture, not a real UDP
// or filtered-network assertion. The cryptographic devices/codecs are real.
type socketCipherFixtureBind struct {
	mu     sync.Mutex
	socket *candidateSocketFixture
	opened bool
	port   uint16
}

func (b *socketCipherFixtureBind) Open(uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.opened {
		return nil, 0, awgconn.ErrBindAlreadyOpen
	}
	b.opened = true
	return []awgconn.ReceiveFunc{func(packets [][]byte, sizes []int, endpoints []awgconn.Endpoint) (int, error) {
		n, address, err := b.socket.ReadFromUDPAddrPort(packets[0])
		if err != nil {
			return 0, err
		}
		sizes[0] = n
		endpoints[0] = &awgconn.StdNetEndpoint{AddrPort: address}
		return 1, nil
	}}, b.port, nil
}
func (b *socketCipherFixtureBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.opened {
		b.opened = false
		return b.socket.Close()
	}
	return nil
}
func (*socketCipherFixtureBind) SetMark(uint32) error { return nil }
func (*socketCipherFixtureBind) BatchSize() int       { return 1 }
func (*socketCipherFixtureBind) ParseEndpoint(value string) (awgconn.Endpoint, error) {
	address, err := netip.ParseAddrPort(value)
	if err != nil {
		return nil, err
	}
	return &awgconn.StdNetEndpoint{AddrPort: address}, nil
}
func (b *socketCipherFixtureBind) Send(packets [][]byte, endpoint awgconn.Endpoint) error {
	address, err := netip.ParseAddrPort(endpoint.DstToString())
	if err != nil {
		return err
	}
	for _, raw := range packets {
		if _, err := b.socket.WriteToUDPAddrPort(raw, address); err != nil {
			return err
		}
	}
	return nil
}

func TestActualIsolatedCryptoChallengeEchoOverCandidateSocket(t *testing.T) {
	for _, engine := range []string{transport.WireGuard, transport.AmneziaWG} {
		t.Run(engine, func(t *testing.T) {
			f := newCandidateFixture(t)
			// Select the other published engine before signing the current assignment.
			if engine == transport.WireGuard {
				profile := cipherProfile(engine)
				profile.Prelude = []transport.Datagram{{Segments: []transport.Segment{{Bytes: []byte{1, 2, 3}}}}}
				digest, err := transport.ProfileDigest(profile)
				if err != nil {
					t.Fatal(err)
				}
				caps := transport.Capabilities{Version: 1, Engines: []transport.Capability{{Engine: engine, ProtocolVersion: "1", ProfileVersion: 1}}}
				f.snapshot.Node.Transport.Capabilities = caps
				f.snapshot.Node.Transport.Endpoints[0].ProfileDigest = digest
				plan := f.snapshot.Peers[0].Transport
				plan.PeerAdvertisement.Capabilities = caps
				plan.PeerAdvertisement.Endpoints[0].ProfileDigest = digest
				plan.Profiles[0].Profile = profile
				plan.Profiles[0].Digest = digest
				cursor := f.verified.Cursor()
				f.clock.advance(time.Millisecond, time.Millisecond)
				f.refresh(t, &cursor)
			}
			a := f.assignment(t)
			first, second := newCandidateSocketFixture(), newCandidateSocketFixture()
			// The fixed source address models the bind's exact endpoint/source fence.
			first.forward = func(p candidateDatagram) error {
				select {
				case <-second.done:
					return net.ErrClosed
				case second.reads <- candidateDatagram{p.raw, netip.MustParseAddrPort(a.offer.LocalEndpoint)}:
					return nil
				default:
					return nil
				}
			}
			second.forward = func(p candidateDatagram) error {
				select {
				case <-first.done:
					return net.ErrClosed
				case first.reads <- candidateDatagram{p.raw, netip.MustParseAddrPort(a.offer.PeerEndpoint)}:
					return nil
				default:
					return nil
				}
			}
			var appDeliveries atomic.Int32
			channel, err := buildAssignedCipher(t.Context(), f.verified, f.lease, "peer", "fixture-profile", f.profile.Keys.WireGuardPrivate, 576, f.clock, func([]byte) error { appDeliveries.Add(1); return nil }, func(uint16) (candidateSocket, error) { return first, nil })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(channel.Close)
			localIP, peerIP, err := a.addresses()
			if err != nil {
				t.Fatal(err)
			}
			var other *peerCipher
			var peerControls atomic.Int32
			peerPort, err := newPacketPort(t.Context(), 576, f.lease.Permissions, func(raw []byte) error {
				value, claimed, err := decodeControlPacket(raw, localIP, peerIP)
				if err != nil || !claimed || value.Kind != transport.Challenge {
					return ErrPacketIO
				}
				peerControls.Add(1)
				reply, err := controlPacket(peerIP, localIP, replyControl(value, transport.Echo))
				if err != nil {
					return err
				}
				return other.Queue(reply, true)
			})
			if err != nil {
				t.Fatal(err)
			}
			other, err = newPeerCipher(t.Context(), coordinator.Node{ID: a.peer.ID, PublicKey: a.peer.PublicKey, AssignedIP: peerIP.String()}, coordinator.Peer{ID: a.local.ID, PublicKey: a.local.PublicKey, AllowedIPs: []string{localIP.String() + "/32"}, ACLRestricted: true, Endpoint: a.offer.LocalEndpoint}, bytes.Repeat([]byte{84}, 32), a.offer.Profile, peerPort, &socketCipherFixtureBind{socket: second, port: 51820})
			if err != nil {
				_ = peerPort.Close()
				t.Fatal(err)
			}
			t.Cleanup(other.Close)
			const candidateID = "112233445566778899aabbccddeeff00"
			cycle, err := newProbeCycle(t.Context(), f.clock, f.profile.ID, 1, candidateID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cycle.cancel)
			probe, err := channel.BeginProbe(cycle, candidateID)
			if err != nil {
				t.Fatal(err)
			}
			if err := probe.Send(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-probe.done:
			case <-time.After(time.Second):
				t.Fatal("actual crypto challenge/echo failed")
			}
			if _, err := probe.Evidence(); err != nil {
				t.Fatal("actual own-nonce proof not eligible", err)
			}
			if peerControls.Load() != 1 || appDeliveries.Load() != 0 {
				t.Fatal("control reached application or replay counted", peerControls.Load(), appDeliveries.Load())
			}
			if cycle.wire.used == 0 || cycle.wire.used > transport.MaxProbeBytes {
				t.Fatal("actual handshake/control bytes were not bounded", cycle.wire.used)
			}
		})
	}
}
