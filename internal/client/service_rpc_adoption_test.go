package client

import (
	"reflect"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCInitialProfileAdoptionPreservesState(t *testing.T) {
	for _, owner := range []string{"", "uid:1000"} {
		m := newRPCStoreTest(t)
		if err := m.store.Update(func(cfg *Config) error {
			cfg.ControlPlaneURLs = []string{"https://CONTROL.test:443/", "https://control.test"}
			cfg.LocalOwnerID = owner
			cfg.NodeID, cfg.NetworkID = "existing-node", "existing-network"
			cfg.PrivateKey, cfg.NodeCredential = "synthetic-installation-key", "synthetic-node-credential"
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before := clonePersistentConfig(m.store.Read())
		if err := m.AdoptInitialProfile(); err != nil {
			t.Fatal(err)
		}
		after := clonePersistentConfig(m.store.Read())
		state := after.RPCState
		after.RPCState = before.RPCState
		if !reflect.DeepEqual(before, after) {
			t.Fatal("adoption changed existing runtime config")
		}
		if !validRPCUUID(state.ActiveProfileID) || len(state.Profiles) != 1 || state.Profiles[state.ActiveProfileID].ControlOrigin != "https://control.test" {
			t.Fatal("invalid adopted profile")
		}
		if state.Profiles[state.ActiveProfileID].Configuration.PrivateKey != "" {
			t.Fatal("adoption duplicated installation keys")
		}
		if err := m.AdoptInitialProfile(); err != nil {
			t.Fatal(err)
		}
		if m.store.Read().RPCState.Revision != state.Revision || m.store.Read().RPCState.ActiveProfileID != state.ActiveProfileID {
			t.Fatal("adoption not idempotent")
		}
		store, err := OpenConfigStore(m.store.path)
		if err != nil {
			t.Fatal(err)
		}
		restarted, err := NewClientRPCMutations(store)
		if err != nil {
			t.Fatal(err)
		}
		if err := restarted.AdoptInitialProfile(); err != nil {
			t.Fatal(err)
		}
		if store.Read().RPCState.ActiveProfileID != state.ActiveProfileID || store.Read().RPCState.Revision != state.Revision {
			t.Fatal("restart duplicated adopted profile")
		}
		if owner == "" {
			_, err := m.listProfilesAs(local.Peer{Identity: "uid:1000"}, &ipc.ListProfilesRequest{})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
		}
	}
}

func TestRPCInitialProfileAdoptionRejectsAmbiguousOrigin(t *testing.T) {
	for _, urls := range [][]string{nil, {"https://one.test", "https://two.test"}, {"https://user:password@control.test"}, {"http://control.test"}} {
		m := newRPCStoreTest(t)
		if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "existing"; cfg.ControlPlaneURLs = urls; return nil }); err != nil {
			t.Fatal(err)
		}
		before := m.store.Read()
		if err := m.AdoptInitialProfile(); err == nil {
			t.Fatal("ambiguous/unsafe origin accepted")
		}
		if !reflect.DeepEqual(before, m.store.Read()) {
			t.Fatal("rejected adoption changed state")
		}
	}
}
