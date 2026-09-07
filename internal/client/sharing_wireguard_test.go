package client

import (
	"bytes"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestSharingEncryptedWireGuardDirectionAndWithdrawal(t *testing.T) {
	if testing.Short() {
		t.Skip("encrypted networking runs in CI")
	}
	base, _, signingKey := signedApplicationFixture(t, false)
	now := time.Now()
	maps := []clientapi.RegisterNodeResponse{sharingFilterFixture(now, false), sharingFilterFixture(now, true)}
	engines := make([]*WireGuardEngine, 2)
	tuns := []*tuntest.ChannelTUN{tuntest.NewChannelTUN(), tuntest.NewChannelTUN()}
	configs := make([]Config, 2)
	for i := range maps {
		m := &maps[i]
		m.Network.Name, m.Network.Revision = "sharing", 1
		m.Revision.Network = 1
		m.Node.Hostname = m.Node.ID
		m.Peers[0].Hostname = m.Peers[0].ID
		m.Node.PublicKey = testWireGuardEnginePublicKey(byte(i + 1))
		m.Peers[0].PublicKey = testWireGuardEnginePublicKey(byte(2 - i))
		m.Network.SharePeerGrants[0].RecipientPublicKey = testWireGuardEnginePublicKey(1)
		m.Network.SharePeerGrants[0].SourcePublicKey = testWireGuardEnginePublicKey(2)
		configs[i] = base
		configs[i].NodeID, configs[i].NetworkID, configs[i].PrivateKey = m.Node.ID, m.Network.ID, testWireGuardEngineKey(byte(i+1))
		device := tuns[i]
		engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "sharing-test", router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return device.TUN(), nil }})
		if err != nil {
			t.Fatal(err)
		}
		engines[i] = engine
		t.Cleanup(func() { _ = engine.Close() })
		resignApplicationMap(t, m, signingKey)
		if _, err := engine.Configure(t.Context(), configs[i], *m); err != nil {
			t.Fatal(err)
		}
	}
	for i := range maps {
		endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(engines[1-i].Inspection().ListenPort))
		maps[i].Peers[0].Endpoint = endpoint
		maps[i].Peers[0].EndpointCandidates = []string{endpoint}
		maps[i].Network.Revision++
		maps[i].Revision.Network++
		resignApplicationMap(t, &maps[i], signingKey)
		if _, err := engines[i].Configure(t.Context(), configs[i], maps[i]); err != nil {
			t.Fatal(err)
		}
	}
	exchange := func(sender int, packet []byte, allowed bool) {
		t.Helper()
		select {
		case tuns[sender].Outbound <- packet:
		case <-time.After(3 * time.Second):
			t.Fatal("TUN did not consume outgoing packet")
		}
		timeout := 500 * time.Millisecond
		if allowed {
			timeout = 5 * time.Second
		}
		select {
		case got := <-tuns[1-sender].Inbound:
			if !allowed {
				t.Fatal("forbidden packet crossed encrypted tunnel")
			}
			if !bytes.Equal(got, packet) {
				t.Fatal("decrypted packet differs from injected packet")
			}
		case <-time.After(timeout):
			if allowed {
				t.Fatal("authorized packet did not cross encrypted tunnel")
			}
		}
	}
	// Establish a real encrypted session before denial probes, so absence of a
	// handshake or disconnected transport cannot explain those denials.
	exchange(0, shareTCP(false, 2), true)
	exchange(1, shareTCP(true, 18), true)
	exchange(0, shareTCP(false, 16), true)
	exchange(1, shareTCP(true, 24), true)
	for _, protocol := range []byte{17, 1} {
		exchange(1, sharingDatagram(protocol, true), false)
		exchange(0, sharingDatagram(protocol, false), true)
		exchange(1, sharingDatagram(protocol, true), true)
	}
	// Expiry must revoke packets while peers, keys and encrypted transport stay
	// configured. A missing WireGuard peer cannot explain these denials.
	leaseDeadline := time.Now().Add(2 * time.Second)
	for i := range maps {
		maps[i].Network.SharePeerGrants[0].ExpiresAt = leaseDeadline
		maps[i].Network.Revision++
		maps[i].Revision.Network++
		resignApplicationMap(t, &maps[i], signingKey)
		if _, err := engines[i].Configure(t.Context(), configs[i], maps[i]); err != nil {
			t.Fatal(err)
		}
	}
	exchange(0, shareTCP(false, 24), true)
	time.Sleep(time.Until(leaseDeadline.Add(50 * time.Millisecond)))
	exchange(0, shareTCP(false, 24), false)
	exchange(1, shareTCP(true, 24), false)
	for _, protocol := range []byte{17, 1} {
		exchange(0, sharingDatagram(protocol, false), false)
		exchange(1, sharingDatagram(protocol, true), false)
	}
	// A fresh signed lease can establish a new flow on the same live transport.
	for i := range maps {
		maps[i].Network.SharePeerGrants[0].ExpiresAt = time.Now().Add(time.Minute)
		maps[i].Network.Revision++
		maps[i].Revision.Network++
		resignApplicationMap(t, &maps[i], signingKey)
		if _, err := engines[i].Configure(t.Context(), configs[i], maps[i]); err != nil {
			t.Fatal(err)
		}
	}
	exchange(0, shareTCP(false, 2), true)
	exchange(1, shareTCP(true, 18), true)
	exchange(0, shareTCP(false, 16), true)
	exchange(1, shareTCP(true, 2), false)
	for _, protocol := range []byte{17, 1} {
		exchange(1, sharingDatagram(protocol, true), false)
		exchange(0, sharingDatagram(protocol, false), true)
		exchange(1, sharingDatagram(protocol, true), true)
	}
	wrongPort := shareTCP(false, 2)
	binary.BigEndian.PutUint16(wrongPort[22:24], 22)
	exchange(0, wrongPort, false)
	exchange(1, shareTCP(true, 24), true)
	// A signed withdrawal must close even a previously established flow.
	for i := range maps {
		maps[i].Network.SharePeerGrants, maps[i].Peers = nil, nil
		maps[i].Network.Revision++
		maps[i].Revision.Network++
		resignApplicationMap(t, &maps[i], signingKey)
		if _, err := engines[i].Configure(t.Context(), configs[i], maps[i]); err != nil {
			t.Fatal(err)
		}
	}
	exchange(0, shareTCP(false, 24), false)
	exchange(1, shareTCP(true, 24), false)
	for _, protocol := range []byte{17, 1} {
		exchange(0, sharingDatagram(protocol, false), false)
		exchange(1, sharingDatagram(protocol, true), false)
	}
}

