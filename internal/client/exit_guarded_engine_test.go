package client

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestGuardedExitEngineOrdersProtectionAndRetainsItOnFailure(t *testing.T) {
	for _, scenario := range []string{"new", "replace_ordinary", "route_failure", "guard_failure", "wrong_interface"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, source, key := signedApplicationFixture(t, false)
			cfg.PrivateKey = testWireGuardEngineKey(1)
			source.Node.PublicKey, source.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
			source.Network.Applications, source.Network.DNS, source.Network.DNSConfig = nil, nil, nil
			source.Relays, source.STUNEndpoints = nil, nil
			source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: source.Peers[0].ID, PublicKey: source.Peers[0].PublicKey}
			source.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &source, key)
			selection := &ClientExitSelection{ID: "exit", NetworkID: cfg.NetworkID, NodeID: cfg.NodeID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
			probeTUN := tuntest.NewChannelTUN().TUN()
			name, err := probeTUN.Name()
			if err != nil {
				t.Fatal(err)
			}
			_ = probeTUN.Close()
			var protected, opened atomic.Bool
			var created atomic.Int32
			guarded := false
			router := &testWireGuardEngineRouter{}
			engine, err := NewWireGuardEngine(WireGuardEngineOptions{
				Interface: "endlessnet", router: router,
				tunFactory: func(string, int) (tun.Device, error) {
					if guarded && !protected.Load() {
						t.Error("TUN creation preceded protection")
					}
					created.Add(1)
					return tuntest.NewChannelTUN().TUN(), nil
				},
				// Exercise the real userspace engine without privileged socket
				// marking. Kernel firewall/mark verification belongs to platform CI.
				setSocketMark: func(*net.UDPConn, uint32) error { return nil },
				stageHook: func(stage wireGuardEngineApplyStage) error {
					if guarded && (!protected.Load() || opened.Load()) {
						t.Error("engine stage ran outside containment")
					}
					if guarded && scenario == "route_failure" && stage == wireGuardEngineStageRoutes {
						return errors.New("injected route-stage failure")
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			if scenario == "replace_ordinary" {
				if _, err := engine.Configure(t.Context(), cfg, source); err != nil {
					t.Fatal(err)
				}
			}
			guarded = true
			guardName := name
			if scenario == "wrong_interface" {
				guardName = "other-tun"
			}
			guard, err := newLinuxExitGuard(guardName, 51820, func(_ context.Context, batch, _ string, _ ...string) ([]byte, error) {
				if strings.Contains(batch, "delete table") {
					t.Error("engine automatically released protection")
				}
				if scenario == "guard_failure" {
					return nil, errors.New("injected firewall failure")
				}
				protected.Store(true)
				open := strings.Contains(batch, "output oifname \""+guardName+"\" accept")
				opened.Store(open)
				if open && (!engine.configured || !engine.exitFilter.allows(aclTestPacket("8.8.8.8", 6, 443), false, time.Now())) {
					t.Error("TUN opened before engine/filter commit")
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.configureExit(t.Context(), cfg, source, selection, guard)
			if scenario != "new" && scenario != "replace_ordinary" {
				if err == nil || result.OK || opened.Load() || engine.exitFilter.allows(aclTestPacket("8.8.8.8", 6, 443), false, time.Now()) {
					t.Fatal("failed apply opened traffic or reported success", err)
				}
				if scenario == "guard_failure" && created.Load() != 0 {
					t.Fatal("failed firewall transaction reached runtime creation")
				}
				if scenario == "route_failure" && (router.down == 0 || engine.device != nil) {
					t.Fatal("failed guarded apply retained/recreated a live runtime")
				}
				return
			}
			engine.mu.Lock()
			hasDefault := strings.Contains(engine.uapi, "allowed_ip=0.0.0.0/0")
			engine.mu.Unlock()
			if err != nil || !result.OK || !opened.Load() || !hasDefault {
				t.Fatal("protected engine did not apply explicit selection", err, result)
			}
			if scenario == "replace_ordinary" && created.Load() != 2 {
				t.Fatal("ordinary TUN was not replaced to attach exit enforcement")
			}
			// A changed authority cannot leave the previously opened TUN usable.
			cfg.ExitSelection = cloneExitSelection(selection)
			source.Network.ClientPolicy.ExitNodes[0].ExpiresAt = time.Now().Add(-time.Second)
			resignApplicationMap(t, &source, key)
			if _, err := engine.Configure(t.Context(), cfg, source); err == nil || opened.Load() || engine.exitFilter.allows(aclTestPacket("8.8.8.8", 6, 443), false, time.Now()) {
				t.Fatal("revoked exit authority retained an open path", err)
			}
		})
	}
}
