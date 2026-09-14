package client

import (
	"context"
	"reflect"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func preparedNetworkSelectionFixture(t *testing.T) (*ClientRPCMutations, string, Config) {
	t.Helper()
	m, owner, request := networkSelectionPlanFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.PrivateKey = "synthetic-installation-key"
		cfg.IdentityPrivateKey = "synthetic-identity-key"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, op.Id, m.store.Read()
}

func TestNetworkRegistrationCheckpointStaysInTargetAcrossRestart(t *testing.T) {
	m, id, initial := preparedNetworkSelectionFixture(t)
	save := m.NetworkSelectionSaveCallback(t.Context(), id, initial)
	target := clonePersistentConfig(*initial.RPCState.NetworkSelection.Target)
	target.NodeID, target.NodeCredential = "target-node", "synthetic-target-credential"
	if err := save(target); err != nil {
		t.Fatal(err)
	}
	target.NodeApprovalState = "pending"
	if err := save(target); err != nil {
		t.Fatal(err)
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	persisted := store.Read()
	if !reflect.DeepEqual(networkSelectionContext(initial), networkSelectionContext(persisted)) || !reflect.DeepEqual(persisted.RPCState.NetworkSelection.Target, &target) {
		t.Fatal("checkpoint replaced live source or lost target authority")
	}
	m, err := NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	save = m.NetworkSelectionSaveCallback(t.Context(), id, persisted)
	target.NodeApprovalState = "approved"
	if err := save(target); err != nil {
		t.Fatal(err)
	}
	if m.store.Read().NodeID == target.NodeID {
		t.Fatal("checkpoint activated target")
	}
}

func TestNetworkRegistrationCheckpointBindsSignedRequest(t *testing.T) {
	setUserConfigDirForTest(t, t.TempDir())
	m, owner, request := networkSelectionPlanFixture(t)
	identity, err := GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	private, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wgkeys.PublicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	identityPublic, err := IdentityPublicKey(identity)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := DeviceFingerprint("https://control.test", public)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.PrivateKey = private
		cfg.IdentityPrivateKey = identity
		cfg.DeviceFingerprint = fingerprint
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.beginNetworkSelectionAs(owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	initial := m.store.Read()
	target := clonePersistentConfig(*initial.RPCState.NetworkSelection.Target)
	registration := api.RegisterNodeRequest{SchemaVersion: api.SchemaVersion, IdempotencyID: op.Id, NetworkID: "target", AccountID: "account", SessionTokenBinding: api.RegistrationSessionTokenBinding(target.Token), Hostname: "target-node", PublicKey: public, IdentityPublicKey: identityPublic, DeviceFingerprint: fingerprint}
	registration.IdentitySignature, err = SignIdentity(identity, api.RegistrationIdentityProofPayload(registration))
	if err != nil {
		t.Fatal(err)
	}
	target.PendingDirectRegistration = &PendingDirectRegistration{Origin: "https://control.test", Request: registration}
	save := m.NetworkSelectionSaveCallback(t.Context(), op.Id, initial)
	if err := save(target); err != nil {
		t.Fatal("signed request checkpoint failed", err)
	}
	registration.Hostname = "replacement-host"
	registration.IdentitySignature, err = SignIdentity(identity, api.RegistrationIdentityProofPayload(registration))
	if err != nil {
		t.Fatal(err)
	}
	target.PendingDirectRegistration.Request = registration
	assertRPCFailure(t, save(target), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if m.store.Read().RPCState.NetworkSelection.Target.PendingDirectRegistration.Request.Hostname != "target-node" {
		t.Fatal("retry replaced persisted signed request")
	}
}

func TestNetworkRegistrationCheckpointRejectsChangedBindings(t *testing.T) {
	for _, scenario := range []string{"network", "account", "origin", "token", "key", "intent", "browser", "concurrent_source", "concurrent_target", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, initial := preparedNetworkSelectionFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			save := m.NetworkSelectionSaveCallback(ctx, id, initial)
			target := clonePersistentConfig(*initial.RPCState.NetworkSelection.Target)
			switch scenario {
			case "network":
				target.NetworkID = "other"
			case "account":
				target.ActiveAccountID = "other"
			case "origin":
				target.ControlPlaneURLs = []string{"https://other.test"}
			case "token":
				target.Token = "other-session"
			case "key":
				target.PrivateKey = "rotated-key"
			case "intent":
				target.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
			case "browser":
				target.EnrollmentRequestID = "unrelated-browser-request"
			case "cancelled":
				cancel()
			case "concurrent_source", "concurrent_target":
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "concurrent_source" {
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
					} else {
						cfg.RPCState.NetworkSelection.Target.NodeID = "newer-node"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.store.Read()
			if err := save(target); err == nil {
				t.Fatal("changed binding accepted")
			}
			if !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("rejected checkpoint changed durable state")
			}
		})
	}
}