// Valid IPv4 UDP (zero UDP checksum is permitted) or ICMP echo packet.
func sharingDatagram(protocol byte, reply bool) []byte {
	packet := make([]byte, 28)
	packet[0], packet[8], packet[9] = 0x45, 64, protocol
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], []byte{100, 64, 0, 1})
	copy(packet[16:20], []byte{100, 65, 0, 1})
	if reply {
		copy(packet[12:16], []byte{100, 65, 0, 1})
		copy(packet[16:20], []byte{100, 64, 0, 1})
	}
	if protocol == 17 {
		source, destination := uint16(50000), uint16(53)
		if reply {
			source, destination = destination, source
		}
		binary.BigEndian.PutUint16(packet[20:22], source)
		binary.BigEndian.PutUint16(packet[22:24], destination)
		binary.BigEndian.PutUint16(packet[24:26], 8)
	} else {
		if !reply {
			packet[20] = 8
		}
		binary.BigEndian.PutUint16(packet[24:26], 9)
		binary.BigEndian.PutUint16(packet[26:28], 1)
		binary.BigEndian.PutUint16(packet[22:24], sharingChecksum(packet[20:]))
	}
	binary.BigEndian.PutUint16(packet[10:12], sharingChecksum(packet[:20]))
	return packet
}

func sharingChecksum(raw []byte) uint16 {
	var sum uint32
	for i := 0; i < len(raw); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(raw[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
