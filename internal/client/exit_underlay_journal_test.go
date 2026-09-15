package client

import (
	"context"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestFirstExitRecoveryRequiresBoundRunningJournal(t *testing.T) {
	for _, scenario := range []string{"running", "pending", "terminal", "missing_operation", "owner", "record_owner", "profile", "origin", "intent", "node", "wrong_kind", "route_table"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic-credential", LocalOwnerID: "owner", ControlPlaneURLs: []string{"https://control.example"}}
			plan := &clientRPCExitChange{OperationID: "operation", ProfileID: "profile", OwnerID: "owner", ControlOrigin: "https://control.example", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Requested: &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID}}
			cfg.WireGuardRouteTable, plan.RouteTable = "51999", "51999"
			cfg.RPCState = &ClientRPCState{ActiveProfileID: "profile", Profiles: map[string]clientRPCProfile{"profile": {ID: "profile", ControlOrigin: plan.ControlOrigin}}, ExitChange: plan}
			op := &ipc.Operation{Id: plan.OperationID, ProfileId: plan.ProfileID, State: ipc.OperationState_OPERATION_STATE_RUNNING, Kind: ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE}
			record := clientRPCOperationRecord{Owner: "owner"}
			switch scenario {
			case "pending":
				op.State = ipc.OperationState_OPERATION_STATE_PENDING
			case "terminal":
				op.State = ipc.OperationState_OPERATION_STATE_FAILED
				now := time.Now()
				record.CompletedAt = &now
			case "missing_operation":
				op.Id = "different"
			case "owner":
				cfg.LocalOwnerID = "different"
			case "record_owner":
				record.Owner = "different"
			case "profile":
				cfg.RPCState.ActiveProfileID = "different"
			case "origin":
				cfg.ControlPlaneURLs = []string{"https://different.example"}
			case "intent":
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected}
			case "node":
				plan.NodeID = "different"
			case "route_table":
				cfg.WireGuardRouteTable = "52000"
			case "wrong_kind":
				op.Kind = ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE
			}
			var err error
			record.Operation, err = proto.Marshal(op)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RPCState.Operations = map[string]clientRPCOperationRecord{"request": record}
			engine := &WireGuardEngine{}
			if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
				t.Fatal("unrestored exit journal acquired ordinary transport")
			}
			if _, err := engine.Configure(t.Context(), cfg, clientapi.RegisterNodeResponse{}); err == nil || engine.device != nil || engine.router != nil {
				t.Fatal("unrestored exit journal entered ordinary runtime configuration")
			}
			calls := 0
			guard, err := newLinuxExitGuard("endlessnet", 51999, func(context.Context, string, string, ...string) ([]byte, error) { calls++; return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "route_table" {
				err = engine.restoreStartupExit(t.Context(), cfg, func(_, table string) (*linuxExitGuard, error) {
					if table != "51999" {
						t.Fatal("startup replaced the durable operation table")
					}
					return guard, nil
				})
			} else {
				err = engine.restoreExitUnderlay(t.Context(), cfg, guard)
			}
			if scenario != "running" {
				if err == nil || calls != 1 || engine.exitGuard != guard {
					t.Fatalf("invalid journal accepted: calls=%d error=%v", calls, err)
				}
				if control, err := engine.ControlPlaneHTTPClient(cfg); err == nil || control != nil {
					t.Fatal("invalid journal acquired control authority after containment")
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("first selection recovery: calls=%d error=%v", calls, err)
			}
			if cfg.ExitSelection != nil || engine.exitSelection != nil || engine.configured {
				t.Fatal("requested exit was promoted to applied")
			}
			control, err := engine.ControlPlaneHTTPClient(cfg)
			if err != nil {
				t.Fatal(err)
			}
			control.CloseIdleConnections()
		})
	}
}
