package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func readyNetworkActivationFixture(t *testing.T) (*ClientRPCMutations, Config) {
	t.Helper()
	m, target := networkRegistrationExecutorFixture(t)
	err := m.ReconcileNetworkSelectionRegistration(t.Context(), func(_ context.Context, _ Config, _ ClientRPCNetworkRegistrationInput, save func(Config) error) (*ipc.UserAction, error) {
		return nil, save(target)
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, target
}

func TestNetworkActivationStopsSourceBeforeAtomicTargetAdoption(t *testing.T) {
	m, target := readyNetworkActivationFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect", StartupRecovery: &clientRuntimeStartRecovery{PreviousState: ConnectionIntentDesiredConnected}}
		cfg.RPCState.NetworkSelection.Source = networkSelectionContext(*cfg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := networkSelectionContext(m.store.Read())
	lock := &sync.Mutex{}
	stops := 0
	driver := ClientRPCProfileDriver{Lock: lock, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		if lock.TryLock() {
			lock.Unlock()
			t.Fatal("stop outside runtime operation lock")
		}
		cfg := m.store.Read()
		if !cfg.RPCState.NetworkSelection.DownStarted || cfg.RPCState.NetworkSelection.Activated || !reflect.DeepEqual(networkSelectionContext(cfg), source) {
			t.Fatal("source changed before stop or teardown barrier missing")
		}
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	m.observedStatus = &ipc.Status{ConnectionPhase: ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED}
	if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	target.ConnectionIntent = clonePersistentConfig(source).ConnectionIntent
	target.ConnectionIntent.StartupRecovery = nil
	if !reflect.DeepEqual(networkSelectionContext(cfg), target) || !cfg.RPCState.NetworkSelection.Activated || m.observedStatus != nil {
		t.Fatal("activation mixed contexts or retained source observation")
	}
	if !reflect.DeepEqual(cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID].Configuration, profileConfiguration(target)) {
		t.Fatal("active profile retained old network configuration")
	}
	assertNetworkActivationOperation(t, cfg, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED)
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err != nil || stops != 1 {
		t.Fatal("replayed activation stopped target or lost checkpoint", err)
	}
}

func TestNetworkActivationRetainsBarrierOnUncertainOrStaleStop(t *testing.T) {
	for _, mode := range []string{"stop_failure", "unconfirmed_stop", "cancel", "source_change", "target_expiry"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := readyNetworkActivationFixture(t)
			source := networkSelectionContext(m.store.Read())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := m.ReconcileNetworkSelectionActivation(ctx, ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				switch mode {
				case "stop_failure":
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("synthetic stop failure")
				case "unconfirmed_stop":
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED, nil
				case "cancel":
					cancel()
				case "source_change":
					if err := m.store.Update(func(cfg *Config) error {
						cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "new_disconnect"}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "target_expiry":
					m.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
				}
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}})
			cfg := m.store.Read()
			if err == nil || !cfg.RPCState.NetworkSelection.DownStarted || cfg.RPCState.NetworkSelection.Activated || cfg.NetworkID != source.NetworkID || cfg.NodeCredential != source.NodeCredential {
				t.Fatal("failed or stale teardown activated target or removed barrier")
			}
			if mode == "source_change" && cfg.ConnectionIntent.Reason != "new_disconnect" {
				t.Fatal("concurrent disconnect overwritten")
			}
		})
	}
}

func TestNetworkActivationResumesUncertainStopFromDisk(t *testing.T) {
	m, _ := readyNetworkActivationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		cancel() // Simulate process lifetime ending after route removal.
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	if err := m.ReconcileNetworkSelectionActivation(ctx, driver); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	store := reopenRPCStoreFromDisk(t, m.store)
	var err error
	m, err = NewClientRPCMutations(store)
	if err != nil {
		t.Fatal(err)
	}
	driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}
	if err := m.ReconcileNetworkSelectionActivation(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	if !m.store.Read().RPCState.NetworkSelection.Activated {
		t.Fatal("durable teardown failed to resume activation")
	}
	assertNetworkActivationOperation(t, m.store.Read(), ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN)
}

func assertNetworkActivationOperation(t *testing.T, cfg Config, continuity ipc.ConnectionContinuity) {
	t.Helper()
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if err := proto.Unmarshal(record.Operation, op); err != nil {
			t.Fatal(err)
		}
		if op.Id == cfg.RPCState.NetworkSelection.OperationID {
			if op.State != ipc.OperationState_OPERATION_STATE_RUNNING || op.Continuity != continuity || op.Outcome != nil {
				t.Fatal("activation claimed completion or incorrect continuity", op)
			}
			return
		}
	}
	t.Fatal("selection operation missing")
}
