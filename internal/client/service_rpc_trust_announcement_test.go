package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCTrustAnnouncementCheckpointAndRejections(t *testing.T) {
	for _, scenario := range []string{"match", "changed announcement", "provider failure", "cancel", "changed local authority"} {
		t.Run(scenario, func(t *testing.T) {
			newBundle := func() clientapi.SigningTrustBundle {
				public, _, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				bundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
				if err != nil {
					t.Fatal(err)
				}
				return bundle
			}
			trusted, announced := newBundle(), newBundle()
			m, peer, profile := rpcConnectFixture(t)
			peer.Administrator = true
			if err := m.store.Update(func(cfg *Config) error {
				cfg.MapSigningTrust = &trusted
				cfg.NodeCredential = "synthetic-authority"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			origin := m.store.Read().RPCState.Profiles[profile.ProfileId].ControlOrigin
			id, err := rpcIdentityAnnouncementID(profile.ProfileId, origin, trusted, announced)
			if err != nil {
				t.Fatal(err)
			}
			op, err := m.trustServerIdentityAs(peer, &ipc.TrustServerIdentityRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ConfirmedControlOrigin: origin, ConfirmedKeyId: announced.ActiveKeyID, ConfirmedAnnouncementId: id})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err = m.ReconcileTrustAnnouncement(ctx, func(_ context.Context, cfg Config) (clientapi.SigningTrustBundle, error) {
				if cfg.NodeCredential != "" || cfg.Token != "" {
					t.Fatal("inspection leaked credentials")
				}
				switch scenario {
				case "changed announcement":
					return newBundle(), nil
				case "provider failure":
					return clientapi.SigningTrustBundle{}, errors.New("private provider details")
				case "cancel":
					cancel()
				case "changed local authority":
					if err := m.store.Update(func(cfg *Config) error { cfg.Token = "replacement-session"; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				return announced, nil
			})
			if scenario == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				if err := m.ReconcileTrustAnnouncement(t.Context(), func(context.Context, Config) (clientapi.SigningTrustBundle, error) { return announced, nil }); err != nil {
					t.Fatal(err)
				}
			}
			disk, err := loadConfigFile(m.store.path)
			if err != nil {
				t.Fatal(err)
			}
			if disk.MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID || disk.NodeCredential != "synthetic-authority" || disk.EnrollmentRecovery != nil {
				t.Fatal("announcement check applied trust or cleanup")
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "match" || scenario == "cancel" {
				if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || disk.RPCState.Trust == nil || disk.RPCState.Trust.Announced.ActiveKeyID != announced.ActiveKeyID {
					t.Fatal("verified announcement checkpoint missing")
				}
			} else if result.State != ipc.OperationState_OPERATION_STATE_FAILED || disk.RPCState.Trust != nil {
				t.Fatal("failed confirmation retained executable plan")
			}
		})
	}
}
