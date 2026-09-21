package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func TestNativeExitClearUsesDurableCheckpointAndObservedRelease(t *testing.T) {
	for _, scenario := range []string{"success", "release_ambiguous", "cancel_after_release", "routes_unobserved", "foreign_live"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NetworkID = "network"
				cfg.ExitSelection = &ClientExitSelection{ID: "previous", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet"})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "foreign_live" {
				engine.configured = true
				engine.exitConfig = Config{NodeID: "another-node"}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			releases, mutations := 0, 0
			runner := exitGuardReadbackRunner(t, "endlessnet", 51820, func(_ context.Context, input, name string, args ...string) ([]byte, error) {
				if name == "ip" {
					if scenario == "routes_unobserved" {
						return nil, errors.New("private native output")
					}
					return []byte(`[]`), nil
				}
				mutations++
				if strings.Contains(input, "delete table") && !strings.Contains(input, "policy drop;") {
					releases++
					stored := reopenRPCStoreFromDisk(t, m.store).Read()
					if stored.RPCState.ExitChange == nil || !stored.RPCState.ExitChange.Releasing || stored.RPCState.ExitProtection == nil {
						t.Error("release preceded durable checkpoint")
					}
					if scenario == "release_ambiguous" {
						return nil, errors.New("lost native reply")
					}
					if scenario == "cancel_after_release" {
						cancel()
					}
				}
				return nil, nil
			})
			executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, func(name, table string) (*linuxExitGuard, error) {
				if name != "endlessnet" {
					t.Fatal("scope changed", name)
				}
				return newLinuxExitGuard(name, 51820, runner)
			})
			if err != nil {
				t.Fatal(err)
			}
			err = m.reconcileExitChange(ctx, executor)
			cfg := m.store.Read()
			if scenario == "success" {
				result, getErr := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
				if err != nil || getErr != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.ExitSelection != nil || cfg.RPCState.ExitProtection != nil || engine.exitGuard != nil || releases != 1 {
					t.Fatal("clear did not finish verified release", err, getErr, result)
				}
			} else {
				if cfg.RPCState.ExitProtection == nil || cfg.RPCState.ExitChange == nil || cfg.ExitSelection == nil {
					t.Fatal("failed native clear lost durable ownership")
				}
				if scenario == "foreign_live" {
					if mutations != 0 || !engine.configured {
						t.Fatal("clear changed another live runtime")
					}
				} else if engine.exitGuard == nil {
					t.Fatal("failed clear discarded guard")
				}
				if scenario == "cancel_after_release" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled release lost cancellation", err)
				}
			}
		})
	}
}

func TestNativeExitSelectionAndObservationRequireRealEngineEvidence(t *testing.T) {
	for _, family := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		t.Run(string(family), func(t *testing.T) { testNativeExitSelectionObservation(t, family) })
	}
}

