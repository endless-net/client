package client

import (
	"context"
	"reflect"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func networkRegistrationExecutorFixture(t *testing.T) (*ClientRPCMutations, Config) {
	t.Helper()
	m, owner, request := networkSelectionPlanFixture(t)
	opts, key := signedServiceDNSFixture(t)
	private, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := GenerateIdentityPrivateKey()
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
	if err := m.store.Update(func(cfg *Config) error {
		cfg.PrivateKey = private
		cfg.IdentityPrivateKey = identity
		cfg.DeviceFingerprint = "synthetic-fingerprint"
		cfg.MapSigningTrust = opts.SigningTrust
		cfg.NodeCredentialSigningTrust = opts.SigningTrust
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.beginNetworkSelectionAs(owner, request); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionPreparation(t.Context(), func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	target := clonePersistentConfig(*m.store.Read().RPCState.NetworkSelection.Target)
	state := opts.NetworkMap
	state.Network.ID, state.Network.AccountID, state.Node.NetworkID = "target", "account", "target"
	state.Node.ID, state.Node.PublicKey, state.Node.IdentityPublicKey, state.Node.DeviceFingerprint = "target-node", public, identityPublic, target.DeviceFingerprint
	state.MapSignature, err = api.SignNetworkMap(key, state)
	if err != nil {
		t.Fatal(err)
	}
	target.NodeID, target.NodeApprovalState = "target-node", api.NodeApprovalApproved
	target.NodeCredential, err = api.SignNodeCredential(key, "target", "target-node", []string{"node:map"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	target.CachedMap = &state
	target.MapRevision, target.MapGlobalRevision, target.MapHash = state.Network.Revision, state.Revision.Global, state.MapSignature.PayloadHash
	return m, target
}

func TestNetworkRegistrationCoordinatorResumesApprovalWithoutActivation(t *testing.T) {
	m, target := networkRegistrationExecutorFixture(t)
	source := networkSelectionContext(m.store.Read())
	calls := 0
	provider := func(_ context.Context, cfg Config, input ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
		calls++
		if input.NetworkID != "target" || input.OperationID != m.store.Read().RPCState.NetworkSelection.OperationID {
			t.Fatal("provider lost durable binding")
		}
		if calls == 1 {
			cfg.NodeID, cfg.NodeCredential, cfg.NodeApprovalState = target.NodeID, target.NodeCredential, api.NodeApprovalPending
			if err := save(cfg); err != nil {
				return nil, err
			}
			return &ipc.UserAction{Kind: ipc.UserAction_KIND_WAIT_FOR_APPROVAL}, nil
		}
		if cfg.NodeID != target.NodeID || cfg.NodeCredential != target.NodeCredential {
			t.Fatal("restart lost registration checkpoint")
		}
		return nil, save(target)
	}
	if err := m.ReconcileNetworkSelectionRegistration(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	if m.store.Read().RPCState.NetworkSelection.RegistrationReady {
		t.Fatal("pending approval marked ready")
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionRegistration(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionRegistration(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	if cfg := m.store.Read(); calls != 2 || !cfg.RPCState.NetworkSelection.RegistrationReady || !reflect.DeepEqual(source, networkSelectionContext(cfg)) {
		t.Fatal("registration repeated, failed readiness or activated target")
	}
	// Ready is only a checkpoint, not an exemption from expiry at handover.
	m.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	if err := m.ReconcileNetworkSelectionRegistration(t.Context(), provider); err == nil || m.store.Read().RPCState.NetworkSelection.RegistrationReady {
		t.Fatal("expired authority remained ready")
	}
}

func TestNetworkRegistrationCoordinatorRejectsFalseSuccess(t *testing.T) {
	for _, mode := range []string{"no_checkpoint", "tampered_map", "changed_source"} {
		t.Run(mode, func(t *testing.T) {
			m, target := networkRegistrationExecutorFixture(t)
			err := m.ReconcileNetworkSelectionRegistration(t.Context(), func(_ context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
				if mode == "no_checkpoint" {
					return nil, nil
				}
				if mode == "tampered_map" {
					target.CachedMap.Network.Name = "modified"
				}
				if err := save(target); err != nil {
					return nil, err
				}
				if mode == "changed_source" {
					if err := m.store.Update(func(cfg *Config) error {
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				return nil, nil
			})
			if err == nil || m.store.Read().RPCState.NetworkSelection.RegistrationReady {
				t.Fatal("unverified or stale provider result marked ready")
			}
		})
	}
}

func TestNetworkSelectionReadinessRejectsMismatchedAuthority(t *testing.T) {
	_, target := networkRegistrationExecutorFixture(t)
	now := time.Now()
	if !networkSelectionTargetReady(target, now) {
		t.Fatal("valid signed target is not ready")
	}
	for name, alter := range map[string]func(*Config){
		"credential":       func(cfg *Config) { cfg.NodeCredential = "invalid" },
		"credential_trust": func(cfg *Config) { cfg.NodeCredentialSigningTrust = nil },
		"map_trust":        func(cfg *Config) { cfg.MapSigningTrust = nil },
		"node":             func(cfg *Config) { cfg.NodeID = "other-node" },
		"network":          func(cfg *Config) { cfg.NetworkID = "other-network" },
		"account":          func(cfg *Config) { cfg.ActiveAccountID = "other-account" },
		"wireguard_key":    func(cfg *Config) { cfg.PrivateKey = "invalid" },
		"identity_key":     func(cfg *Config) { cfg.IdentityPrivateKey = "invalid" },
		"fingerprint":      func(cfg *Config) { cfg.DeviceFingerprint = "other-device" },
		"revision":         func(cfg *Config) { cfg.MapRevision++ },
		"global_revision":  func(cfg *Config) { cfg.MapGlobalRevision++ },
		"hash":             func(cfg *Config) { cfg.MapHash = "other-hash" },
		"approval":         func(cfg *Config) { cfg.NodeApprovalState = api.NodeApprovalPending },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := clonePersistentConfig(target)
			alter(&cfg)
			if networkSelectionTargetReady(cfg, now) {
				t.Fatal("mismatched registration authority marked ready")
			}
		})
	}
}
