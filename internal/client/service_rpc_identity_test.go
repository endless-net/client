package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCIdentityBindsAnnouncementAndRejectsStaleRead(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	newTrust := func() clientapi.SigningTrustBundle {
		key, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(key))
		if err != nil {
			t.Fatal(err)
		}
		return bundle
	}
	trusted, announced := newTrust(), newTrust()
	if err := m.store.Update(func(cfg *Config) error {
		cfg.MapSigningTrust = &trusted
		cfg.Token = "synthetic-private-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	calls := 0
	s.ServerIdentityProvider = func(_ context.Context, cfg Config) (clientapi.SigningTrustBundle, error) {
		calls++
		if cfg.Token != "" || cfg.NodeCredential != "" || cfg.PrivateKey != "" || cfg.RPCState != nil {
			t.Fatal("provider received private state")
		}
		return announced, nil
	}
	req := &ipc.GetServerIdentityRequest{Profile: profile}
	_, err := s.serverIdentityAs(t.Context(), local.Peer{Identity: "unrelated"}, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if calls != 0 {
		t.Fatal("unauthorized lookup called provider")
	}
	first, err := s.serverIdentityAs(t.Context(), peer, req)
	if err != nil || !first.Identity.Changed || first.Identity.AnnouncementId == "" || first.Identity.TrustedKeyId != trusted.ActiveKeyID {
		t.Fatal("missing bound identity", err)
	}
	repeated, err := s.serverIdentityAs(t.Context(), peer, req)
	if err != nil || repeated.Identity.AnnouncementId != first.Identity.AnnouncementId {
		t.Fatal("unchanged announcement changed identity")
	}
	announced = newTrust()
	replaced, err := s.serverIdentityAs(t.Context(), peer, req)
	if err != nil || replaced.Identity.AnnouncementId == first.Identity.AnnouncementId {
		t.Fatal("new announcement reused confirmation identity")
	}
	s.ServerIdentityProvider = func(context.Context, Config) (clientapi.SigningTrustBundle, error) {
		if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.Revision++; return nil }); err != nil {
			t.Fatal(err)
		}
		return announced, nil
	}
	_, err = s.serverIdentityAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	s.ServerIdentityProvider = func(context.Context, Config) (clientapi.SigningTrustBundle, error) {
		return clientapi.SigningTrustBundle{}, errors.New("private provider detail")
	}
	_, err = s.serverIdentityAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	if m.store.Read().MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID {
		t.Fatal("lookup changed trust")
	}
}
