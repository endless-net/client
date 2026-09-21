package client

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeExitClearRecoversOriginalInterfaceAndTable(t *testing.T) {
	for _, scenario := range []string{"restart", "release_checkpoint", "foreign_live"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NetworkID = "network"
				cfg.WireGuardRouteTable = "51820"
				cfg.ExitSelection = &ClientExitSelection{ID: "old-exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: "51821"}
				cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "old-select", ProfileID: profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "oldexit0", RouteTable: "51821"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			mutations, releases, commands := 0, 0, 0
			originalProtection := cloneExitProtection(m.store.Read().RPCState.ExitProtection)
			ambiguous := scenario == "release_checkpoint"
			run := exitGuardReadbackRunner(t, "oldexit0", 51821, func(_ context.Context, input, command string, args ...string) ([]byte, error) {
				if command == "ip" {
					return []byte(`[]`), nil
				}
				mutations++
				if strings.Contains(input, "delete table") && !strings.Contains(input, "policy drop;") {
					releases++
					cfg := reopenRPCStoreFromDisk(t, m.store).Read()
					if cfg.RPCState.ExitChange == nil || !cfg.RPCState.ExitChange.Releasing || cfg.RPCState.ExitProtection.InterfaceName != "oldexit0" || cfg.RPCState.ExitProtection.RouteTable != "51821" {
						t.Error("release lost original durable scope/checkpoint")
					}
					if ambiguous {
						return nil, errors.New("ambiguous release")
					}
				}
				return nil, nil
			})
			create := func(iface, table string) (*linuxExitGuard, error) {
				if iface != "oldexit0" || table != "51821" {
					t.Errorf("cleanup addressed replacement scope: %s %s", iface, table)
					return nil, errors.New("wrong scope")
				}
				return newLinuxExitGuard(iface, 51821, func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
					commands++
					return run(ctx, input, command, args...)
				})
			}
			engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "newexit0"}}
			if scenario == "foreign_live" {
				engine.configured = true
				engine.interface_ = "newexit0"
			} else if err := engine.restoreStartupExit(t.Context(), m.store.Read(), create); err != nil {
				t.Fatal(err)
			}
			executor, err := newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, create)
			if err != nil {
				t.Fatal(err)
			}
			err = m.reconcileExitChange(t.Context(), executor)
			if scenario == "foreign_live" {
				if commands != 0 || mutations != 0 || releases != 0 || !engine.configured || engine.exitGuard != nil || !reflect.DeepEqual(m.store.Read().RPCState.ExitProtection, originalProtection) {
					t.Fatal("foreign live runtime changed")
				}
				return
			}
			if ambiguous {
				cfg := m.store.Read()
				if cfg.RPCState.ExitChange == nil || !cfg.RPCState.ExitChange.Releasing || cfg.RPCState.ExitProtection == nil {
					t.Fatal("ambiguous release lost retry checkpoint", err)
				}
				ambiguous = false
				if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ExitChange.NextAttemptAt = time.Time{}; return nil }); err != nil {
					t.Fatal(err)
				}
				engine = &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "thirdexit0"}}
				if err := engine.restoreStartupExit(t.Context(), reopenRPCStoreFromDisk(t, m.store).Read(), create); err != nil {
					t.Fatal(err)
				}
				executor, err = newNativeExitExecutorWithGuard(engine, &sync.Mutex{}, create)
				if err != nil {
					t.Fatal(err)
				}
				err = m.reconcileExitChange(t.Context(), executor)
			}
			result, getErr := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			cfg := m.store.Read()
			if err != nil || getErr != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || cfg.ExitSelection != nil || cfg.RPCState.ExitProtection != nil || engine.exitGuard != nil || releases == 0 {
				t.Fatal("original scope clear failed", err, getErr, result)
			}
			before := mutations
			if status, err := executor.Observe(t.Context(), cfg); err != nil || status == nil || mutations != before {
				t.Fatal("original cleared scope not freshly observed", err)
			}
		})
	}
}
