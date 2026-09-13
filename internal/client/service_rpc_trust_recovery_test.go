package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCTrustRecoveryTransactions(t *testing.T) {
	for _, scenario := range []string{"success", "retry", "terminal", "blocked", "binding", "cancel", "stale", "invalid result", "provider error"} {
		t.Run(scenario, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			peer.Administrator = true
			public, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
			if err != nil {
				t.Fatal(err)
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.MapSigningTrust = &bundle
				cfg.Token = "synthetic-session"
				cfg.NodeCredential = "synthetic-original"
				cfg.NetworkID = "network"
				cfg.CachedMap.Node.ID = cfg.NodeID
				cfg.CachedMap.Network.ID = cfg.NetworkID
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			origin := m.store.Read().RPCState.Profiles[profile.ProfileId].ControlOrigin
			id, err := rpcIdentityAnnouncementID(profile.ProfileId, origin, bundle, bundle)
			if err != nil {
				t.Fatal(err)
			}
			op, err := m.trustServerIdentityAs(peer, &ipc.TrustServerIdentityRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ConfirmedControlOrigin: origin, ConfirmedKeyId: bundle.ActiveKeyID, ConfirmedAnnouncementId: id})
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.ReconcileOperation(op.Id, func(cfg *Config, op *ipc.Operation) error {
				plan := cfg.RPCState.Trust
				plan.Announced, plan.DownStarted, plan.Adopted = &bundle, true, true
				recovery, err := NewEnrollmentRecovery(op.Id, op.Id, origin, bundle.ActiveKeyID, m.now())
				if err != nil {
					return err
				}
				cfg.EnrollmentRecovery = &recovery
				p := cfg.RPCState.Profiles[op.ProfileId]
				p.Configuration = clonePersistentConfig(*cfg)
				p.Configuration.RPCState = nil
				cfg.RPCState.Profiles[p.ID] = p
				op.State, op.Continuity = ipc.OperationState_OPERATION_STATE_RUNNING, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			before := m.store.Read()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			provider := func(context.Context, Config) (ClientRPCTrustRecoveryResult, error) {
				switch scenario {
				case "retry":
					return ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "temporary", Retryable: true}, Phase: RecoveryPhaseRecovering}, nil
				case "terminal":
					return ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, ReasonKey: "node_revoked", ControlRequestId: "correlation"}, RequiresEnrollment: true}, nil
				case "blocked":
					return ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED, ReasonKey: "policy"}, Phase: RecoveryPhasePolicyBlocked}, nil
				case "binding":
					return ClientRPCTrustRecoveryResult{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "node_identity_binding_mismatch", ControlRequestId: "binding-correlation"}, Phase: RecoveryPhaseBlocked}, nil
				case "provider error":
					return ClientRPCTrustRecoveryResult{}, errors.New("private provider details")
				case "cancel":
					cancel()
				case "stale":
					if err := m.store.Update(func(cfg *Config) error { cfg.NodeCredential = "synthetic-replacement"; return nil }); err != nil {
						t.Fatal(err)
					}
				case "invalid result":
					return ClientRPCTrustRecoveryResult{Configuration: &Config{}}, nil
				}
				next := clonePersistentConfig(before)
				next.NodeCredential = "synthetic-renewed"
				next.MapRevision++
				next.CachedMap.Network.Revision = next.MapRevision
				now := m.now()
				next.CachedMapSavedAt = &now
				// A provider must not be able to replace unrelated authority.
				next.Token, next.PrivateKey = "not-authorized", "not-authorized"
				next.MapSigningTrust, next.ConnectionIntent = nil, nil
				return ClientRPCTrustRecoveryResult{Configuration: &next}, nil
			}
			err = m.ReconcileTrustRecovery(ctx, provider)
			if scenario == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if scenario == "stale" || scenario == "invalid result" {
				if err == nil {
					t.Fatal("invalid/stale result accepted")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after, err := loadConfigFile(m.store.path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			if after.Token != before.Token || after.PrivateKey != before.PrivateKey || !reflect.DeepEqual(after.MapSigningTrust, before.MapSigningTrust) || !reflect.DeepEqual(after.ConnectionIntent, before.ConnectionIntent) {
				t.Fatal("recovery replaced unrelated authority")
			}
			if scenario == "terminal" || scenario == "binding" {
				code := ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT
				if scenario == "binding" {
					code = ipc.ErrorCode_ERROR_CODE_APPLY_FAILED
				}
				beforeConnect := m.store.Read()
				_, connectErr := m.connectAs(peer, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
				assertRPCFailure(t, connectErr, code)
				if !reflect.DeepEqual(beforeConnect, m.store.Read()) {
					t.Fatal("Connect changed authoritative recovery outcome")
				}
			}
			switch scenario {
			case "success":
				if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || after.NodeCredential != "synthetic-renewed" || after.EnrollmentRecovery != nil || after.RPCState.Trust != nil || after.RPCState.Profiles[profile.ProfileId].Configuration.NodeCredential != after.NodeCredential {
					t.Fatal("recovery not committed atomically")
				}
			case "terminal":
				if result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().ControlRequestId != "correlation" || after.NodeCredential != "" || after.CachedMap != nil || after.RPCState.Profiles[profile.ProfileId].Configuration.NodeCredential != "" {
					t.Fatal("terminal recovery did not clean both registrations")
				}
			case "retry":
				if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || !after.EnrollmentRecovery.Retryable || after.RPCState.Trust == nil || after.NodeCredential != before.NodeCredential {
					t.Fatal("retry lost durable authority")
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				if err := m.ReconcileTrustRecovery(t.Context(), provider); err != nil {
					t.Fatal("restart retry failed", err)
				}
			case "blocked", "binding", "provider error":
				if result.State != ipc.OperationState_OPERATION_STATE_FAILED || after.NodeCredential != before.NodeCredential || after.RPCState.Trust != nil || after.EnrollmentRecovery.Retryable {
					t.Fatal("blocked recovery altered registration")
				}
				if scenario == "binding" && (after.EnrollmentRecovery.Phase != RecoveryPhaseBlocked || result.GetFailure().GetControlRequestId() != "binding-correlation") {
					t.Fatal("binding failure lost recovery restriction or correlation")
				}
			default:
				if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || after.RPCState.Trust == nil || after.EnrollmentRecovery == nil {
					t.Fatal("interrupted result destroyed resumable operation")
				}
			}
		})
	}
}
