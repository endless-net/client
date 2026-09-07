package client

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

// The Coordinator test process supplies its committed signed snapshots. Engines
// stay alive across acceptance and withdrawal; this test never edits a map.
func TestSharingLiveBackendEngines(t *testing.T) {
	if testing.Short() || os.Getenv("CLIENT_SHARING_LIVE_BACKEND") != "1" {
		t.Skip("requires isolated live backend peer in CI")
	}
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var engines [2]*WireGuardEngine
	tuns := [2]*tuntest.ChannelTUN{tuntest.NewChannelTUN(), tuntest.NewChannelTUN()}
	acceptedCount, withdrawnCount := 0, 0
	expiredCount, replacedCount := 0, 0
	var lastHashes [2]string
	for {
		var input struct {
			Maps          []clientapi.RegisterNodeResponse
			Trust         clientapi.SigningTrustBundle
			Accepted      bool
			WaitForExpiry bool
			SourceDeleted bool
			RelayCAPEM    []byte
		}
		if err := decoder.Decode(&input); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal("invalid backend snapshot handoff", err)
		}
		mapCount := 2
		if input.SourceDeleted {
			mapCount = 3 // retained source, updated recipient, newly registered source
		}
		if len(input.Maps) != mapCount || input.Maps[0].Node.ID != "machine" || input.Maps[1].Node.ID != "recipient-node" {
			t.Fatal("unexpected backend endpoint pair")
		}
		var relayTLS *tls.Config
		if len(input.RelayCAPEM) > 0 {
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(input.RelayCAPEM) {
				t.Fatal("invalid test Relay trust")
			}
			relayTLS = NewRelayTLSConfig(roots)
		}
		for i, networkMap := range input.Maps[:2] {
			if err := clientapi.VerifyNetworkMapSignatureWithTrustBundle(networkMap, input.Trust); err != nil {
				t.Fatal("backend signature rejected", err)
			}
			if networkMap.Node.PublicKey != testWireGuardEnginePublicKey(byte(i+1)) {
				t.Fatal("backend map does not bind the test engine key")
			}
			if input.WaitForExpiry || input.SourceDeleted && i == 0 {
				if engines[i] == nil || lastHashes[i] != networkMap.MapSignature.PayloadHash {
					t.Fatal("stale-map probe must retain the configured signed map")
				}
				continue
			}
			if engines[i] == nil {
				port := 0
				if relayTLS == nil {
					host, portText, err := net.SplitHostPort(networkMap.Node.Endpoint)
					if err != nil || host != "127.0.0.1" {
						t.Fatal("backend test endpoint must be loopback")
					}
					port, err = strconv.Atoi(portText)
					if err != nil || port < 1 || port > 65535 {
						t.Fatal("invalid backend test endpoint port")
					}
				}
				device := tuns[i]
				engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "sharing-live", ListenPort: port, RelayTLSConfig: relayTLS, RelayTimeout: 3 * time.Second, router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return device.TUN(), nil }})
				if err != nil {
					t.Fatal(err)
				}
				engines[i] = engine
				t.Cleanup(func() { _ = engine.Close() })
			}
			cfg := Config{NodeID: networkMap.Node.ID, NetworkID: networkMap.Network.ID, PrivateKey: testWireGuardEngineKey(byte(i + 1)), MapSigningTrust: &input.Trust, NodeCredential: "isolated-test"}
			if _, err := engines[i].Configure(t.Context(), cfg, networkMap); err != nil {
				t.Fatal("engine rejected committed backend map", err)
			}
			lastHashes[i] = networkMap.MapSignature.PayloadHash
			if relayTLS != nil && input.Accepted {
				if len(networkMap.Relays) != 1 || networkMap.RelayCredential == nil {
					t.Fatal("backend omitted Relay discovery or credential")
				}
				for _, peer := range networkMap.Peers {
					if peer.Endpoint != "" || len(peer.EndpointCandidates) != 0 {
						t.Fatal("Relay acceptance must have no direct peer endpoint")
					}
				}
				paths := engines[i].PathStatus()
				if len(paths) != 1 || paths[0].SelectedPath != "relay" {
					t.Fatal("engine did not select Relay", paths)
				}
			}
		}
		exchange := func(sender int, packet []byte, allowed bool) {
			t.Helper()
			started := time.Now()
			select {
			case tuns[sender].Outbound <- packet:
			case <-time.After(3 * time.Second):
				t.Fatal("backend engine stopped consuming packets")
			}
			timeout := 500 * time.Millisecond
			if allowed {
				// Engines connect sequentially. A keepalive handshake can reach
				// Relay before the other session exists; WireGuard retries after
				// five seconds plus jitter. Allow those real retries to finish.
				timeout = 15 * time.Second
			}
			select {
			case got := <-tuns[1-sender].Inbound:
				if !allowed || !bytes.Equal(got, packet) {
					t.Fatal("backend grant failed encrypted packet enforcement")
				}
				if elapsed := time.Since(started); elapsed > 5*time.Second {
					t.Logf("authorized backend packet arrived after transport recovery in %s", elapsed)
				}
			case <-time.After(timeout):
				if allowed {
					for i, engine := range engines {
						_, ready, relayErr := engine.RelayStatus()
						t.Logf("endpoint %d Relay ready=%t error=%v paths=%+v", i, ready, relayErr, engine.PathStatus())
					}
					t.Fatal("backend-authorized packet did not reach the other engine")
				}
			}
		}
		packet := func(sender int, flags byte) []byte {
			first, last := uint16(443), uint16(50123)
			if sender == 1 {
				first, last = last, first
			}
			p := applicationTCPPacket(input.Maps[sender].Node.AssignedIP, input.Maps[1-sender].Node.AssignedIP, first, last)
			p[33] = flags
			return p
		}
		datagram := func(sender int, protocol byte) []byte {
			p := sharingDatagram(protocol, sender == 0)
			copy(p[12:16], net.ParseIP(input.Maps[sender].Node.AssignedIP).To4())
			copy(p[16:20], net.ParseIP(input.Maps[1-sender].Node.AssignedIP).To4())
			p[10], p[11] = 0, 0
			binary.BigEndian.PutUint16(p[10:12], sharingChecksum(p[:20]))
			return p
		}
		if input.SourceDeleted {
			if input.Accepted || input.WaitForExpiry || acceptedCount != 3 || len(input.Maps[0].Network.SharePeerGrants) != 1 || len(input.Maps[1].Network.SharePeerGrants) != 0 {
				t.Fatal("source deletion must follow a distinct active sharing grant")
			}
			exchange(0, packet(0, 24), false)
			exchange(1, packet(1, 16), false)
			for _, protocol := range []byte{17, 1} {
				exchange(0, datagram(0, protocol), false)
				exchange(1, datagram(1, protocol), false)
			}
			verifyLiveReplacementEngine(t, input.Maps[2], input.Maps[1], input.Trust, relayTLS, tuns[1])
			replacedCount++
			t.Log("deleted source retained its old key/map but lost established traffic; replacement engine inherited no access")
		} else if input.WaitForExpiry {
			if !input.Accepted || acceptedCount == 0 || len(input.Maps[0].Network.SharePeerGrants) != 1 || len(input.Maps[1].Network.SharePeerGrants) != 1 {
				t.Fatal("expiry probe requires an established sharing pair")
			}
			deadline := input.Maps[0].Network.SharePeerGrants[0].ExpiresAt
			if !deadline.Equal(input.Maps[1].Network.SharePeerGrants[0].ExpiresAt) || time.Until(deadline) <= 0 || time.Until(deadline) > 2*time.Minute {
				t.Fatal("invalid live sharing expiry deadline")
			}
			select {
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			case <-time.After(time.Until(deadline.Add(50 * time.Millisecond))):
			}
			exchange(1, packet(1, 16), false)
			exchange(0, packet(0, 24), false)
			exchange(1, packet(1, 2), false)
			for _, protocol := range []byte{17, 1} {
				exchange(1, datagram(1, protocol), false)
				exchange(0, datagram(0, protocol), false)
			}
			expiredCount++
			t.Log("real backend lease expired with the same configured peers and signed maps")
		} else if input.Accepted {
			// A successful handshake and reply establish live transport before
			// the reverse-initiation denial probe.
			exchange(1, packet(1, 2), true)
			exchange(0, packet(0, 18), true)
			exchange(1, packet(1, 16), true)
			exchange(0, packet(0, 2), false)
			for _, protocol := range []byte{17, 1} {
				exchange(0, datagram(0, protocol), false)
				exchange(1, datagram(1, protocol), true)
				exchange(0, datagram(0, protocol), true)
			}
			t.Log("live backend TCP, UDP and ICMP grants passed encrypted request/reply enforcement")
			acceptedCount++
		} else {
			if acceptedCount == 0 {
				t.Fatal("withdrawal probe has no prior successful transport")
			}
			exchange(1, packet(1, 2), false)
			exchange(0, packet(0, 18), false)
			for _, protocol := range []byte{17, 1} {
				exchange(1, datagram(1, protocol), false)
				exchange(0, datagram(0, protocol), false)
			}
			withdrawnCount++
		}
		fmt.Println("ENDLESSNET_SHARING_ENGINE_OK")
	}
	wantAccepted := 2
	if os.Getenv("CLIENT_SHARING_EXPECT_REPLACEMENT") == "1" {
		wantAccepted = 3
		if replacedCount != 1 {
			t.Fatal("required backend source replacement probe was omitted")
		}
	}
	if acceptedCount != wantAccepted || withdrawnCount != 2 {
		t.Fatal("live user/group engine scenario was incomplete", acceptedCount, withdrawnCount)
	}
	if os.Getenv("CLIENT_SHARING_EXPECT_EXPIRY") == "1" && expiredCount != 1 {
		t.Fatal("required backend expiry probe was omitted")
	}
}

