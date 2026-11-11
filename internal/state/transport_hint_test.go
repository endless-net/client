package state

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

func transportHintFixture(t *testing.T) (*Store, *memoryBackend, TransportHint) {
	t.Helper()
	s, backend, now := registeredStateFixture(t)
	v, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	local, err := ecdh.X25519().NewPrivateKey(v.Profiles[profileID].Keys.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	remote, err := ecdh.X25519().NewPrivateKey(peer.WireGuardPrivate)
	if err != nil {
		t.Fatal(err)
	}
	key := TransportHintKey{LocalProfileID: profileID, AccountID: "account", NetworkID: "network", LocalNodeID: "node", PeerNodeID: "peer", LocalPublicKey: base64.StdEncoding.EncodeToString(local.PublicKey().Bytes()), PeerPublicKey: base64.StdEncoding.EncodeToString(remote.PublicKey().Bytes()), ContextDigest: strings.Repeat("1", 64), Engine: transport.WireGuard, ProtocolVersion: "1", TransportProfileID: "pair-profile", ProfileVersion: transport.Version, ProfileDigest: strings.Repeat("2", 64)}
	return s, backend, TransportHint{Key: key, SucceededAt: now, ExpiresAt: now.Add(TransportHintTTL), RTT: 20 * time.Millisecond, StableSpan: 5 * time.Second, SuccessfulProbes: 3}
}

func mutateTransportHints(t *testing.T, s *Store, fn func(*State) error) error {
	t.Helper()
	v, err := s.Load(t.Context())
	if err != nil {
		return err
	}
	return s.Update(t.Context(), v.Revision, fn)
}

func TestTransportHintRestartPriorityNeverRenewsTTL(t *testing.T) {
	s, backend, h := transportHintFixture(t)
	if err := mutateTransportHints(t, s, func(v *State) error { return v.RecordTransportSuccess(h, h.SucceededAt) }); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(backend, s.protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, elapsed := range []time.Duration{time.Hour, 23 * time.Hour, 24 * time.Hour} {
		if err := mutateTransportHints(t, restarted, func(v *State) error {
			keys, err := v.ReconcileTransportHints(profileID, h.Key.ContextDigest, []TransportHintKey{h.Key}, h.SucceededAt.Add(elapsed))
			if err != nil {
				return err
			}
			if len(keys) != 1 && elapsed < TransportHintTTL || len(keys) != 0 && elapsed >= TransportHintTTL {
				t.Fatal("TTL changed on priority read/restart")
			}
			if len(v.TransportHints) != 0 && !v.TransportHints[0].ExpiresAt.Equal(h.ExpiresAt) {
				t.Fatal("read renewed original success expiry")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := mutateTransportHints(t, restarted, func(v *State) error {
		keys, err := v.ReconcileTransportHints(profileID, h.Key.ContextDigest, []TransportHintKey{h.Key}, h.SucceededAt.Add(23*time.Hour))
		if len(keys) != 0 || len(v.TransportHints) != 0 {
			t.Fatal("expired priority revived after wall rollback")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTransportHintGlobalBoundEvictsOldestSuccessAndFailuresRemovePriority(t *testing.T) {
	s, _, h := transportHintFixture(t)
	if err := mutateTransportHints(t, s, func(v *State) error {
		for i := range MaxTransportHints + 1 {
			next := h
			next.Key.PeerNodeID = fmt.Sprintf("peer-%03d", i)
			next.SucceededAt = h.SucceededAt.Add(time.Duration(i) * time.Millisecond)
			next.ExpiresAt = next.SucceededAt.Add(TransportHintTTL)
			if err := v.RecordTransportSuccess(next, next.SucceededAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Load(t.Context())
	if err != nil || len(v.TransportHints) != MaxTransportHints {
		t.Fatal("global cache size not bounded", err)
	}
	for _, old := range v.TransportHints {
		if old.Key.PeerNodeID == "peer-000" {
			t.Fatal("oldest success not evicted")
		}
	}
	key := v.TransportHints[0].Key
	if err := s.Update(t.Context(), v.Revision, func(v *State) error { v.ForgetTransportHint(key); return nil }); err != nil {
		t.Fatal(err)
	}
	v, err = s.Load(t.Context())
	if err != nil || len(v.TransportHints) != MaxTransportHints-1 {
		t.Fatal("failed profile retained priority", err)
	}
}

func TestTransportHintReconciliationInvalidatesCurrentBindingChanges(t *testing.T) {
	for _, name := range []string{"context", "identity", "node", "profile", "version", "removed-peer", "revoked", "unknown-context"} {
		t.Run(name, func(t *testing.T) {
			s, _, h := transportHintFixture(t)
			if err := mutateTransportHints(t, s, func(v *State) error { return v.RecordTransportSuccess(h, h.SucceededAt) }); err != nil {
				t.Fatal(err)
			}
			next := h.Key
			current := []TransportHintKey{next}
			switch name {
			case "context":
				next.ContextDigest = strings.Repeat("3", 64)
			case "identity":
				next.PeerPublicKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			case "node":
				next.PeerNodeID = "replacement"
			case "profile":
				next.ProfileDigest = strings.Repeat("4", 64)
			case "version":
				next.Engine, next.ProtocolVersion = transport.AmneziaWG, "3.1"
			case "removed-peer", "revoked":
				current = nil
			case "unknown-context":
				next.ContextDigest, current = "", nil
			}
			if current != nil {
				current[0] = next
			}
			if err := mutateTransportHints(t, s, func(v *State) error {
				keys, err := v.ReconcileTransportHints(profileID, next.ContextDigest, current, h.SucceededAt.Add(time.Second))
				if len(keys) != 0 || len(v.TransportHints) != 0 {
					t.Fatal("changed/removed binding inherited priority")
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTransportHintLostWriteACKAndStaleRevisionPreserveOriginal(t *testing.T) {
	for _, after := range []bool{false, true} {
		s, backend, h := transportHintFixture(t)
		before, err := s.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		backend.failAfter, backend.failBefore = after, !after
		if err := s.Update(t.Context(), before.Revision, func(v *State) error { return v.RecordTransportSuccess(h, h.SucceededAt) }); !errors.Is(err, ErrUncertain) {
			t.Fatal("lost ACK reported success", err)
		}
		backend.failAfter, backend.failBefore = false, false
		value, err := s.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if after && (len(value.TransportHints) != 1 || !value.TransportHints[0].ExpiresAt.Equal(h.ExpiresAt)) || !after && len(value.TransportHints) != 0 {
			t.Fatal("uncertain hint write fabricated/refreshed outcome")
		}
		if after {
			called := false
			if err := s.Update(t.Context(), before.Revision, func(*State) error { called = true; return nil }); !errors.Is(err, ErrStale) || called {
				t.Fatal("stale hint writer overwrote newer state", err)
			}
		}
	}
}

func TestTransportHintInvalidMetadataCannotBecomeProtectedState(t *testing.T) {
	s, _, h := transportHintFixture(t)
	for _, change := range []func(*TransportHint){
		func(h *TransportHint) { h.Key.AccountID = "foreign" },
		func(h *TransportHint) { h.Key.LocalPublicKey = h.Key.PeerPublicKey },
		func(h *TransportHint) { h.Key.ContextDigest = "192.0.2.1" },
		func(h *TransportHint) { h.Key.ProfileVersion++ },
		func(h *TransportHint) { h.Key.ProtocolVersion = "unknown" },
		func(h *TransportHint) { h.ExpiresAt = h.ExpiresAt.Add(time.Second) },
		func(h *TransportHint) { h.SuccessfulProbes = 2 },
		func(h *TransportHint) { h.StableSpan = 4999 * time.Millisecond },
		func(h *TransportHint) { h.RTT = -1 },
	} {
		invalid := h
		change(&invalid)
		if err := mutateTransportHints(t, s, func(v *State) error { return v.RecordTransportSuccess(invalid, h.SucceededAt) }); !errors.Is(err, ErrCorrupt) {
			t.Fatal("invalid hint metadata accepted", err)
		}
	}
	if err := mutateTransportHints(t, s, func(v *State) error {
		v.TransportHints = []TransportHint{h, h}
		v.HintObservedAt = h.SucceededAt
		return nil
	}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("duplicate priorities bypassed state validation", err)
	}
}

func TestV3UpgradePreservesRevocationAndConnectionWithEmptyHintCache(t *testing.T) {
	s, backend, now := registeredStateFixture(t)
	if err := connectionChange(s, func(c *ConnectionState) { _, _ = c.AcceptIntent(IntentConnected, now) }); err != nil {
		t.Fatal(err)
	}
	if err := mutateTransportHints(t, s, func(v *State) error {
		p := v.Profiles[profileID]
		command := "c086b75d-a2a6-4d4e-9bb8-6ce3bc0b674e"
		p.RemoteRevocations = map[string]RemoteRevocation{command: {CallerSID: v.OwnerSID, UserID: "user", ControlOrigin: p.ControlOrigin, CreatedAt: now, Request: contract.RevokeNodeRequest{AccountID: "account", NetworkID: "network", NodeID: "lost", CommandID: command, ExpectedRevision: 7}}}
		v.Profiles[profileID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	legacy := before
	legacy.Version = 3
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	backend.data, err = s.protector.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upgrade(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := s.Load(t.Context())
	if err != nil || after.Version != CurrentVersion || after.Revision != before.Revision+1 || !reflect.DeepEqual(before.Profiles, after.Profiles) || len(after.TransportHints) != 0 || !after.HintObservedAt.IsZero() {
		t.Fatal("v3 migration changed registration/intent/revocation or invented success", err)
	}
}
