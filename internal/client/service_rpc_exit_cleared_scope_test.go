package client

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitClearedScopeRequiresSuccessfulFinalCommit(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkID = "network"
		cfg.NodeCredential = "synthetic-secret-not-in-receipt"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReconcileOperation(op.Id, func(cfg *Config, operation *ipc.Operation) error {
		recordExitTestProtection(cfg)
		operation.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.checkpointExitRelease(t.Context(), op.Id, preparedExitClearTestStatus(profile.ProfileId)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.completeExitChange(op.Id, preparedExitClearTestStatus(profile.ProfileId), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN); err == nil {
		t.Fatal("closed checkpoint was accepted as final release")
	}
	before := reopenRPCStoreFromDisk(t, m.store).Read()
	if before.RPCState.ExitCleared != nil || before.RPCState.ExitProtection == nil || before.RPCState.ExitChange == nil {
		t.Fatal("failed final commit published cleared scope or lost protection")
	}
	if _, err := m.completeExitChange(op.Id, appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN); err != nil {
		t.Fatal(err)
	}
	after := reopenRPCStoreFromDisk(t, m.store).Read()
	if after.RPCState.ExitCleared == nil || after.RPCState.ExitCleared.OperationID != op.Id || after.RPCState.ExitChange != nil || after.RPCState.ExitProtection != nil || !after.RPCState.ExitCleared.bound(after, "endlessnet") {
		t.Fatal("successful final commit did not atomically replace ownership with observation scope")
	}
	raw, err := json.Marshal(after.RPCState.ExitCleared)
	if err != nil || strings.Contains(string(raw), "synthetic-secret") || strings.Contains(string(raw), "credential") || strings.Contains(string(raw), "private_key") {
		t.Fatal("observation receipt retained credentials", err)
	}
}

func TestInactiveProfileClearPersistsOriginalArtifactsAndCurrentObservationIdentity(t *testing.T) {
	m, owner, original := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "old-select", ProfileID: original.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: "old-node", NetworkID: "old-network", InterfaceName: "endlessnet", RouteTable: "51820"}
		current := cfg.RPCState.Profiles[original.ProfileId]
		current.ID = "current"
		cfg.RPCState.Profiles[current.ID] = current
		cfg.RPCState.ActiveProfileID = current.ID
		cfg.NodeID, cfg.NetworkID, cfg.WireGuardRouteTable = "current-node", "current-network", "52999"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: original}); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	runner := exitGuardReadbackRunner(t, "endlessnet", 51820, func(_ context.Context, _ string, command string, _ ...string) ([]byte, error) {
		if command == "nft" {
			mutations++
		}
		return []byte(`[]`), nil
	})
	create := func(name, table string) (*linuxExitGuard, error) {
		if name != "endlessnet" || table != "51820" {
			t.Fatal("old artifacts replaced by active identity scope", name, table)
		}
		return newLinuxExitGuard(name, 51820, runner)
	}
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}
	n := &nativeExitExecutor{engine: engine, createGuard: create}
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{}, Apply: n.apply, Release: n.release, Contain: n.contain}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	cfg := reopenRPCStoreFromDisk(t, m.store).Read()
	r := cfg.RPCState.ExitCleared
	if r == nil || r.Protection.NodeID != "old-node" || r.Protection.ProfileID != original.ProfileId || r.NodeID != "current-node" || r.ActiveProfileID != "current" || r.RouteTable != "52999" {
		t.Fatal("clear scope conflated original artifacts and current identity")
	}
	cfg.RPCState.Operations = nil
	restarted := &nativeExitExecutor{engine: &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}, createGuard: create}
	before := mutations
	status, err := restarted.observe(t.Context(), cfg)
	if err != nil || status == nil || status.ProfileId != "current" || status.FailClosed || mutations != before {
		t.Fatal("restart did not freshly observe old artifacts without changing current runtime", err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.LocalOwnerID = "another-owner" },
		func(c *Config) { c.RPCState.ActiveProfileID = original.ProfileId },
		func(c *Config) { c.NetworkID = "replacement" },
		func(c *Config) { c.RPCState.ExitCleared.Protection.InterfaceName = "../foreign" },
		func(c *Config) { c.RPCState.ExitCleared.Protection.OwnerID = "another-owner" },
	} {
		changed := clonePersistentConfig(cfg)
		change(&changed)
		if _, err := restarted.observe(t.Context(), changed); err == nil {
			t.Fatal("replacement ownership retained durable clear observation")
		}
	}
}
