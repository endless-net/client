package client

import (
	"context"
	"crypto/ed25519"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func networkSourceRefreshFixture(t *testing.T) (*ClientRPCMutations, Config, ed25519.PrivateKey) {
	t.Helper()
	m, target := networkRegistrationExecutorFixture(t)
	opts, key := signedServiceDNSFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		state := clonePersistentConfig(*cfg).CachedMap
		state.Network.AccountID = cfg.ActiveAccountID
		state.Node.PublicKey, _ = wgkeys.PublicKey(cfg.PrivateKey)
		state.Node.IdentityPublicKey, _ = IdentityPublicKey(cfg.IdentityPrivateKey)
		state.Node.DeviceFingerprint = cfg.DeviceFingerprint
		state.Node.ApprovalState = api.NodeApprovalApproved
		cfg.NodeApprovalState = api.NodeApprovalApproved
		state.Revision.Network = state.Network.Revision
		var err error
		state.MapSignature, err = api.SignNetworkMap(key, *state)
		if err != nil {
			return err
		}
		cfg.CachedMap, cfg.MapSigningTrust = state, opts.SigningTrust
		cfg.MapHash = state.MapSignature.PayloadHash
		cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, target, key
}

func refreshNetworkSourceForTest(t *testing.T, cfg *Config, key ed25519.PrivateKey, alter func(*api.RegisterNodeResponse)) {
	t.Helper()
	state := clonePersistentConfig(*cfg).CachedMap
	state.Network.Revision++
	state.Revision.Network, state.Revision.Global = state.Network.Revision, state.Revision.Global+1
	state.Node.Status = api.NodeStatusOnline
	state.Node.LastSeen = time.Now().UTC()
	state.Node.Endpoint = "203.0.113.12:51820"
	if alter != nil {
		alter(state)
	}
	var err error
	state.MapSignature, err = api.SignNetworkMap(key, *state)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cfg.CachedMap, cfg.CachedMapSavedAt = state, &now
	cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash = state.Network.Revision, state.Revision.Global, state.MapSignature.PayloadHash
}

func TestNetworkSourceRefreshDoesNotCancelIsolatedRegistration(t *testing.T) {
	m, target, key := networkSourceRefreshFixture(t)
	oldRevision := m.store.Read().MapRevision
	err := m.ReconcileNetworkSelectionRegistration(t.Context(), func(_ context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
		if err := m.store.Update(func(cfg *Config) error { refreshNetworkSourceForTest(t, cfg, key, nil); return nil }); err != nil {
			return nil, err
		}
		return nil, save(target)
	})
	if err != nil || !m.store.Read().RPCState.NetworkSelection.RegistrationReady || m.store.Read().MapRevision != oldRevision+1 {
		t.Fatal("verified source observation cancelled registration or was overwritten", err)
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	err = m.ReconcileNetworkSelectionActivation(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		if m.store.Read().MapRevision != oldRevision+1 {
			t.Fatal("handover restored stale source observations")
		}
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}})
	if err != nil || m.store.Read().NetworkID != target.NetworkID || m.store.Read().MapHash != target.MapHash {
		t.Fatal("handover failed or copied source observations into target", err)
	}
}

func TestNetworkSourceRefreshRejectsAuthorityChangesAndInvalidMaps(t *testing.T) {
	for _, mode := range []string{"managed_policy", "identity", "rollback", "cursor_conflict", "tampered_endpoint", "hash", "intent", "credential", "trust"} {
		t.Run(mode, func(t *testing.T) {
			m, _, key := networkSourceRefreshFixture(t)
			cfg := m.store.Read()
			plan := cfg.RPCState.NetworkSelection
			refreshNetworkSourceForTest(t, &cfg, key, func(state *api.RegisterNodeResponse) {
				switch mode {
				case "managed_policy":
					behavior := api.ClientLifecycleDisconnect
					state.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingUIQuit, Source: api.ClientPolicyAccount, PolicyID: "new-policy", Locked: true, LifecycleValue: &behavior}}}
				case "identity":
					state.Node.IdentityPublicKey = ""
				case "rollback":
					state.Network.Revision = plan.Source.MapRevision - 1
					state.Revision.Network = state.Network.Revision
				case "cursor_conflict":
					state.Network.Revision = plan.Source.MapRevision
					state.Revision = api.MapRevision{Network: plan.Source.MapRevision, Global: plan.Source.MapGlobalRevision}
				}
			})
			switch mode {
			case "tampered_endpoint":
				cfg.CachedMap.Node.Endpoint = "203.0.113.44:51820"
			case "hash":
				cfg.MapHash = "invalid"
			case "intent":
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "new_disconnect"}
			case "credential":
				cfg.NodeCredential = "new-credential"
			case "trust":
				cfg.MapSigningTrust = nil
			}
			if networkSelectionSourceMatches(cfg, plan) {
				t.Fatal("source authority change or unverified observation bypassed CAS")
			}
		})
	}
}
