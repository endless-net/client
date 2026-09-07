package client

import (
	"bytes"
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
	for {
		var input struct {
			Maps     []clientapi.RegisterNodeResponse
			Trust    clientapi.SigningTrustBundle
			Accepted bool
		}
		if err := decoder.Decode(&input); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal("invalid backend snapshot handoff", err)
		}
		if len(input.Maps) != 2 || input.Maps[0].Node.ID != "machine" || input.Maps[1].Node.ID != "recipient-node" {
			t.Fatal("unexpected backend endpoint pair")
		}
		for i, networkMap := range input.Maps {
			if err := clientapi.VerifyNetworkMapSignatureWithTrustBundle(networkMap, input.Trust); err != nil {
				t.Fatal("backend signature rejected", err)
			}
			if networkMap.Node.PublicKey != testWireGuardEnginePublicKey(byte(i+1)) {
				t.Fatal("backend map does not bind the test engine key")
			}
			if engines[i] == nil {
				host, portText, err := net.SplitHostPort(networkMap.Node.Endpoint)
				if err != nil || host != "127.0.0.1" {
					t.Fatal("backend test endpoint must be loopback")
				}
				port, err := strconv.Atoi(portText)
				if err != nil || port < 1 || port > 65535 {
					t.Fatal("invalid backend test endpoint port")
				}
				device := tuns[i]
				engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "sharing-live", ListenPort: port, router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return device.TUN(), nil }})
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
		}
		exchange := func(sender int, packet []byte, allowed bool) {
			t.Helper()
			select {
			case tuns[sender].Outbound <- packet:
			case <-time.After(3 * time.Second):
				t.Fatal("backend engine stopped consuming packets")
			}
			timeout := 500 * time.Millisecond
			if allowed {
				timeout = 5 * time.Second
			}
			select {
			case got := <-tuns[1-sender].Inbound:
				if !allowed || !bytes.Equal(got, packet) {
					t.Fatal("backend grant failed encrypted packet enforcement")
				}
			case <-time.After(timeout):
				if allowed {
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
		if input.Accepted {
			// A successful handshake and reply establish live transport before
			// the reverse-initiation denial probe.
			exchange(1, packet(1, 2), true)
			exchange(0, packet(0, 18), true)
			exchange(1, packet(1, 16), true)
			exchange(0, packet(0, 2), false)
			acceptedCount++
		} else {
			if acceptedCount == 0 {
				t.Fatal("withdrawal probe has no prior successful transport")
			}
			exchange(1, packet(1, 2), false)
			exchange(0, packet(0, 18), false)
			withdrawnCount++
		}
		fmt.Println("ENDLESSNET_SHARING_ENGINE_OK")
	}
	if acceptedCount != 2 || withdrawnCount != 2 {
		t.Fatal("live user/group engine scenario was incomplete", acceptedCount, withdrawnCount)
	}
}
