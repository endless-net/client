package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestNativeExitClearedObservationRechecksReleasedScopeAndOrdinaryRuntime(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	trusted, source, key := signedApplicationFixture(t, false)
	source.Node.PublicKey, source.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	source.Peers[0].Endpoint = "127.0.0.1:51820"
	source.Network.Applications, source.Network.DNS, source.Network.DNSConfig = nil, nil, nil
	source.Relays, source.STUNEndpoints = nil, nil
	resignApplicationMap(t, &source, key)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeID, cfg.NetworkID = source.Node.ID, source.Network.ID
		cfg.PrivateKey, cfg.NodeCredential = testWireGuardEngineKey(1), "synthetic"
		cfg.ControlPlaneURLs = []string{"https://control.test"}
		cfg.CachedMap, cfg.MapSigningTrust = &source, trusted.MapSigningTrust
		cfg.MapRevision, cfg.MapGlobalRevision = source.Network.Revision, source.Revision.Global
		cfg.WireGuardRouteTable = "51820"
		cfg.ExitSelection = &ClientExitSelection{ID: "previous", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	probe := tuntest.NewChannelTUN().TUN()
	name, err := probe.Name()
	if err != nil {
		t.Fatal(err)
	}
	_ = probe.Close()
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: name, router: &testWireGuardEngineRouter{},
		tunFactory: func(string, int) (tun.Device, error) { return tuntest.NewChannelTUN().TUN(), nil }, setSocketMark: func(*net.UDPConn, uint32) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	routes, rules, nftBad := `[]`, `[]`, false
	mutations, reads := 0, 0
	native := exitGuardReadbackRunner(t, name, 51820, func(_ context.Context, input, command string, args ...string) ([]byte, error) {
		if command == "nft" {
			mutations++
			return nil, nil
		}
		if slices.Contains(args, "rule") {
			return []byte(rules), nil
		}
		return []byte(routes), nil
	})
	run := func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
		if input == "" {
			reads++
		}
		if command == "nft" && input == "" && nftBad {
			return nil, errors.New("sensitive native error")
		}
		return native(ctx, input, command, args...)
	}
	n := &nativeExitExecutor{engine: engine, createGuard: func(iface, _ string) (*linuxExitGuard, error) { return newLinuxExitGuard(iface, 51820, run) }}
	executor := clientRPCExitExecutor{InterfaceName: name, Lock: &sync.Mutex{}, Apply: n.apply, Release: n.release, Contain: n.contain}
	if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
		t.Fatal(err)
	}
	stale := m.store.Read()
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	persisted := reopenRPCStoreFromDisk(t, m.store).Read()
	if persisted.RPCState.ExitCleared == nil || persisted.RPCState.ExitCleared.Protection == nil || persisted.RPCState.ExitProtection != nil || persisted.RPCState.ExitChange != nil {
		t.Fatal("clear did not atomically persist observation scope")
	}
	before := mutations
	for _, ordinary := range []bool{false, true} {
		if ordinary {
			result, err := engine.Configure(t.Context(), cfg, *cfg.CachedMap)
			if err != nil || !result.OK {
				t.Fatal("ordinary runtime failed to start after clear", err)
			}
			routes = fmt.Sprintf(`[{"dst":"100.64.0.0/10","dev":%q,"table":"51820"},{"dst":"default","dev":"eth0","table":"254"}]`, name)
		}
		oldReads := reads
		status, err := n.observe(t.Context(), cfg)
		if err != nil || status == nil || !exitAppliedResultMatches(&clientRPCExitChange{ProfileID: profile.ProfileId}, status) || reads <= oldReads || mutations != before {
			t.Fatal("clear was not freshly observed without mutations", ordinary, err)
		}
	}
	if _, err := n.observe(t.Context(), stale); err == nil {
		t.Fatal("pre-clear snapshot accepted")
	}
	pruned := clonePersistentConfig(cfg)
	pruned.RPCState.Operations = nil
	if _, err := n.observe(t.Context(), pruned); err != nil {
		t.Fatal("receipt depended on retained terminal operation after commit", err)
	}
	restarted := &nativeExitExecutor{engine: engine, createGuard: n.createGuard}
	if _, err := restarted.observe(t.Context(), pruned); err != nil {
		t.Fatal("restarted observer could not verify durable scope without operation records", err)
	}
	withoutScope := clonePersistentConfig(cfg)
	withoutScope.RPCState.ExitCleared = nil
	if _, err := restarted.observe(t.Context(), withoutScope); err == nil {
		t.Fatal("missing durable scope fabricated clear evidence")
	}
	rotated := clonePersistentConfig(cfg)
	rotated.NodeCredential = "rotated-synthetic"
	if _, err := restarted.observe(t.Context(), rotated); err != nil {
		t.Fatal("credential rotation invalidated read-only artifact address", err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.NodeID = "replacement" },
		func(c *Config) { c.NetworkID = "replacement" },
		func(c *Config) { c.RPCState.ActiveProfileID = "replacement" },
		func(c *Config) { c.WireGuardRouteTable = "51999" },
		func(c *Config) { c.ExitSelection = &ClientExitSelection{ID: "replacement"} },
	} {
		changed := clonePersistentConfig(cfg)
		change(&changed)
		if _, err := n.observe(t.Context(), changed); err == nil {
			t.Fatal("replacement identity or selection retained cleared observation")
		}
	}
	for _, fault := range []string{"nft", "routes", "rules", "uapi"} {
		savedRoutes, savedRules := routes, rules
		switch fault {
		case "nft":
			nftBad = true
		case "routes":
			routes = fmt.Sprintf(`[{"dst":"default","dev":%q,"table":"51820"}]`, name)
		case "rules":
			rules = `[{"priority":100,"src":"all","table":"51820"}]`
		case "uapi":
			if err := engine.device.IpcSet("fwmark=51820\n"); err != nil {
				t.Fatal(err)
			}
		}
		if status, err := n.observe(t.Context(), cfg); err == nil || status != nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("changed native state retained cleared status or disclosed output", fault, err)
		}
		routes, rules, nftBad = savedRoutes, savedRules, false
		if fault == "uapi" {
			if err := engine.device.IpcSet("fwmark=0\n"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNativeExitClearedDefaultsRejectOwnedAndAmbiguousRoutes(t *testing.T) {
	for _, raw := range []string{
		`[]`,
		`[{"dst":"100.64.0.0/10","dev":"exit0","table":"51820"}]`,
		`[{"dst":"default","dev":"eth0"},{"dst":"::/0","dev":"eth1","table":"254"}]`,
		`[{"dst":"default","dev":"eth0","table":254}]`,
		`[{"dst":"default","dev":"eth0","table":"main"}]`,
	} {
		if !nativeExitDefaultsAbsent([]byte(raw), 51820, "exit0") {
			t.Fatal("ordinary routes blocked clear evidence", raw)
		}
	}
	for _, raw := range []string{
		`null`, `[] true`,
		`[{"dst":"default","table":"51820","dev":"eth0"}]`,
		`[{"dst":"default","table":51820,"dev":"eth0"}]`,
		`[{"dst":"default","table":51820.0,"dev":"eth0"}]`,
		`[{"dst":"default","table":null,"dev":"eth0"}]`,
		`[{"dst":"::/0","dev":"exit0"}]`,
		`[{"dst":"0.0.0.0/0","dev":"exit0","table":"100"}]`,
		`[{"dst":"default","table":"254","table":"51820"}]`,
		`[{"dst":"default","nhid":5}]`,
		`[{"dst":"default","nexthops":[{"dev":"exit0"}]}]`,
		`[{"dst":"default","multipath":[{"dev":"exit0"}]}]`,
	} {
		if nativeExitDefaultsAbsent([]byte(raw), 51820, "exit0") {
			t.Fatal("owned or ambiguous default was accepted", raw)
		}
	}
}

func TestNativeExitClearedUAPIRequiresOrdinaryActualState(t *testing.T) {
	expected, _ := nativeExitUAPIFixture(t, api.ExitFamilyDualStack)
	expected = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(expected, "fwmark=51999", "fwmark=0"), "allowed_ip=0.0.0.0/0\n", ""), "allowed_ip=::/0\n", "")
	actual := strings.Replace(expected, "fwmark=0\n", "", 1)
	if err := nativeExitClearedUAPI(expected, actual, nativeExitUAPITestLocalPublic(t)); err != nil {
		t.Fatal("native omitted zero mark was rejected", err)
	}
	for _, bad := range []string{actual + "allowed_ip=0.0.0.0/0\n", actual + "allowed_ip=::/0\n", "fwmark=51820\n" + actual, "fwmark=0\nfwmark=0\n" + actual, strings.Replace(actual, "allowed_ip=100.64.0.2/32", "allowed_ip=100.64.0.2/24", 1)} {
		if nativeExitClearedUAPI(expected, bad, nativeExitUAPITestLocalPublic(t)) == nil {
			t.Fatal("unexpected actual WireGuard state accepted")
		}
	}
}
