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
	exchange(1, shareTCP(true, 2), false)
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
}
