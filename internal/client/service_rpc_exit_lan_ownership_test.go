package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func rpcExitLANOwnershipFixture(t *testing.T) (*ClientRPCMutations, string, *clientRPCExitProtection) {
	t.Helper()
	m, owner, profile := rpcExitFixture(t)
	trusted, networkMap, key := signedApplicationFixture(t, false)
	networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
	host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
	networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANAllow}}}}
	resignApplicationMap(t, &networkMap, key)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
		cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_ALLOW}, []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANAllow}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReconcileOperation(op.Id, func(cfg *Config, operation *ipc.Operation) error {
		operation.State = ipc.OperationState_OPERATION_STATE_RUNNING
		recordExitTestProtection(cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, op.Id, cloneExitProtection(m.store.Read().RPCState.ExitProtection)
}

func TestExitLANOwnershipCheckpointPersistsBothScopesAndClones(t *testing.T) {
	m, id, expected := rpcExitLANOwnershipFixture(t)
	manifest := exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
	want := cloneExitLANOwnership(manifest)
	if err := m.checkpointExitLANOwnership(t.Context(), id, expected, manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Links[0].ID++
	if expected.LAN != nil {
		t.Fatal("checkpoint mutated caller protection")
	}
	reopened, err := NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	cfg := reopened.store.Read()
	if !reflect.DeepEqual(cfg.RPCState.ExitProtection.LAN, want) || !reflect.DeepEqual(cfg.RPCState.ExitChange.Protection, cfg.RPCState.ExitProtection) {
		t.Fatal("durable ownership lost one protection copy")
	}
	copy := cloneExitProtection(cfg.RPCState.ExitProtection)
	copy.LAN.Links[0].ID++
	if !reflect.DeepEqual(cfg.RPCState.ExitProtection.LAN, want) {
		t.Fatal("protection clone shares link slice")
	}
	cfg.RPCState.ExitChange.Protection.LAN.Links[0].ID++
	if !reflect.DeepEqual(cfg.RPCState.ExitProtection.LAN, want) || !reflect.DeepEqual(reopened.store.Read().RPCState.ExitChange.Protection.LAN, want) {
		t.Fatal("journal/read snapshot aliases durable ownership")
	}
	before := clonePersistentConfig(reopened.store.Read())
	if err := reopened.checkpointExitLANOwnership(t.Context(), id, expected, want); err == nil {
		t.Fatal("old expected protection bypassed CAS")
	}
	fresh := reopened.store.Read().RPCState.ExitProtection
	if err := reopened.checkpointExitLANOwnership(t.Context(), id, fresh, want); err != nil {
		t.Fatal("identical fresh retry rejected", err)
	}
	changed := cloneExitLANOwnership(want)
	changed.Scope = "1123456789abcdef01234567"
	if err := reopened.checkpointExitLANOwnership(t.Context(), id, fresh, changed); err == nil {
		t.Fatal("checkpoint replaced retained manifest")
	}
	if err := reopened.checkpointExitLANOwnership(t.Context(), id, fresh, nil); err == nil {
		t.Fatal("checkpoint removed retained manifest")
	}
	if !reflect.DeepEqual(before, clonePersistentConfig(reopened.store.Read())) {
		t.Fatal("retry/rejected replacement changed durable state")
	}
}

func TestExitLANOwnershipCheckpointRejectsInvalidAdmission(t *testing.T) {
	for _, scenario := range []string{"expected", "nil_expected", "invalid_manifest", "family", "pending", "terminal", "kind", "containing", "releasing", "block", "clear", "node", "profile", "owner", "map", "protection", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, expected := rpcExitLANOwnershipFixture(t)
			manifest := exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "expected":
				expected.OperationID = "foreign"
			case "nil_expected":
				expected = nil
			case "invalid_manifest":
				manifest.Scope = "../foreign"
			case "family":
				manifest = exitLANOwnershipFixture(api.ExitFamilyIPv6Only)
			case "cancelled":
				cancel()
			case "pending", "terminal", "kind":
				// Deliberately persisted invalid/replayed journals must not gain
				// pin authority; the normal reconciler rejects these transitions.
				if err := m.store.Update(func(cfg *Config) error {
					for request, record := range cfg.RPCState.Operations {
						operation := new(ipc.Operation)
						if err := proto.Unmarshal(record.Operation, operation); err != nil {
							return err
						}
						if operation.Id != id {
							continue
						}
						switch scenario {
						case "pending":
							operation.State = ipc.OperationState_OPERATION_STATE_PENDING
						case "terminal":
							operation.State = ipc.OperationState_OPERATION_STATE_FAILED
						case "kind":
							operation.Kind = ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE
						}
						encoded, err := proto.Marshal(operation)
						if err != nil {
							return err
						}
						record.Operation = encoded
						cfg.RPCState.Operations[request] = record
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err := m.ReconcileOperation(id, func(cfg *Config, operation *ipc.Operation) error {
					plan := cfg.RPCState.ExitChange
					switch scenario {
					case "containing":
						plan.Containing = true
					case "releasing":
						plan.Releasing = true
					case "block":
						plan.Requested.LAN = api.ExitLANBlock
					case "clear":
						plan.Requested = nil
					case "node":
						cfg.NodeID = "replacement"
					case "profile":
						cfg.RPCState.ActiveProfileID = "replacement"
					case "owner":
						cfg.LocalOwnerID = "replacement"
					case "map":
						cfg.CachedMap.MapSignature.PayloadHash = "replacement"
					case "protection":
						cfg.RPCState.ExitProtection.OperationID = "replacement"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := clonePersistentConfig(m.store.Read())
			err := m.checkpointExitLANOwnership(ctx, id, expected, manifest)
			if err == nil {
				t.Fatal("invalid admission persisted manifest")
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			if !reflect.DeepEqual(before, clonePersistentConfig(reopenRPCStoreFromDisk(t, m.store).Read())) {
				t.Fatal("rejected checkpoint changed durable state")
			}
		})
	}
}

func TestExitClearCannotReleaseRetainedLANManifest(t *testing.T) {
	m, owner, profile := rpcExitFixture(t)
	manifest := exitLANOwnershipFixture(api.ExitFamilyIPv4Only)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "previous", ProfileID: profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "endlessnet", RouteTable: cfg.WireGuardRouteTable, LAN: cloneExitLANOwnership(manifest)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReconcileOperation(op.Id, func(_ *Config, operation *ipc.Operation) error {
		operation.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.checkpointExitRelease(t.Context(), op.Id, preparedExitClearTestStatus(profile.ProfileId)); err == nil {
		t.Fatal("clear checkpoint ignored retained pins")
	}
	// Even a persisted release checkpoint must not bypass pending BPF cleanup.
	if _, err := m.ReconcileOperation(op.Id, func(cfg *Config, _ *ipc.Operation) error { cfg.RPCState.ExitChange.Releasing = true; return nil }); err != nil {
		t.Fatal(err)
	}
	before := clonePersistentConfig(m.store.Read())
	if _, err := nativeExitOperation(before, op.Id, nil, true); err == nil {
		t.Fatal("native release accepted retained LAN ownership")
	}
	if _, err := m.exitReleaseInput(t.Context(), op.Id); err == nil {
		t.Fatal("native release dispatched with retained pins")
	}
	if _, err := m.completeExitChange(op.Id, appliedExitTestStatus(profile.ProfileId, false), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN); err == nil {
		t.Fatal("clear completion discarded retained pins")
	}
	after := clonePersistentConfig(reopenRPCStoreFromDisk(t, m.store).Read())
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(after.RPCState.ExitProtection.LAN, manifest) || !reflect.DeepEqual(after.RPCState.ExitChange.Protection.LAN, manifest) {
		t.Fatal("rejected clear lost durable cleanup scope")
	}
}
