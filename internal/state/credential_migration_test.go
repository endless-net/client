package state

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestV6CredentialUpgradePreservesInitialDataWithoutInventingRenewal(t *testing.T) {
	store, backend, _ := registeredStateFixture(t)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	old := before
	old.Version = 6
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	backend.data, err = store.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != 7 || after.Revision != before.Revision+1 || !reflect.DeepEqual(before.Profiles, after.Profiles) || before.InstallationID != after.InstallationID || before.OwnerSID != after.OwnerSID {
		t.Fatal("upgrade changed registration, identity or initial credential")
	}
	p := after.Profiles[profileID]
	if p.ActiveSession != nil || p.ActiveNodeCredential != nil || p.SessionRefresh != nil || p.NodeRenewal != nil {
		t.Fatal("migration invented current authority or an operation")
	}
	if err = store.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load(t.Context())
	if err != nil || again.Revision != after.Revision {
		t.Fatal("migration repeated a completed upgrade", err)
	}
}
