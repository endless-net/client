package client

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func nativeExitResumeFixture(t *testing.T) (*nativeExitExecutor, Config, *int) {
	t.Helper()
	cfg, m, key := signedApplicationFixture(t, false)
	cfg.PrivateKey = testWireGuardEngineKey(1)
	m.Node.PublicKey, m.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	m.Peers[0].Endpoint = "127.0.0.1:51820"
	m.Network.Applications, m.Network.DNS, m.Network.DNSConfig = nil, nil, nil
	m.Relays, m.STUNEndpoints = nil, nil
	m.Peers[0].AllowedIPs = append(m.Peers[0].AllowedIPs, "0.0.0.0/0")
	host := api.ServiceHost{NodeID: m.Peers[0].ID, PublicKey: m.Peers[0].PublicKey}
	m.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
	resignApplicationMap(t, &m, key)
	cfg.CachedMap = &m
	cfg.MapRevision = m.Network.Revision
	cfg.MapGlobalRevision = m.Revision.Global
	cfg.LocalOwnerID = "owner"
	cfg.ControlPlaneURLs = []string{"https://control.example"}
	cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
	cfg.ExitSelection = &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
	probe := tuntest.NewChannelTUN().TUN()
	name, err := probe.Name()
	if err != nil {
		t.Fatal(err)
	}
	_ = probe.Close()
	cfg.RPCState = &ClientRPCState{ActiveProfileID: "profile", Profiles: map[string]clientRPCProfile{"profile": {ID: "profile", ControlOrigin: "https://control.example"}}, ExitProtection: &clientRPCExitProtection{OperationID: "committed-select", ProfileID: "profile", OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: name}}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: name, router: &testWireGuardEngineRouter{}, underlayDNSCapture: testUnderlayDNSCapture, tunFactory: func(string, int) (tun.Device, error) { return tuntest.NewChannelTUN().TUN(), nil }, setSocketMark: func(*net.UDPConn, uint32) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	mutations := new(int)
	runner := exitGuardReadbackRunner(t, name, 51820, func(_ context.Context, _ string, command string, args ...string) ([]byte, error) {
		if command == "nft" {
			*mutations++
			return nil, nil
		}
		if slices.Contains(args, "-6") {
			return []byte(`[]`), nil
		}
		if slices.Contains(args, "rule") {
			return []byte(strings.ReplaceAll(string(exitAppliedRuleFixture(args[len(args)-1])), "51999", "51820")), nil
		}
		return []byte(fmt.Sprintf(`[{"dst":"default","dev":%q,"flags":[]}]`, name)), nil
	})
	guard, err := newLinuxExitGuard(name, 51820, runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.restoreClearProtection(t.Context(), guard); err != nil {
		t.Fatal(err)
	}
	return &nativeExitExecutor{engine: engine, createGuard: func(string, string) (*linuxExitGuard, error) { return guard, nil }}, cfg, mutations
}

func TestNativeExitResumeSavedSelectionAppliesStoppedProtectedRuntime(t *testing.T) {
	n, cfg, mutations := nativeExitResumeFixture(t)
	if n.engine.exitSelection != nil || n.engine.configured {
		t.Fatal("fixture is already applied")
	}
	status, err := n.resumeSaved(t.Context(), cfg)
	if err != nil || status == nil || status.GetEffectiveExitNodeId() != cfg.ExitSelection.ID || !n.engine.configured {
		t.Fatal("saved selection did not resume", err)
	}
	count := *mutations
	if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
		t.Fatal("current selection was not observed", err)
	}
	if *mutations != count {
		t.Fatal("already applied selection was reopened")
	}
	if cfg.RPCState.ExitChange != nil || cfg.RPCState.ExitProtection.OperationID != "committed-select" {
		t.Fatal("resume manufactured an operation")
	}
}

func TestNativeExitResumeRejectsDisconnectedStaleAndPendingContext(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected },
		func(c *Config) { c.RPCState.ActiveProfileID = "other" },
		func(c *Config) { c.ControlPlaneURLs = []string{"https://another.example"} },
		func(c *Config) { c.RPCState.ExitProtection.NodeID = "other" },
		func(c *Config) { c.RPCState.ExitProtection.InterfaceName = "other" },
		func(c *Config) { c.RPCState.ExitChange = &clientRPCExitChange{} },
		func(c *Config) { c.RPCState.DisconnectOperationID = "pending" },
		func(c *Config) { c.RPCState.ProfileSwitch = &clientRPCProfileSwitch{} },
		func(c *Config) { c.MapRevision++ },
		func(c *Config) { c.CachedMap.MapSignature.ExpiresAt = time.Now().Add(-time.Minute) },
	} {
		n, cfg, mutations := nativeExitResumeFixture(t)
		count := *mutations
		change(&cfg)
		if status, err := n.resumeSaved(t.Context(), cfg); err == nil || status != nil || *mutations != count || n.engine.configured {
			t.Fatal("invalid saved context acquired effects", err)
		}
	}
}

func TestNativeExitResumeFailedLiveObservationContainsWithoutReapply(t *testing.T) {
	n, cfg, mutations := nativeExitResumeFixture(t)
	if _, err := n.resumeSaved(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	n.engine.exitFilter.withdraw()
	count := *mutations
	if status, err := n.resumeSaved(t.Context(), cfg); err == nil || status != nil {
		t.Fatal("withdrawn runtime was silently reopened")
	}
	if *mutations != count+1 {
		t.Fatal("failure did not perform exactly one containment")
	}
	if err := n.engine.exitGuard.ObserveContained(t.Context()); err != nil {
		t.Fatal(err)
	}
}