func testNativeExitSelectionObservation(t *testing.T, family api.ExitFamilyMode) {
	m, owner, profile := rpcConnectFixture(t)
	trusted, source, key := signedApplicationFixture(t, false)
	source.Node.PublicKey, source.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	source.Peers[0].Endpoint = "127.0.0.1:51820"
	source.Network.Applications, source.Network.DNS, source.Network.DNSConfig = nil, nil, nil
	source.Relays, source.STUNEndpoints = nil, nil
	if family != api.ExitFamilyIPv6Only {
		source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "0.0.0.0/0")
	}
	if family != api.ExitFamilyIPv4Only {
		source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "::/0")
	}
	host := api.ServiceHost{NodeID: source.Peers[0].ID, PublicKey: source.Peers[0].PublicKey}
	source.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{family}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
	resignApplicationMap(t, &source, key)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeID, cfg.NetworkID = source.Node.ID, source.Network.ID
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.NodeCredential = "synthetic"
		cfg.ControlPlaneURLs = []string{"https://control.test"}
		cfg.PrivateKey = testWireGuardEngineKey(1)
		cfg.CachedMap, cfg.MapSigningTrust = &source, trusted.MapSigningTrust
		cfg.MapRevision, cfg.MapGlobalRevision = source.Network.Revision, source.Revision.Global
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
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: name, router: &testWireGuardEngineRouter{}, underlayDNSCapture: testUnderlayDNSCapture,
		tunFactory: func(string, int) (tun.Device, error) { return tuntest.NewChannelTUN().TUN(), nil }, setSocketMark: func(*net.UDPConn, uint32) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	tamper := false
	native := exitGuardReadbackRunner(t, name, 51820, func(_ context.Context, _ string, command string, args ...string) ([]byte, error) {
		if command != "ip" {
			return nil, nil
		}
		if (family == api.ExitFamilyIPv4Only && slices.Contains(args, "-6")) || (family == api.ExitFamilyIPv6Only && slices.Contains(args, "-4")) {
			return []byte(`[]`), nil
		}
		if slices.Contains(args, "rule") {
			return []byte(strings.ReplaceAll(string(exitAppliedRuleFixture(args[len(args)-1])), "51999", "51820")), nil
		}
		return []byte(fmt.Sprintf(`[{"dst":"default","dev":%q,"flags":[]}]`, name)), nil
	})
	runner := func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
		if tamper && command == "nft" && input == "" {
			return []byte(`{"nftables":[]}`), nil
		}
		return native(ctx, input, command, args...)
	}
	executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, func(scope, table string) (*linuxExitGuard, error) { return newLinuxExitGuard(scope, 51820, runner) })
	if err != nil {
		t.Fatal(err)
	}
	mode := map[api.ExitFamilyMode]ipc.ExitFamilyMode{api.ExitFamilyIPv4Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, api.ExitFamilyIPv6Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, api.ExitFamilyDualStack: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[family]
	op, err := m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: mode, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, executor.Modes)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("native selection did not commit", err, result)
	}
	cfg := m.store.Read()
	executor.Lock.Lock()
	maintenanceErr := executor.Maintain(t.Context(), cfg)
	status, err := executor.Observe(t.Context(), cfg)
	executor.Lock.Unlock()
	if maintenanceErr != nil {
		t.Fatal("healthy native enforcement was contained", maintenanceErr)
	}
	if err != nil || status.ProfileId != profile.ProfileId || !exitAppliedResultMatches(&clientRPCExitChange{ProfileID: profile.ProfileId, Requested: cfg.ExitSelection}, status) {
		t.Fatal("complete native observation missing", err, status)
	}
	tamper = true
	if status, err := executor.Observe(t.Context(), cfg); err == nil || status != nil {
		t.Fatal("changed nft rules retained applied observation")
	}
	tamper = false
	engine.mu.Lock()
	engine.exitConfig.RPCState.ActiveProfileID = "another-profile"
	engine.mu.Unlock()
	if status, err := executor.Observe(t.Context(), cfg); err == nil || status != nil {
		t.Fatal("different runtime profile retained applied observation")
	}
	engine.mu.Lock()
	engine.exitConfig.RPCState.ActiveProfileID = profile.ProfileId
	engine.mu.Unlock()
	engine.exitFilter.withdraw()
	if status, err := executor.Observe(t.Context(), cfg); err == nil || status != nil {
		t.Fatal("withdrawn packet filter retained applied observation")
	}
	engine.exitFilter.mu.Lock()
	engine.exitFilter.closed = false
	engine.exitFilter.mu.Unlock()
	if !underlayDNSRequired(cfg, cfg.CachedMap) || engine.underlayLease == nil {
		t.Fatal("applied hostname scope has no lease")
	}
	engine.underlayLease.revoke()
	if status, err := executor.Observe(t.Context(), cfg); err == nil || status != nil {
		t.Fatal("revoked DNS source retained applied observation")
	}
	executor.Lock.Lock()
	maintenanceErr = executor.Maintain(t.Context(), cfg)
	executor.Lock.Unlock()
	if !errors.Is(maintenanceErr, errNativeExitMaintenance) || !engine.exitFilter.closed || engine.exitGuard == nil {
		t.Fatal("revoked DNS source was not contained", maintenanceErr)
	}
	if err := engine.exitGuard.ObserveContained(t.Context()); err != nil {
		t.Fatal("maintenance did not close native enforcement", err)
	}
}