func verifyLiveReplacementEngine(t *testing.T, replacement, recipient clientapi.RegisterNodeResponse, trust clientapi.SigningTrustBundle, relayTLS *tls.Config, recipientTUN *tuntest.ChannelTUN) {
	t.Helper()
	if err := clientapi.VerifyNetworkMapSignatureWithTrustBundle(replacement, trust); err != nil {
		t.Fatal("replacement map signature rejected", err)
	}
	if replacement.Node.ID == "machine" || replacement.Node.PublicKey != testWireGuardEnginePublicKey(3) || len(replacement.Peers) != 0 || len(replacement.Network.SharePeerGrants) != 0 {
		t.Fatal("new source inherited the deleted source binding or sharing")
	}
	device := tuntest.NewChannelTUN()
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "sharing-new", RelayTLSConfig: relayTLS, router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return device.TUN(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	cfg := Config{NodeID: replacement.Node.ID, NetworkID: replacement.Network.ID, PrivateKey: testWireGuardEngineKey(3), MapSigningTrust: &trust, NodeCredential: "isolated-test"}
	if _, err := engine.Configure(t.Context(), cfg, replacement); err != nil {
		t.Fatal("replacement engine rejected backend map", err)
	}
	for _, protocol := range []byte{6, 17, 1} {
		packet := applicationTCPPacket(replacement.Node.AssignedIP, recipient.Node.AssignedIP, 443, 50123)
		if protocol != 6 {
			packet = sharingDatagram(protocol, true)
			copy(packet[12:16], net.ParseIP(replacement.Node.AssignedIP).To4())
			copy(packet[16:20], net.ParseIP(recipient.Node.AssignedIP).To4())
			packet[10], packet[11] = 0, 0
			binary.BigEndian.PutUint16(packet[10:12], sharingChecksum(packet[:20]))
		}
		select {
		case device.Outbound <- packet:
		case <-time.After(3 * time.Second):
			t.Fatal("replacement engine did not consume packet")
		}
		select {
		case <-recipientTUN.Inbound:
			t.Fatal("replacement inherited deleted source access")
		case <-time.After(500 * time.Millisecond):
		}
	}
}
