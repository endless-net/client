package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strconv"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	relay "github.com/endless-net/relay/protocol/v1"
)

// This uses the product TLS dataplane with fixture authorization. Management
// and Coordinator authorization must be verified by the backend acceptance run.
func sharingEncryptedRelayEndpoints(t *testing.T, maps []clientapi.RegisterNodeResponse, engines []*WireGuardEngine) []string {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	addr := freeTCPAddrForRelayPathTest(t)
	server := newRelayPathTestServer(t, addr, public, serverTLS)
	ctx, cancel := context.WithCancel(t.Context())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ListenAndServe(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("sharing Relay did not stop")
		}
	})
	waitTCPForRelayPathTest(t, addr)
	endpoints := make([]string, len(maps))
	for i, networkMap := range maps {
		credential, err := relay.Sign(private, networkMap.Network.ID, networkMap.Node.ID, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		ready := make(chan RelayDataplaneBridgeStatus, 1)
		done := make(chan error, 1)
		opts := RelayDataplaneBridgeOptions{NetworkMap: networkMap, Credential: *credential, Relays: []relay.Endpoint{{ID: "relay-1", Addr: addr, Protocol: "relay-v1-tls"}}, WireGuardListenAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(engines[i].Inspection().ListenPort)), TLSConfig: clientTLS, Timeout: time.Second, Ready: func(status RelayDataplaneBridgeStatus) { ready <- status }}
		go func() { done <- RunRelayDataplaneBridge(ctx, opts) }()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				t.Error("sharing Relay bridge did not stop")
			}
		})
		status := requireRelayBridgeReadyForTest(t, ready)
		endpoints[i] = status.PeerEndpoints[networkMap.Peers[0].ID]
		if endpoints[i] == "" {
			t.Fatal("sharing peer Relay endpoint missing")
		}
	}
	return endpoints
}