func TestExitApplyReceivesCommittedRunningOperation(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{}, Release: releaseExitTestCallback,
		Apply: func(_ context.Context, id string, cfg Config, selection *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			called = true
			if id != op.Id {
				t.Fatal("changed operation identity")
			}
			if _, err := nativeExitOperation(cfg, id, selection, false); err != nil {
				t.Fatal("adapter input lacks committed RUNNING operation", err)
			}
			if _, err := nativeExitOperation(reopenRPCStoreFromDisk(t, m.store).Read(), id, selection, false); err != nil {
				t.Fatal("dispatch preceded durable RUNNING operation", err)
			}
			return preparedExitClearTestStatus(profile.ProfileId), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
		}}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil || !called {
		t.Fatal("committed dispatch did not reach adapter", err)
	}
}

func TestNativeExitRejectsMissingJournalBeforeNativeEffects(t *testing.T) {
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}
	creates := 0
	executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, func(string, string) (*linuxExitGuard, error) { creates++; return nil, errors.New("unexpected") })
	if err != nil {
		t.Fatal(err)
	}
	if status, _, err := executor.Apply(t.Context(), "unknown", Config{}, nil); err == nil || status != nil {
		t.Fatal("uncheckpointed clear admitted")
	}
	if status, _, err := executor.Release(t.Context(), "unknown", Config{}); err == nil || status != nil {
		t.Fatal("uncheckpointed release admitted")
	}
	if creates != 0 {
		t.Fatal("unbound operation touched native scope")
	}
}

func TestNativeExitFactoryRequiresPlatformAndOwnedEngine(t *testing.T) {
	if _, err := newNativeExitExecutor(nil, &sync.Mutex{}); err == nil {
		t.Fatal("missing engine accepted")
	}
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}
	executor, err := newNativeExitExecutor(engine, &sync.Mutex{})
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("unsupported platform advertised native executor")
		}
		return
	}
	if err != nil || executor.Apply == nil || executor.Contain == nil || executor.Release == nil || executor.Observe == nil || executor.Maintain == nil || executor.ResumeSaved == nil || len(executor.Modes) != 3 {
		t.Fatal("Linux factory did not provide complete callbacks", err)
	}
	if engine.exitGuard != nil || engine.configured {
		t.Fatal("factory changed native state")
	}
	if _, err := newNativeExitExecutor(engine, nil); err == nil {
		t.Fatal("missing effect lock accepted")
	}
}

func TestNativeExitContainmentPreservesOriginalOwnershipWithoutControlAuthority(t *testing.T) {
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}
	commands := 0
	runner := exitGuardReadbackRunner(t, "endlessnet", 51820, func(_ context.Context, _ string, name string, _ ...string) ([]byte, error) {
		commands++
		if name == "ip" {
			return []byte(`[]`), nil
		}
		return nil, nil
	})
	executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, func(name, table string) (*linuxExitGuard, error) {
		if name != "endlessnet" || table != "51820" {
			t.Fatal("cleanup replaced original scope")
		}
		return newLinuxExitGuard(name, 51820, runner)
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := clientRPCExitChange{OperationID: "new-clear", ProfileID: "original-profile", OwnerID: "owner", NodeID: "current-node", NetworkID: "current-network", RouteTable: "51999", Containing: true,
		Protection: &clientRPCExitProtection{OperationID: "old-selection", ProfileID: "original-profile", OwnerID: "owner", NodeID: "original-node", NetworkID: "original-network", InterfaceName: "endlessnet", RouteTable: "51820"}}
	proof, err := executor.Contain(t.Context(), plan)
	if err != nil || proof.OperationID != plan.OperationID || proof.NodeID != plan.NodeID || proof.NetworkID != plan.NetworkID || !proof.IPv4Blocked || !proof.IPv6Blocked || !proof.ExitRoutesRemoved {
		t.Fatal("original-scope containment was not observed", proof, err)
	}
	if engine.exitConfig.NodeID != "" || engine.exitSelection != nil || engine.underlayDNS != nil || engine.exitGuard == nil || commands == 0 {
		t.Fatal("clear ownership became control authority")
	}
	before := commands
	plan.Protection = cloneExitProtection(plan.Protection)
	plan.Protection.InterfaceName = "foreign0"
	if _, err := executor.Contain(t.Context(), plan); err == nil || commands != before {
		t.Fatal("foreign interface containment reached native effects")
	}
}
