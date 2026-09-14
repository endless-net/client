package client

import (
	"strings"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestResourceEngineAppliesChangesAndRejectsStaticBypass(t *testing.T) {
	cfg, source, key := signedApplicationFixture(t, false)
	cfg.PrivateKey = testWireGuardEngineKey(1)
	source.Node.PublicKey, source.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	source.Network.Applications[0].Sources[0].PublicKey = source.Node.PublicKey
	source.Network.Applications[0].Connectors[0].PublicKey = source.Peers[0].PublicKey
	source.Network.Applications[0].Routes[0].Connector.PublicKey = source.Peers[0].PublicKey
	resignApplicationMap(t, &source, key)
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_APPLICATION, source.Network.Applications[0].ID)
	fakeTUN := tuntest.NewChannelTUN()
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet", router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return fakeTUN.TUN(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := engine.Configure(t.Context(), cfg, source); err != nil {
		t.Fatal(err)
	}
	packet := applicationTCPPacket("100.64.0.1", "10.1.2.3", 50000, 443)
	if !engine.resourceFilter.allows(packet, false, time.Now()) {
		t.Fatal("default resource choice denied")
	}
	cfg.ResourcePreferences = map[string]bool{id: false}
	result, err := engine.Configure(t.Context(), cfg, source)
	if err != nil || !result.Changed || engine.resourceFilter.allows(packet, false, time.Now()) {
		t.Fatal("resource-only change not applied/reported", result, err)
	}
	if output, err := RenderWireGuardWithOptionsChecked(cfg, source, WireGuardRenderOptions{}); err == nil || output != "" || !strings.Contains(err.Error(), "resource restrictions") {
		t.Fatal("static export bypassed resource restriction", err)
	}
	cfg.ResourcePreferences[id] = true
	if _, err := engine.Configure(t.Context(), cfg, source); err != nil {
		t.Fatal(err)
	}
	if !engine.resourceFilter.allows(packet, false, time.Now()) {
		t.Fatal("successful resource enable not committed")
	}
	if engine.resourceFilter.allows(packet, false, source.MapSignature.ExpiresAt) {
		t.Fatal("engine resource authority ignored expiry")
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	if engine.resourceFilter.allows(packet, false, time.Now()) {
		t.Fatal("engine shutdown retained resource access")
	}
}
