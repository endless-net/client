package client

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestStandaloneExitProtectionBlocksOrdinaryRuntime(t *testing.T) {
	cfg := Config{ControlPlaneURLs: []string{"https://control.example"}, RPCState: &ClientRPCState{ExitProtection: &clientRPCExitProtection{OperationID: "op", ProfileID: "profile", OwnerID: "owner", InterfaceName: "endlessnet"}}}
	engine := &WireGuardEngine{}
	if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
		t.Fatal("standalone ownership allowed ordinary control transport")
	}
	if _, err := engine.Configure(t.Context(), cfg, clientapi.RegisterNodeResponse{}); err == nil || !strings.Contains(err.Error(), "protected runtime recovery") {
		t.Fatal("standalone ownership reached ordinary tunnel apply", err)
	}
	cfg.RPCState.ExitProtection = nil
	control, err := engine.ControlPlaneHTTPClient(cfg)
	if err != nil || control == nil {
		t.Fatal("ordinary config lost ordinary control transport", err)
	}
	control.CloseIdleConnections()
}

func TestExitReleaseLateContextRetainsOwnershipForExplicitClear(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	var original *clientRPCExitProtection
	executor := clientRPCExitExecutor{InterfaceName: "endlessnet", Lock: &sync.Mutex{},
		Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			return preparedExitClearTestStatus(profile.ProfileId), 0, nil
		},
		Release: func(_ context.Context, _ string, input Config) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			original = cloneExitProtection(input.RPCState.ExitChange.Protection)
			if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "replacement"; return nil }); err != nil {
				t.Fatal(err)
			}
			return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
		},
		Contain: func(_ context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
			if !reflect.DeepEqual(plan.Protection, original) {
				t.Fatal("containment lost original OS scope")
			}
			return clientRPCExitContainment{OperationID: plan.OperationID, ProfileID: plan.ProfileID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, IPv4Blocked: true, IPv6Blocked: true, ExitRoutesRemoved: true}, nil
		},
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_FAILED {
		t.Fatal("late release context claimed success", err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	if cfg.ExitSelection != nil || cfg.RPCState.ExitChange != nil || !reflect.DeepEqual(cfg.RPCState.ExitProtection, original) {
		t.Fatal("terminal containment lost standalone protection marker")
	}
	engine := &WireGuardEngine{}
	contained := 0
	if err := engine.restoreStartupExit(t.Context(), cfg, func(name, table string) (*linuxExitGuard, error) {
		if name != original.InterfaceName || table != original.RouteTable {
			t.Fatal("restart changed ownership scope")
		}
		return newLinuxExitGuard(name, 51820, exitGuardReadbackRunner(t, name, 51820, func(context.Context, string, string, ...string) ([]byte, error) { contained++; return nil, nil }))
	}); err != nil {
		t.Fatal(err)
	}
	if contained != 1 || engine.exitGuard == nil {
		t.Fatal("standalone marker did not restore containment")
	}
	if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
		t.Fatal("clear marker conferred control authority")
	}

	// A different active profile does not make the original owner's artifacts
	// impossible to clear. No replacement selection may be affected.
	if err := m.store.Update(func(cfg *Config) error {
		other := cfg.RPCState.Profiles[profile.ProfileId]
		other.ID = "other"
		cfg.RPCState.Profiles[other.ID] = other
		cfg.RPCState.ActiveProfileID = other.ID
		cfg.WireGuardRouteTable = "52999"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	adminOwner := owner
	adminOwner.Administrator = true
	_, err = m.removeProfileAs(adminOwner, &ipc.RemoveProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	_, err = m.forgetEnrollmentAs(adminOwner, &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Confirmed: true})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	_, err = m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: "other"}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	clear, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal("original-profile cleanup became inaccessible", err)
	}
	executor.Release = func(_ context.Context, id string, input Config) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		if id != clear.Id || input.NodeID != "replacement" || input.RPCState.ActiveProfileID != "other" || !reflect.DeepEqual(input.RPCState.ExitChange.Protection, original) {
			t.Fatal("cleanup changed active identity or targeted replacement OS scope")
		}
		return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal(err)
	}
	cfg = m.store.Read()
	if cfg.RPCState.ExitProtection != nil || cfg.RPCState.ExitChange != nil || cfg.NodeID != "replacement" || cfg.RPCState.ActiveProfileID != "other" || cfg.WireGuardRouteTable != "52999" {
		t.Fatal("explicit cleanup lost current context or retained released ownership")
	}
}

func TestExitProtectionRejectsOtherOwnerAndActiveSelection(t *testing.T) {
	for _, changed := range []string{"owner", "selection"} {
		t.Run(changed, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "original", ProfileID: profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "endlessnet", RouteTable: cfg.WireGuardRouteTable}
				if changed == "owner" {
					cfg.RPCState.ExitProtection.OwnerID = "foreign"
				} else {
					cfg.ExitSelection = &ClientExitSelection{ID: "replacement", NodeID: "other-node"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			_, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			if m.store.Read().RPCState.ExitChange != nil || m.store.Read().RPCState.ExitProtection == nil {
				t.Fatal("rejected clear altered ownership")
			}
		})
	}
}
