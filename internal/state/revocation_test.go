package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestRevocationJournalCannotEraseRetargetOrRebaseOriginal(t *testing.T) {
	s, _, now := registeredStateFixture(t)
	value, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	const command = "c086b75d-a2a6-4d4e-9bb8-6ce3bc0b674e"
	request := contract.RevokeNodeRequest{AccountID: "account", NetworkID: "network", NodeID: "lost", CommandID: command, ExpectedRevision: 7}
	if err := s.Update(t.Context(), value.Revision, func(v *State) error {
		p := v.Profiles[profileID]
		p.RemoteRevocations = map[string]RemoteRevocation{command: {CallerSID: v.OwnerSID, UserID: "user", ControlOrigin: p.ControlOrigin, CreatedAt: now, Request: request}}
		v.Profiles[profileID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RemoteRevocation){func(r *RemoteRevocation) { r.Request.NodeID = "other" }, func(r *RemoteRevocation) { r.Request.ExpectedRevision++ }, func(r *RemoteRevocation) { r.UserID = "foreign" }, func(r *RemoteRevocation) { r.CreatedAt = r.CreatedAt.AddDate(0, 0, 1) }} {
		before, _ := s.Load(t.Context())
		if err := s.Update(t.Context(), before.Revision, func(v *State) error {
			p := v.Profiles[profileID]
			r := p.RemoteRevocations[command]
			mutate(&r)
			p.RemoteRevocations[command] = r
			v.Profiles[profileID] = p
			return nil
		}); !errors.Is(err, ErrCorrupt) {
			t.Fatal("changed durable revoke binding accepted", err)
		}
	}
	before, _ := s.Load(t.Context())
	if err := s.Update(t.Context(), before.Revision, func(v *State) error {
		p := v.Profiles[profileID]
		delete(p.RemoteRevocations, command)
		v.Profiles[profileID] = p
		return nil
	}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("unresolved original command erased", err)
	}
}

func TestV2UpgradePreservesKeysConnectionAndDoesNotInventRevoke(t *testing.T) {
	s, b, now := registeredStateFixture(t)
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy := before
	legacy.Version = 2
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	b.data, err = s.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := s.Load(t.Context())
	old, newProfile := before.Profiles[profileID], after.Profiles[profileID]
	if err != nil || after.Version != CurrentVersion || after.Revision != before.Revision+1 || !bytes.Equal(old.Keys.IdentityPrivate, newProfile.Keys.IdentityPrivate) || !bytes.Equal(old.Keys.WireGuardPrivate, newProfile.Keys.WireGuardPrivate) || *old.Connection != *newProfile.Connection || len(newProfile.RemoteRevocations) != 0 {
		t.Fatal("v2 migration reset data or created a remote command", err)
	}
}
