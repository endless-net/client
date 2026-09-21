package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCProviderWorkersCancelContendedAcquisition(t *testing.T) {
	for _, stage := range []string{"session", "enrollment", "announcement", "recovery", "coordinator", "preparation", "registration"} {
		t.Run(stage, func(t *testing.T) {
			m := newRPCStoreTest(t)
			calls := 0
			var lock *sync.Mutex
			var run func(context.Context) error
			switch stage {
			case "session":
				lock = &m.sessionWorker
				provider := ClientRPCSessionRenewalProvider{
					Renew: func(context.Context, string, string, *backend.RenewSessionRequest) (*backend.RenewSessionResponse, error) {
						calls++
						return nil, nil
					},
					Poll: func(context.Context, string, string, *backend.GetSessionRenewalRequest) (*backend.GetSessionRenewalResponse, error) {
						calls++
						return nil, nil
					},
				}
				run = func(ctx context.Context) error { return m.ReconcileSessionRenewal(ctx, provider) }
			case "enrollment":
				lock = &m.enrollmentWorker
				run = func(ctx context.Context) error {
					return m.ReconcileEnrollment(ctx, func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error) {
						calls++
						return nil, nil
					})
				}
			case "announcement":
				lock = &m.trustWorker
				run = func(ctx context.Context) error {
					return m.ReconcileTrustAnnouncement(ctx, func(context.Context, Config) (api.SigningTrustBundle, error) {
						calls++
						return api.SigningTrustBundle{}, nil
					})
				}
			case "recovery":
				lock = &m.trustWorker
				run = func(ctx context.Context) error {
					return m.ReconcileTrustRecovery(ctx, func(context.Context, Config) (ClientRPCTrustRecoveryResult, error) {
						calls++
						return ClientRPCTrustRecoveryResult{}, nil
					})
				}
			case "coordinator":
				lock = &m.networkCoordinator
				run = func(ctx context.Context) error {
					return m.ReconcileNetworkSelection(ctx, ClientRPCProfileDriver{}, ClientRPCNetworkSelectionProviders{})
				}
			case "preparation":
				lock = &m.networkSelectionWorker
				run = func(ctx context.Context) error {
					return m.ReconcileNetworkSelectionPreparation(ctx, func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) { calls++; return nil, nil })
				}
			case "registration":
				lock = &m.networkSelectionWorker
				run = func(ctx context.Context) error {
					return m.ReconcileNetworkSelectionRegistration(ctx, func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) (*ipc.UserAction, error) {
						calls++
						return nil, nil
					})
				}
			}
			before := m.store.Read()
			lock.Lock()
			var once sync.Once
			release := func() { once.Do(lock.Unlock) }
			defer release()
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- run(ctx) }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("worker lost expired context", err)
				}
			case <-time.After(time.Second):
				release()
				<-done
				t.Fatal("cancelled worker waited for another provider's lock")
			}
			if calls != 0 || !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("contended cancellation called provider or changed durable state")
			}
			release()
			if err := run(t.Context()); err != nil {
				t.Fatal("retry could not reacquire worker lock", err)
			}
			if calls != 0 || !reflect.DeepEqual(before, m.store.Read()) {
				t.Fatal("empty retry manufactured provider work")
			}
		})
	}
}

func TestRPCEnrollmentLockCancellationPreservesDurableRestart(t *testing.T) {
	m, peer, request := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	called := false
	provider := func(_ context.Context, cfg Config, input ClientRPCEnrollmentInput, save func(Config) error) (*ipc.UserAction, error) {
		called = true
		if input.OperationID != op.Id {
			t.Error("restart replaced accepted enrollment")
		}
		cfg.NodeID = "recovered-node"
		cfg.CachedMap = &api.RegisterNodeResponse{Node: api.Node{ID: cfg.NodeID}}
		return nil, save(cfg)
	}
	m.enrollmentWorker.Lock()
	var once sync.Once
	release := func() { once.Do(m.enrollmentWorker.Unlock) }
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.ReconcileEnrollment(ctx, provider) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		release()
		<-done
		t.Fatal("cancelled enrollment did not leave lock wait")
	}
	if called || !reflect.DeepEqual(clonePersistentConfig(before), clonePersistentConfig(reopenRPCStoreFromDisk(t, m.store).Read())) {
		t.Fatal("cancelled wait dispatched or rewrote accepted enrollment")
	}
	release()
	restarted, err := NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ReconcileEnrollment(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	result, err := restarted.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || !called || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || restarted.store.Read().RPCState.Enrollment != nil {
		t.Fatal("restart could not complete original enrollment", err)
	}
}
