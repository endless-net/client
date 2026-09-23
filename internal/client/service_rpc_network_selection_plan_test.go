package client

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func networkSelectionPlanFixture(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.SelectNetworkRequest) {
	t.Helper()
	m, owner, profile := rpcPreferenceFixture(t)
	private, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.PrivateKey, cfg.IdentityPrivateKey, cfg.DeviceFingerprint = private, identity, "synthetic-fingerprint"
		cfg.ControlPlaneURLs = []string{cfg.RPCState.Profiles[profile.ProfileId].ControlOrigin}
		cfg.Token, cfg.ActiveAccountID, cfg.NodeCredential = "synthetic-private-session", "account", "synthetic-private-node"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, owner, &ipc.SelectNetworkRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, NetworkId: "target"}
}

func TestNetworkSelectionPreparationPersistsIsolatedPlanAndReplay(t *testing.T) {
	m, owner, request := networkSelectionPlanFixture(t)
	before := networkSelectionContext(m.store.Read())
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != ipc.OperationState_OPERATION_STATE_PENDING || !reflect.DeepEqual(before, networkSelectionContext(m.store.Read())) {
		t.Fatal("admission changed live network or completed selection")
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil || !proto.Equal(replay, op) {
		t.Fatal("admission replay changed operation", err)
	}
	calls := 0
	provider := func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		calls++
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	prepared := m.store.Read()
	if prepared.RPCState.NetworkSelection.Target == nil || prepared.RPCState.NetworkSelection.Target.NodeCredential != "" || !reflect.DeepEqual(before, networkSelectionContext(prepared)) {
		t.Fatal("prepared candidate mixed or activated network authority")
	}
	store = reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), provider); err != nil || calls != 1 {
		t.Fatal("checkpoint recovery repeated catalog preparation", err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protojson.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || strings.Contains(string(raw), "synthetic-private") {
		t.Fatal("preparation leaked authority or claimed completion")
	}
}

func TestNetworkSelectionAdmissionRejectsInvalidInstallationAndOrigin(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Config)
		code   ipc.ErrorCode
	}{
		{"missing_wireguard_key", func(cfg *Config) { cfg.PrivateKey = "" }, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT},
		{"invalid_wireguard_key", func(cfg *Config) { cfg.PrivateKey = "invalid" }, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT},
		{"missing_identity_key", func(cfg *Config) { cfg.IdentityPrivateKey = "" }, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT},
		{"invalid_identity_key", func(cfg *Config) { cfg.IdentityPrivateKey = "invalid" }, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT},
		{"missing_fingerprint", func(cfg *Config) { cfg.DeviceFingerprint = "" }, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT},
		{"missing_origin", func(cfg *Config) { cfg.ControlPlaneURLs = nil }, ipc.ErrorCode_ERROR_CODE_STALE_STATE},
		{"foreign_origin", func(cfg *Config) { cfg.ControlPlaneURLs = append(cfg.ControlPlaneURLs, "https://foreign.test") }, ipc.ErrorCode_ERROR_CODE_STALE_STATE},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, owner, request := networkSelectionPlanFixture(t)
			if err := m.store.Update(func(cfg *Config) error { test.change(cfg); return nil }); err != nil {
				t.Fatal(err)
			}
			before := clonePersistentConfig(m.store.Read())
			_, err := m.beginNetworkSelectionAs(owner, request)
			assertRPCFailure(t, err, test.code)
			if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
				t.Fatal("rejected admission persisted an operation or changed source")
			}
		})
	}
}

func TestNetworkSelectionRejectsOwnedExitBeforeAdmission(t *testing.T) {
	for _, state := range []string{"protected", "selected"} {
		t.Run(state, func(t *testing.T) {
			m, owner, request := networkSelectionPlanFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				if state == "protected" {
					cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "exit-select", ProfileID: request.Profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "endlessnet"}
				} else {
					cfg.ExitSelection = &ClientExitSelection{ID: "exit"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := clonePersistentConfig(m.store.Read())
			_, err := m.beginNetworkSelectionAs(owner, request)
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
			if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
				t.Fatal("rejected network switch changed exit ownership or accepted an operation")
			}
		})
	}
}

func TestNetworkSelectionAbortsPersistedExitOwnershipBeforeStop(t *testing.T) {
	m, owner, request := networkSelectionPlanFixture(t)
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ExitSelection = &ClientExitSelection{ID: "source-exit"}
		cfg.RPCState.ExitProtection = &clientRPCExitProtection{OperationID: "exit-select", ProfileID: request.Profile.ProfileId, OwnerID: cfg.LocalOwnerID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, InterfaceName: "endlessnet"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := clonePersistentConfig(m.store.Read())
	store := reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	if err := m.ReconcileNetworkSelection(t.Context(), driver, ClientRPCNetworkSelectionProviders{}); err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil {
		t.Fatal(err)
	}
	after := m.store.Read()
	if stops != 0 || result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED || result.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED || after.RPCState.NetworkSelection != nil {
		t.Fatal("saved network switch crossed owned exit or remained pending")
	}
	if after.NodeID != before.NodeID || after.NetworkID != before.NetworkID || !reflect.DeepEqual(after.ExitSelection, before.ExitSelection) || !reflect.DeepEqual(after.RPCState.ExitProtection, before.RPCState.ExitProtection) {
		t.Fatal("saved network switch changed protected source")
	}
}

func TestNetworkSelectionPreparationRejectsConcurrentSourceChange(t *testing.T) {
	for _, change := range []string{"intent", "owner", "profile"} {
		t.Run(change, func(t *testing.T) {
			m, owner, request := networkSelectionPlanFixture(t)
			op, err := m.beginNetworkSelectionAs(owner, request)
			if err != nil {
				t.Fatal(err)
			}
			err = m.ReconcileNetworkSelectionPreparation(t.Context(), func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
				if err := m.store.Update(func(cfg *Config) error {
					switch change {
					case "intent":
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
					case "owner":
						cfg.LocalOwnerID = "another-owner"
					case "profile":
						p := cfg.RPCState.Profiles[request.Profile.ProfileId]
						p.DisplayName = "renamed"
						cfg.RPCState.Profiles[p.ID] = p
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
			})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			if cfg := m.store.Read(); cfg.RPCState.NetworkSelection.Target != nil || cfg.RPCState.Revision != op.Metadata.Revision {
				t.Fatal("stale provider committed target or advanced journal")
			}
		})
	}
}
