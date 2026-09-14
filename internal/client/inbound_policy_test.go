package client

import (
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
	"google.golang.org/protobuf/proto"
)

func TestInboundPolicyResolutionAndExportRestriction(t *testing.T) {
	for _, locked := range []bool{false, true} {
		cfg, source, key := signedApplicationFixture(t, false)
		cfg.PrivateKey = testWireGuardEngineKey(1)
		source.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingAllowInbound, Source: api.ClientPolicyDevice, PolicyID: "inbound", Locked: locked, BooleanValue: proto.Bool(false)}}}
		resignApplicationMap(t, &source, key)
		cfg.NetworkPreferences = &ClientNetworkPreferences{AllowInbound: proto.Bool(true)}
		resolved, err := resolveNetworkAcceptance(cfg, source, time.Now())
		if err != nil || resolved.inbound == locked {
			t.Fatal("managed inbound intersection wrong", resolved, err)
		}
		cfg.NetworkPreferences.AllowInbound = nil
		resolved, err = resolveNetworkAcceptance(cfg, source, time.Now())
		if err != nil || resolved.inbound {
			t.Fatal("reset did not restore inbound policy", err)
		}
		if output, err := RenderWireGuardWithOptionsChecked(cfg, source, WireGuardRenderOptions{}); err == nil || !strings.Contains(err.Error(), "inbound restriction") || output != "" {
			t.Fatal("static export dropped inbound restriction", err)
		}
	}
}

func TestInboundEngineAppliesPolicyAndClearsIdentityFlows(t *testing.T) {
	cfg, source, key := signedApplicationFixture(t, false)
	cfg.PrivateKey = testWireGuardEngineKey(1)
	source.Node.PublicKey, source.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	source.Network.Applications[0].Sources[0].PublicKey = source.Node.PublicKey
	source.Network.Applications[0].Connectors[0].PublicKey = source.Peers[0].PublicKey
	source.Network.Applications[0].Routes[0].Connector.PublicKey = source.Peers[0].PublicKey
	resignApplicationMap(t, &source, key)
	cfg.NetworkPreferences = &ClientNetworkPreferences{AllowInbound: proto.Bool(false)}
	fakeTUN := tuntest.NewChannelTUN()
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet", router: &testWireGuardEngineRouter{}, tunFactory: func(string, int) (tun.Device, error) { return fakeTUN.TUN(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := engine.Configure(t.Context(), cfg, source); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if engine.inboundFilter == nil || engine.inboundFilter.allows(shareTCP(true, 2), true, now) {
		t.Fatal("engine did not install inbound restriction")
	}
	if !engine.inboundFilter.allows(inboundTestUDP(false, 53), false, now) || !engine.inboundFilter.allows(inboundTestUDP(true, 53), true, now) {
		t.Fatal("restricted engine lost outgoing responses")
	}
	engine.inboundFilter.suspend(false, "different-identity")
	if engine.inboundFilter.allows(inboundTestUDP(true, 53), true, now) {
		t.Fatal("identity transition retained flow permission")
	}
	cfg.NetworkPreferences.AllowInbound = proto.Bool(true)
	if _, err := engine.Configure(t.Context(), cfg, source); err != nil {
		t.Fatal(err)
	}
	if !engine.inboundFilter.allows(shareTCP(true, 2), true, now) {
		t.Fatal("successful engine apply did not enable inbound")
	}
}
