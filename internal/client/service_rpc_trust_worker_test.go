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
)

func TestRPCTrustWorkerRecoveryAndIndependentDisconnect(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	peer.Administrator = true
	makeBundle := func() clientapi.SigningTrustBundle {
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
	trusted, announced := makeBundle(), makeBundle()
	if err := m.store.Update(func(cfg *Config) error {
		cfg.MapSigningTrust = &trusted
		cfg.NodeCredential = "synthetic-original"
		cfg.NetworkID = "network"
		cfg.CachedMap.Node.ID, cfg.CachedMap.Network.ID = cfg.NodeID, cfg.NetworkID
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
	s := NewClientRPCService(m, nil)
	driver := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	if _, err := s.StartTrustWorker(t.Context(), driver); err == nil {
		t.Fatal("missing providers accepted")
	}
	s.ServerIdentityProvider = func(context.Context, Config) (clientapi.SigningTrustBundle, error) { return announced, nil }
	entered := make(chan struct{})
	assertDurableRecovery := func(input Config) {
		disk, err := loadConfigFile(m.store.path)
		if err != nil {
			t.Error("provider cannot read durable trust checkpoint")
			return
		}
		recovery := disk.EnrollmentRecovery
		if recovery == nil || recovery.OperationID != op.Id || recovery.IdempotencyID != op.Id ||
			!reflect.DeepEqual(disk.MapSigningTrust, &announced) || !reflect.DeepEqual(input.MapSigningTrust, disk.MapSigningTrust) ||
			!reflect.DeepEqual(input.EnrollmentRecovery, recovery) || disk.NodeCredential != "synthetic-original" ||
			disk.RPCState.Trust == nil || !disk.RPCState.Trust.Adopted || disk.RPCState.Trust.OperationID != op.Id {
			t.Error("recovery provider ran before durable trust and renewal identity were committed")
			return
		}
		profileState := disk.RPCState.Profiles[profile.ProfileId].Configuration
		if !reflect.DeepEqual(profileState.MapSigningTrust, disk.MapSigningTrust) || !reflect.DeepEqual(profileState.EnrollmentRecovery, recovery) {
			t.Error("recovery provider observed divergent active and profile trust checkpoints")
		}
	}
	s.TrustRecoveryProvider = func(ctx context.Context, input Config) (ClientRPCTrustRecoveryResult, error) {
		assertDurableRecovery(input)
		close(entered)
		<-ctx.Done()
		return ClientRPCTrustRecoveryResult{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done, err := s.StartTrustWorker(ctx, driver)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer cancel()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("recovery provider not entered")
	}
	if _, err := s.StartTrustWorker(ctx, driver); err == nil {
		t.Fatal("duplicate worker accepted")
	}
	// A blocked remote recovery owns neither the profile nor tunnel lock.
	disconnect, err := m.disconnectAs(peer, &ipc.DisconnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- m.ReconcileDisconnect(ctx, driver) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trust network work blocked Disconnect")
	}
	disconnected, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: disconnect.Id}})
	if err != nil || disconnected.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("Disconnect not completed", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trust worker did not stop")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	s = NewClientRPCService(m, nil)
	s.ServerIdentityProvider = func(context.Context, Config) (clientapi.SigningTrustBundle, error) {
		t.Error("adopted trust was re-fetched")
		return announced, nil
	}
	s.TrustRecoveryProvider = func(_ context.Context, cfg Config) (ClientRPCTrustRecoveryResult, error) {
		assertDurableRecovery(cfg)
		cfg.NodeCredential = "synthetic-renewed"
		now := time.Now().UTC()
		cfg.CachedMapSavedAt = &now
		return ClientRPCTrustRecoveryResult{Configuration: &cfg}, nil
	}
	ctx2, cancel2 := context.WithCancel(t.Context())
	done2, err := s.StartTrustWorker(ctx2, driver)
	if err != nil {
		cancel2()
		t.Fatal(err)
	}
	defer func() { cancel2(); <-done2 }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("restarted trust operation not completed")
		case <-tick.C:
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			if result.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				cfg := m.store.Read()
				if cfg.NodeCredential != "synthetic-renewed" || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
					t.Fatal("recovery lost Disconnect intent")
				}
				return
			}
		}
	}
}
