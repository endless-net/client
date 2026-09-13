package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCTrustAdoptionDurability(t *testing.T) {
	for _, scenario := range []string{"enrolled", "unenrolled", "unchanged", "unchanged disconnected", "down failure", "restart during down", "changed authority", "changed trust", "change during down", "not verified"} {
		t.Run(scenario, func(t *testing.T) {
			bundle := func() clientapi.SigningTrustBundle {
				public, _, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				value, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			trusted, announced := bundle(), bundle()
			unchanged := scenario == "unchanged" || scenario == "unchanged disconnected"
			if unchanged {
				announced = trusted
			}
			m, peer, profile := rpcConnectFixture(t)
			peer.Administrator = true
			credential := "synthetic-current-node"
			if scenario == "unenrolled" {
				credential = ""
			}
			if err := m.store.Update(func(cfg *Config) error {
				cfg.MapSigningTrust, cfg.NodeCredential = &trusted, credential
				if scenario == "unchanged disconnected" {
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
				}
				p := cfg.RPCState.Profiles[profile.ProfileId]
				p.Configuration.MapSigningTrust, p.Configuration.NodeCredential = &trusted, credential
				cfg.RPCState.Profiles[p.ID] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			origin := m.store.Read().RPCState.Profiles[profile.ProfileId].ControlOrigin
			id, err := rpcIdentityAnnouncementID(profile.ProfileId, origin, trusted, announced)
			if err != nil {
				t.Fatal(err)
			}
			request := &ipc.TrustServerIdentityRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ConfirmedControlOrigin: origin, ConfirmedKeyId: announced.ActiveKeyID, ConfirmedAnnouncementId: id}
			op, err := m.trustServerIdentityAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "not verified" {
				if err := m.ReconcileTrustAnnouncement(t.Context(), func(context.Context, Config) (clientapi.SigningTrustBundle, error) { return announced, nil }); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "changed authority" || scenario == "changed trust" {
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "changed authority" {
						cfg.Token = "synthetic-replacement-session"
					} else {
						replacement := bundle()
						cfg.MapSigningTrust = &replacement
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.store.Read()
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			driver := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				calls++
				disk, err := loadConfigFile(m.store.path)
				if err != nil {
					t.Fatal(err)
				}
				if !disk.RPCState.Trust.DownStarted || disk.MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID || disk.EnrollmentRecovery != nil {
					t.Fatal("Down lacks durable checkpoint or trust changed prematurely")
				}
				if scenario == "down failure" {
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("private driver details")
				}
				if scenario == "restart during down" && calls == 1 {
					cancel()
				}
				if scenario == "change during down" {
					if err := m.store.Update(func(cfg *Config) error { cfg.Token = "synthetic-new-session"; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
			}}
			err = m.ReconcileTrustAdoption(ctx, driver)
			if scenario == "not verified" {
				if err == nil || calls != 0 {
					t.Fatal("unverified trust executed")
				}
				return
			}
			if scenario == "restart during down" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if cfg := m.store.Read(); cfg.EnrollmentRecovery != nil || cfg.MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID {
					t.Fatal("canceled Down adopted trust")
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				err = m.ReconcileTrustAdoption(t.Context(), driver)
			}
			if err != nil {
				t.Fatal(err)
			}
			disk, err := loadConfigFile(m.store.path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			success := scenario == "enrolled" || scenario == "unenrolled" || scenario == "restart during down"
			if unchanged {
				if calls != 0 || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || result.GetChange() == nil || result.GetChange().Changed || result.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED || disk.RPCState.Trust != nil || disk.EnrollmentRecovery != nil || !reflect.DeepEqual(disk.MapSigningTrust, &trusted) {
					t.Fatal("unchanged trust disrupted the connection")
				}
				if !reflect.DeepEqual(disk.ConnectionIntent, before.ConnectionIntent) || disk.NodeCredential != before.NodeCredential {
					t.Fatal("unchanged trust changed user intent or enrollment")
				}
				// Reopen the persisted journal: exact retry must return the old
				// result even though its original CAS belongs to the old instance.
				restartedStore := reopenRPCStoreFromDisk(t, m.store)
				m, err = NewClientRPCMutations(restartedStore)
				if err != nil {
					t.Fatal(err)
				}
				replay, err := m.trustServerIdentityAs(peer, request)
				if err != nil || !proto.Equal(replay, result) {
					t.Fatal("restarted unchanged trust replay lost its terminal result")
				}
				// A new confirmation is a separate successful no-op, not a
				// request to resume recovery or touch an absent tunnel engine.
				request.Mutation = rpcCreateRequest(t, m).Mutation
				next, err := m.trustServerIdentityAs(peer, request)
				if err != nil {
					t.Fatal(err)
				}
				if err := m.ReconcileTrustAnnouncement(t.Context(), func(context.Context, Config) (clientapi.SigningTrustBundle, error) { return announced, nil }); err != nil {
					t.Fatal(err)
				}
				noEngine := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					t.Error("unchanged confirmation attempted to stop an absent engine")
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("engine unavailable")
				}}
				if err := m.ReconcileTrustAdoption(t.Context(), noEngine); err != nil {
					t.Fatal(err)
				}
				next, err = m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: next.Id}})
				after := m.store.Read()
				if err != nil || next.Id == result.Id || next.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED ||
					next.GetChange() == nil || next.GetChange().Changed || after.EnrollmentRecovery != nil ||
					!reflect.DeepEqual(after.ConnectionIntent, before.ConnectionIntent) || after.NodeCredential != before.NodeCredential {
					t.Fatal("fresh unchanged confirmation restarted recovery or lost disconnected intent")
				}
				return
			}
			if !success {
				if result.State != ipc.OperationState_OPERATION_STATE_FAILED || disk.RPCState.Trust != nil || !reflect.DeepEqual(disk.MapSigningTrust, before.MapSigningTrust) || disk.EnrollmentRecovery != nil {
					t.Fatal("failed adoption changed trust or retained plan")
				}
				if (scenario == "changed authority" || scenario == "changed trust") && calls != 0 {
					t.Fatal("stale confirmation stopped tunnel")
				}
				if scenario == "down failure" && result.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED {
					t.Fatal("wrong Down failure")
				}
				return
			}
			if !reflect.DeepEqual(disk.MapSigningTrust, &announced) || !reflect.DeepEqual(disk.RPCState.Profiles[profile.ProfileId].Configuration.MapSigningTrust, &announced) || disk.NodeCredential != credential {
				t.Fatal("trust/profile/credential atomicity violated")
			}
			if scenario == "unenrolled" {
				if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !result.GetChange().Changed || disk.RPCState.Trust != nil || disk.EnrollmentRecovery != nil {
					t.Fatal("unenrolled trust not completed")
				}
			} else {
				if result.State != ipc.OperationState_OPERATION_STATE_RUNNING || disk.RPCState.Trust == nil || !disk.RPCState.Trust.Adopted || disk.EnrollmentRecovery == nil || disk.EnrollmentRecovery.OperationID != op.Id || disk.EnrollmentRecovery.IdempotencyID != op.Id || !reflect.DeepEqual(disk.EnrollmentRecovery, disk.RPCState.Profiles[profile.ProfileId].Configuration.EnrollmentRecovery) {
					t.Fatal("durable recovery not paired with trust")
				}
				previousCalls := calls
				if err := m.ReconcileTrustAdoption(t.Context(), driver); err != nil || calls != previousCalls {
					t.Fatal("adopted checkpoint repeated Down", err)
				}
			}
			if scenario == "restart during down" && (calls != 2 || result.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN) {
				t.Fatal("restart claimed preserved continuity")
			}
			if m.observedStatus.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
				t.Fatal("completed Down not reflected in status")
			}
		})
	}
}
