package client

import (
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestExitSelectionRejectsUnexecutableRouteScopeBeforeJournal(t *testing.T) {
	for _, table := range []string{"off", "253", "254", "255", "wrong", "051820", " AUTO ", "0"} {
		t.Run(table, func(t *testing.T) {
			m, owner, profile := rpcExitFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.WireGuardRouteTable = table
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}
			before := m.store.Read()
			_, err := m.selectExitNodeAs(owner, request, []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}})
			want := ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
			if table == "off" || table == "253" || table == "254" || table == "255" {
				want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			}
			assertRPCFailure(t, err, want)
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("invalid selection route recorded journal")
			}
			if m.exitMutationRestriction(before, profile, ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE, true).Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
				t.Fatal("invalid route advertised selection readiness")
			}
		})
	}
}

func TestExitClearRejectsUnrecoverableIdentityAndRouteBeforeJournal(t *testing.T) {
	for _, scenario := range []string{"missing_node", "missing_network", "off", "main", "local", "default", "malformed", "incomplete_protection"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcExitFixture(t)
			want := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			if err := m.store.Update(func(cfg *Config) error {
				switch scenario {
				case "missing_node":
					cfg.NodeID = ""
				case "missing_network":
					cfg.NetworkID = ""
				case "off":
					cfg.WireGuardRouteTable = "off"
					want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				case "main":
					cfg.WireGuardRouteTable = "254"
					want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				case "local":
					cfg.WireGuardRouteTable = "255"
					want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				case "default":
					cfg.WireGuardRouteTable = "253"
					want = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				case "malformed":
					cfg.WireGuardRouteTable = "wrong"
					want = ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
				case "incomplete_protection":
					cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "old", ProfileID: profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, InterfaceName: "endlessnet"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
			before := m.store.Read()
			if _, err := m.clearExitNodeAs(owner, request); err == nil {
				t.Fatal("unrecoverable clear admitted")
			} else {
				assertRPCFailure(t, err, want)
			}
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("rejected clear recorded effects or journal")
			}
			restriction := m.exitMutationRestriction(before, profile, ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE, true)
			if restriction.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
				t.Fatal("read projection advertised rejected Clear")
			}
		})
	}
}

func TestExitClearUsesRetainedIdentityWithoutCurrentEnrollmentAndPreservesReplay(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "old", ProfileID: profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "endlessnet", RouteTable: "51999"}
		cfg.NodeID, cfg.NetworkID, cfg.NodeCredential = "", "", ""
		cfg.WireGuardRouteTable = "off"
		cfg.CachedMap = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}
	op, err := m.clearExitNodeAs(owner, request)
	if err != nil {
		t.Fatal("offline original protection could not clear", err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ExitProtection = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	replayed, err := m.clearExitNodeAs(owner, request)
	if err != nil || !proto.Equal(op, replayed) {
		t.Fatal("accepted replay re-evaluated current cleanup admission", err)
	}
}
