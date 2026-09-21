package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNativeExitMaintenanceWithdrawsBeforeBoundedContainment(t *testing.T) {
	for _, scenario := range []string{"changed_context", "native_failure", "cancel_during_containment"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			filter := &exitPacketFilter{}
			commands := 0
			runner := exitGuardReadbackRunner(t, "endlessnet", 51820, func(recovery context.Context, input, command string, args ...string) ([]byte, error) {
				commands++
				filter.mu.RLock()
				closed := filter.closed
				filter.mu.RUnlock()
				if !closed || recovery.Err() != nil {
					t.Fatal("containment preceded withdrawal or inherited cancellation")
				}
				deadline, ok := recovery.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("emergency containment is unbounded")
				}
				if scenario == "cancel_during_containment" {
					cancel()
				}
				if scenario == "native_failure" {
					return nil, errors.New("sensitive native output")
				}
				return nil, nil
			})
			guard, err := newLinuxExitGuard("endlessnet", 51820, runner)
			if err != nil {
				t.Fatal(err)
			}
			selection := &ClientExitSelection{ID: "actual-runtime"}
			engine := &WireGuardEngine{configured: true, exitGuard: guard, exitSelection: selection, exitFilter: filter}
			err = (&nativeExitExecutor{engine: engine}).maintain(ctx, Config{})
			if !errors.Is(err, errNativeExitMaintenance) || commands == 0 || engine.exitGuard != guard || engine.exitSelection != selection || !filter.closed {
				t.Fatal("maintenance lost ownership or accepted failed evidence", err)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("native error disclosed")
			}
			if scenario == "native_failure" && !strings.Contains(err.Error(), "not confirmed") {
				t.Fatal("failed containment was reported as confirmed")
			}
			if scenario == "cancel_during_containment" && !errors.Is(err, context.Canceled) {
				t.Fatal("caller cancellation lost", err)
			}
		})
	}
}

func TestNativeExitMaintenanceDoesNotActivateStoppedRuntime(t *testing.T) {
	for _, scenario := range []string{"healthy", "tampered", "failed_repair"} {
		t.Run(scenario, func(t *testing.T) {
			commands := 0
			filter := &exitPacketFilter{}
			initialized := false
			native := exitGuardReadbackRunner(t, "endlessnet", 51820, func(context.Context, string, string, ...string) ([]byte, error) {
				commands++
				if initialized && !filter.closed {
					t.Fatal("stopped guard repair preceded filter withdrawal")
				}
				if initialized && scenario == "failed_repair" {
					return nil, errors.New("repair failed")
				}
				return nil, nil
			})
			tamper := scenario != "healthy"
			guard, err := newLinuxExitGuard("endlessnet", 51820, func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
				if initialized && tamper && input == "" {
					tamper = false
					return []byte(`{"nftables":[]}`), nil
				}
				return native(ctx, input, command, args...)
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := guard.Contain(t.Context()); err != nil {
				t.Fatal(err)
			}
			initialized, commands = true, 0
			engine := &WireGuardEngine{exitGuard: guard, exitFilter: filter}
			cfg := Config{ExitSelection: &ClientExitSelection{ID: "saved"}, ConnectionIntent: &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected}}
			err = (&nativeExitExecutor{engine: engine}).maintain(t.Context(), cfg)
			if engine.exitGuard != guard || engine.exitSelection != nil || engine.configured || engine.device != nil {
				t.Fatal("maintenance resumed saved selection or discarded protection", err)
			}
			if scenario == "healthy" {
				if err != nil || commands != 0 {
					t.Fatal("healthy stopped guard was mutated", err)
				}
			} else if !errors.Is(err, errNativeExitMaintenance) || commands == 0 || !filter.closed {
				t.Fatal("tampered stopped protection was ignored", err)
			}
			if scenario == "tampered" {
				if err := guard.ObserveContained(t.Context()); err != nil {
					t.Fatal("stopped guard repair not observed", err)
				}
			}
			if scenario == "failed_repair" && !strings.Contains(err.Error(), "not confirmed") {
				t.Fatal("failed stopped guard repair was accepted", err)
			}
		})
	}
}

func TestNativeExitMaintenanceAcceptsOnlyBoundDispatchedEffect(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkID = "network"
		cfg.CachedMap.MapSignature = &api.MapSignature{PayloadHash: "signed-hash"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	err = m.store.Update(func(cfg *Config) error {
		plan := cfg.RPCState.ExitChange
		plan.Requested = &ClientExitSelection{ID: "requested", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable}
		plan.MapHash = cfg.CachedMap.MapSignature.PayloadHash
		recordExitTestProtection(cfg)
		for key, record := range cfg.RPCState.Operations {
			operation := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, operation); err != nil {
				return err
			}
			if operation.Id != op.Id {
				continue
			}
			operation.State = ipc.OperationState_OPERATION_STATE_RUNNING
			operation.Kind = ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE
			encoded, err := proto.Marshal(operation)
			if err != nil {
				return err
			}
			record.Operation = encoded
			cfg.RPCState.Operations[key] = record
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	actual := cloneExitSelection(cfg.RPCState.ExitChange.Requested)
	guard, err := newLinuxExitGuard("endlessnet", 51820, func(context.Context, string, string, ...string) ([]byte, error) {
		t.Error("binding validation invoked a native command")
		return nil, errors.New("unexpected command")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := nativeExitMaintenanceProfile(cfg, actual, guard); err != nil || got != profile.ProfileId {
		t.Fatal("uncommitted but durably dispatched effect rejected", err)
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.RPCState.ExitChange.Containing = true },
		func(c *Config) { c.RPCState.ExitChange.Releasing = true },
		func(c *Config) { c.CachedMap.MapSignature.PayloadHash = "new-map" },
		func(c *Config) { c.RPCState.ActiveProfileID = "new-profile" },
		func(c *Config) { delete(c.RPCState.Profiles, c.RPCState.ActiveProfileID) },
		func(c *Config) { c.RPCState.ExitProtection.OperationID = "" },
		func(c *Config) { c.LocalOwnerID = ""; c.RPCState.ExitProtection.OwnerID = "" },
		func(c *Config) { c.RPCState.ExitProtection.InterfaceName = "other" },
		func(c *Config) { c.NodeID = "new-node" },
		func(c *Config) { c.WireGuardRouteTable = "51999" },
		func(c *Config) {
			c.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected}
		},
		func(c *Config) { c.RPCState.DisconnectOperationID = "disconnect" },
	} {
		changed := clonePersistentConfig(cfg)
		mutate(&changed)
		if _, err := nativeExitMaintenanceProfile(changed, actual, guard); err == nil {
			t.Fatal("changed authority accepted for existing native effect")
		}
	}
	settled := clonePersistentConfig(cfg)
	settled.ExitSelection = cloneExitSelection(actual)
	settled.RPCState.ExitChange = nil
	if _, err := nativeExitMaintenanceProfile(settled, actual, guard); err != nil {
		t.Fatal("settled matching selection rejected", err)
	}
}
