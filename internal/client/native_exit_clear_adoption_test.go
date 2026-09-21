package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestNativeExitClearAdoptsOnlyOwnedOrdinaryRuntime(t *testing.T) {
	for _, scenario := range []string{"owned", "foreign_owner_same_node", "foreign_profile_same_node", "contain_retry"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, network, key := signedApplicationFixture(t, false)
			network.Node.PublicKey, network.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
			network.Peers[0].Endpoint = "127.0.0.1:51820"
			network.Network.Applications, network.Network.DNS, network.Network.DNSConfig = nil, nil, nil
			network.Relays, network.STUNEndpoints = nil, nil
			resignApplicationMap(t, &network, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = network.Node.ID, network.Network.ID
				cfg.PrivateKey, cfg.NodeCredential = testWireGuardEngineKey(1), "synthetic"
				cfg.ControlPlaneURLs = []string{"https://control.test"}
				cfg.CachedMap, cfg.MapSigningTrust = &network, trusted.MapSigningTrust
				cfg.MapRevision, cfg.MapGlobalRevision = network.Network.Revision, network.Revision.Global
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			applied := clonePersistentConfig(cfg)
			if scenario == "foreign_owner_same_node" {
				applied.LocalOwnerID = "foreign-owner"
			}
			if scenario == "foreign_profile_same_node" {
				foreign := applied.RPCState.Profiles[profile.ProfileId]
				foreign.ID = "foreign-profile"
				applied.RPCState.Profiles[foreign.ID] = foreign
				applied.RPCState.ActiveProfileID = foreign.ID
			}
			probe := tuntest.NewChannelTUN().TUN()
			name, err := probe.Name()
			if err != nil {
				t.Fatal(err)
			}
			_ = probe.Close()
			router := &testWireGuardEngineRouter{}
			engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: name, router: router,
				tunFactory: func(string, int) (tun.Device, error) { return tuntest.NewChannelTUN().TUN(), nil }, setSocketMark: func(*net.UDPConn, uint32) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = engine.Close() })
			result, err := engine.Configure(t.Context(), applied, network)
			if err != nil || !result.OK || engine.exitGuard != nil || engine.exitSelection != nil {
				t.Fatal("ordinary runtime fixture was not configured", err)
			}
			identity := engine.runtimeIdentity
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			changed := clonePersistentConfig(applied)
			changed.LocalOwnerID = "failed-apply-owner"
			if _, err := engine.Configure(cancelled, changed, network); err == nil || engine.runtimeIdentity != identity {
				t.Fatal("failed apply replaced committed runtime ownership")
			}
			// Force runtime replacement and a route-stage failure, then verify the
			// recreated previous device also recovers its committed ownership.
			previousDevice := engine.device
			changed.WireGuardMTU = engine.opts.MTU + 16
			router.failNext = errors.New("route-stage failure")
			if _, err := engine.Configure(t.Context(), changed, network); err == nil || !engine.configured || engine.device == previousDevice || engine.runtimeIdentity != identity {
				t.Fatal("native rollback lost or replaced committed runtime ownership", err)
			}
			commands, failContain := 0, scenario == "contain_retry"
			runner := exitGuardReadbackRunner(t, name, 51820, func(_ context.Context, _ string, command string, _ ...string) ([]byte, error) {
				commands++
				if command == "nft" {
					if engine.exitFilter == nil || !engine.exitFilter.closed || engine.exitGuard == nil {
						t.Fatal("native effects preceded owned guard/filter adoption")
					}
					if failContain {
						failContain = false
						return nil, errors.New("ambiguous containment")
					}
				}
				return []byte(`[]`), nil
			})
			executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, func(iface, _ string) (*linuxExitGuard, error) { return newLinuxExitGuard(iface, 51820, runner) })
			if err != nil {
				t.Fatal(err)
			}
			op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if err := m.reconcileExitChange(t.Context(), executor); err != nil {
				t.Fatal(err)
			}
			if scenario == "foreign_owner_same_node" || scenario == "foreign_profile_same_node" {
				if commands != 0 || !engine.configured || engine.exitGuard != nil || engine.runtimeIdentity != identity {
					t.Fatal("clear adopted or stopped another owner's/profile's ordinary runtime")
				}
				return
			}
			if scenario == "contain_retry" {
				if engine.exitGuard == nil || m.store.Read().RPCState.ExitProtection == nil {
					t.Fatal("ambiguous adoption lost ownership")
				}
				if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
					t.Fatal("clear-only adoption acquired control authority")
				}
				if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ExitChange.NextAttemptAt = time.Time{}; return nil }); err != nil {
					t.Fatal(err)
				}
				if err := m.reconcileExitChange(t.Context(), executor); err != nil {
					t.Fatal(err)
				}
			}
			completed, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || completed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || engine.configured || engine.exitGuard != nil || engine.runtimeIdentity != (nativeExitRuntimeIdentity{}) || m.store.Read().RPCState.ExitProtection != nil || commands == 0 {
				t.Fatal("owned ordinary runtime clear did not complete verified cleanup", err, completed)
			}
		})
	}
}
