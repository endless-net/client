package client

import (
	"context"
	"reflect"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRuntimeStartupRetryCommitsAuthorityIntentAndEventsTogether(t *testing.T) {
	for _, scenario := range []string{"success", "stay_disconnected", "tampered", "expired", "disconnect", "pending", "enrollment_recovery", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			valid := m.store.Read().CachedMap
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap = nil
				cfg.NodeCredential = "synthetic-credential"
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect"}
				if scenario == "stay_disconnected" {
					cfg.ConnectionIntent = nil
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			if scenario == "pending" {
				if _, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
					t.Fatal(err)
				}
			}
			before := m.store.Read()
			if scenario == "enrollment_recovery" {
				if err := m.store.Update(func(cfg *Config) error {
					cfg.EnrollmentRecovery = &EnrollmentRecovery{Phase: RecoveryPhaseNeedsLogin}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				before = m.store.Read()
			}
			candidate := before
			candidate.CachedMap = valid
			candidate.MapHash = valid.MapSignature.PayloadHash
			if scenario == "tampered" {
				candidate.CachedMap.Network.Name = "tampered"
			}
			if scenario == "expired" {
				m.now = func() time.Time { return valid.MapSignature.ExpiresAt }
			}
			if scenario == "disconnect" {
				if _, err := m.disconnectAs(owner, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile}); err != nil {
					t.Fatal(err)
				}
			}
			sub, err := m.subscribe(owner, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if _, err := sub.next(ctx); err != nil {
				t.Fatal(err)
			}
			unchanged := m.store.Read()
			if scenario == "cancelled" {
				cancel()
			}
			err = m.RecoverStartupPolicy(ctx, before, candidate)
			if scenario != "success" && scenario != "stay_disconnected" {
				if err == nil || !reflect.DeepEqual(unchanged, m.store.Read()) {
					t.Fatal("rejected retry changed authority or intent", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			final := m.store.Read()
			wantIntent := ConnectionIntentDesiredConnected
			wantPublicIntent := ipc.DesiredState_DESIRED_STATE_CONNECTED
			if scenario == "stay_disconnected" {
				wantIntent = ConnectionIntentDesiredDisconnected
				wantPublicIntent = ipc.DesiredState_DESIRED_STATE_DISCONNECTED
			}
			if final.RPCState.Revision != before.RPCState.Revision+1 || final.CachedMap == nil || final.ConnectionIntent.DesiredState != wantIntent || final.ConnectionIntent.StartupRecovery != nil || RuntimeStartRecoveryReady(final) {
				t.Fatal("retry did not commit authority and intent together")
			}
			for {
				event, err := sub.next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if event.Metadata.Revision != final.RPCState.Revision {
					t.Fatal("retry events used a different revision")
				}
				if event.GetInvalidated().GetDomain() == ipc.Domain_DOMAIN_PREFERENCES {
					break
				}
			}
			snapshot, err := m.snapshotAs(owner, nil)
			if err != nil || snapshot.Status.GetIntent().GetDesiredState() != wantPublicIntent || snapshot.Status.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED {
				t.Fatal("retry fabricated applied network state", err)
			}
		})
	}
}
