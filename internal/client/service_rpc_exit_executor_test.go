package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCExitExecutorSerializesConcurrentAttempts(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	_, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	executor := clientRPCExitExecutor{Lock: &sync.Mutex{}, Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	finished := make(chan error, 2)
	go func() { finished <- m.reconcileExitChange(t.Context(), executor) }()
	<-entered
	go func() { finished <- m.reconcileExitChange(t.Context(), executor) }()
	close(release)
	for range 2 {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 || m.store.Read().RPCState.ExitChange != nil {
		t.Fatal("concurrent attempts duplicated OS application")
	}
}

func TestRPCExitExecutorDurabilityAndRevalidation(t *testing.T) {
	for _, scenario := range []string{"select", "clear", "expired", "unsupported", "stale", "route_table", "ambiguous", "partial", "late_context_change", "late_table_change", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			now := time.Now()
			m.now = func() time.Time { return now }
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: now.Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &networkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.WireGuardRouteTable = "51821"
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				if scenario == "clear" {
					cfg.ExitSelection = &ClientExitSelection{ID: "exit", NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, Host: host, Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
					cfg.CachedMap = nil
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			modes := []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}}
			var op *ipc.Operation
			var err error
			if scenario == "clear" {
				op, err = m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
			} else {
				op, err = m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, modes)
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				now = now.Add(2 * time.Minute)
			}
			if scenario == "unsupported" {
				modes = nil
			}
			if scenario == "stale" {
				if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "replacement"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "route_table" {
				if err := m.store.Update(func(cfg *Config) error { cfg.WireGuardRouteTable = "51999"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			executor := clientRPCExitExecutor{Lock: &sync.Mutex{}, Modes: modes, Apply: func(_ context.Context, id string, input Config, selection *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
				calls++
				stored, loadErr := loadConfigFile(m.store.path)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if id != op.Id || input.RPCState.ExitChange.OperationID != id || stored.RPCState.ExitChange.OperationID != id || !stored.RPCState.ExitChange.NextAttemptAt.After(now) {
					t.Fatal("OS dispatch preceded durable checkpoint or changed operation identity")
				}
				if stored.RPCState.ExitChange.RouteTable != "51821" || input.RPCState.ExitChange.RouteTable != "51821" {
					t.Fatal("route table was not durably admitted before OS dispatch")
				}
				current, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: id}})
				if err != nil || current.State != ipc.OperationState_OPERATION_STATE_RUNNING {
					t.Fatal("operation was not RUNNING before dispatch", err)
				}
				if (selection == nil) != (scenario == "clear") {
					t.Fatal("wrong apply intent")
				}
				status := appliedExitTestStatus(profile.ProfileId, scenario != "clear")
				if calls == 1 {
					switch scenario {
					case "ambiguous":
						return nil, 0, errors.New("private native failure")
					case "partial":
						status.Ipv4.FailClosed = false
					case "cancelled":
						cancel()
					case "late_context_change":
						if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "replacement"; return nil }); err != nil {
							t.Fatal(err)
						}
					case "late_table_change":
						if err := m.store.Update(func(cfg *Config) error { cfg.WireGuardRouteTable = "51999"; return nil }); err != nil {
							t.Fatal(err)
						}
					}
				}
				return status, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
			}}
			err = m.reconcileExitChange(ctx, executor)
			if scenario == "late_context_change" || scenario == "late_table_change" {
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			} else if scenario == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			current, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "expired", "unsupported", "stale", "route_table":
				if calls != 0 || current.State != ipc.OperationState_OPERATION_STATE_FAILED || m.store.Read().RPCState.ExitChange != nil {
					t.Fatal("invalid queued intent reached executor or retained guard")
				}
			case "ambiguous", "partial", "cancelled", "late_context_change", "late_table_change":
				if calls != 1 || current.State != ipc.OperationState_OPERATION_STATE_RUNNING || m.store.Read().ExitSelection != nil || m.store.Read().RPCState.ExitChange == nil {
					t.Fatal("uncertain effects were committed or their guard released")
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				m.now = func() time.Time { return now }
				if scenario == "late_context_change" || scenario == "late_table_change" {
					assertRPCFailure(t, m.reconcileExitChange(t.Context(), executor), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
					if calls != 1 || m.store.Read().RPCState.ExitChange == nil {
						t.Fatal("stale running effects lost containment guard")
					}
					return
				}
				if err := m.reconcileExitChange(t.Context(), executor); err != nil || calls != 1 {
					t.Fatal("restart ignored retry deadline", err)
				}
				now = now.Add(6 * time.Second)
				if err := m.reconcileExitChange(t.Context(), executor); err != nil {
					t.Fatal(err)
				}
				current, err = m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
				if err != nil || calls != 2 || current.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
					t.Fatal("same-ID recovery failed", err)
				}
			default:
				if calls != 1 || current.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || m.store.Read().RPCState.ExitChange != nil {
					t.Fatal("verified application did not complete")
				}
			}
		})
	}
}
