package client

import (
	"context"
	"errors"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestExitStartupRestoresContainmentBeforeControlAccess(t *testing.T) {
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{NodeID: "node", NetworkID: "network", NodeCredential: "synthetic", ControlPlaneURLs: []string{"https://control.example"}, ExitSelection: &ClientExitSelection{ID: "exit", NodeID: "node", NetworkID: "network"}}
	created, contained := 0, 0
	cfg.WireGuardRouteTable = "51999"
	cfg.ExitSelection.RouteTable = cfg.WireGuardRouteTable
	create := func(name, table string) (*linuxExitGuard, error) {
		created++
		if name != "endlessnet" {
			t.Fatal("startup changed guard interface")
		}
		if table != "51999" {
			t.Fatal("startup lost the configured route table")
		}
		return newLinuxExitGuard(name, 51999, exitGuardReadbackRunner(t, name, 51999, func(context.Context, string, string, ...string) ([]byte, error) { contained++; return nil, nil }))
	}
	if err := engine.restoreStartupExit(t.Context(), cfg, create); err != nil {
		t.Fatal(err)
	}
	if created != 1 || contained != 1 || engine.configured || engine.exitSelection != nil {
		t.Fatal("startup did not restore only closed containment")
	}
	control, err := engine.ControlPlaneHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	control.CloseIdleConnections()
	if err := engine.restoreStartupExit(t.Context(), cfg, create); err != nil {
		t.Fatal(err)
	}
	if created != 1 || contained != 2 {
		t.Fatal("startup retry replaced guard ownership")
	}
	// No operation journal remains for a completed selection.
	fresh, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet"})
	if err != nil {
		t.Fatal(err)
	}
	changed := cfg
	changed.WireGuardRouteTable = "52000"
	if err := fresh.restoreStartupExit(t.Context(), changed, create); err == nil || created != 2 || contained != 3 || fresh.exitGuard == nil || fresh.exitGuard.mark != 51999 {
		t.Fatal("completed selection lost its original table after restart", err)
	}
	if control, err := fresh.ControlPlaneHTTPClient(changed); err == nil || control != nil {
		t.Fatal("changed table acquired completed selection's control authority")
	}
	if err := fresh.restoreStartupExit(t.Context(), cfg, create); err != nil || created != 2 || contained != 4 {
		t.Fatal("original completed selection could not recover", err)
	}
}

func TestExitStartupSkipsOnlyUndispatchedJournal(t *testing.T) {
	engine := &WireGuardEngine{}
	op := &ipc.Operation{Id: "op", ProfileId: "profile", State: ipc.OperationState_OPERATION_STATE_PENDING, Kind: ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE}
	raw, err := proto.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{RPCState: &ClientRPCState{ExitChange: &clientRPCExitChange{OperationID: "op", ProfileID: "profile"}, Operations: map[string]clientRPCOperationRecord{"request": {Operation: raw}}}}
	failure := errors.New("platform guard unavailable")
	calls := 0
	create := func(string, string) (*linuxExitGuard, error) { calls++; return nil, failure }
	if err := engine.restoreStartupExit(t.Context(), Config{}, create); err != nil || calls != 0 {
		t.Fatal("ordinary startup requested an exit guard")
	}
	if err := engine.restoreStartupExit(t.Context(), cfg, create); err != nil || calls != 0 {
		t.Fatal("undispatched clear required guard recovery")
	}
	cfg.RPCState.ExitChange.Containing = true
	if err := engine.restoreStartupExit(t.Context(), cfg, create); !errors.Is(err, failure) || calls != 1 {
		t.Fatal("uncertain effects bypassed guard recovery")
	}
}
