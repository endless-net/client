package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testrelay"
	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

// Component regression: the background monitor must reconnect without a new
// Configure call or map revision. Native traffic remains covered in CI.
func TestWireGuardRelayRecoversWithoutMapChange(t *testing.T) {
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	server := testrelay.New(t, "network", "client", "peer", peer.LocalAddr().String(), nil)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(server.CertificatePEM) {
		t.Fatal("invalid reference Relay CA")
	}
	fakeTUN := tuntest.NewChannelTUN()
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet", RelayTimeout: 100 * time.Millisecond,
		RelayDirectRetry: 100 * time.Millisecond,
		RelayTLSConfig:   &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots},
		tunFactory:       func(string, int) (tun.Device, error) { return fakeTUN.TUN(), nil },
		router:           &testWireGuardEngineRouter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	m := api.RegisterNodeResponse{
		Network: api.Network{ID: "network", Name: "network", CIDR: "198.18.94.0/24", Revision: 1},
		Node:    api.Node{ID: "client", NetworkID: "network", Hostname: "client", PublicKey: testWireGuardEnginePublicKey(1), AssignedIP: "198.18.94.1"},
		Peers:   []api.Peer{{ID: "peer", NetworkID: "network", Hostname: "peer", PublicKey: testWireGuardEnginePublicKey(2), AllowedIPs: []string{"198.18.94.20/32"}}},
		Relays:  []relay.Endpoint{server.Endpoint}, RelayCredential: &server.Credential,
	}
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: testWireGuardEngineKey(1)}, m); err != nil {
		t.Fatal("initial engine configuration failed")
	}
	await := func(condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("engine Relay recovery condition was not reached")
	}
	await(func() bool { _, ok, _ := engine.RelayStatus(); return ok })
	before, _, _ := server.Counts()
	server.SetUnavailable(true)
	await(func() bool { _, ok, _ := engine.RelayStatus(); return !ok })
	server.SetUnavailable(false)
	await(func() bool {
		auth, _, _ := server.Counts()
		status, ok, err := engine.RelayStatus()
		inspection := engine.Inspection()
		return auth > before && ok && err == nil && len(inspection.Peers) == 1 && inspection.Peers[0].Endpoint == status.PeerEndpoints["peer"]
	})
}
