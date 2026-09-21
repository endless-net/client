package client

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeExitRuntimeConstructionAndBinding(t *testing.T) {
	engine := &WireGuardEngine{opts: WireGuardEngineOptions{Interface: "endlessnet"}}
	lock, store := &sync.Mutex{}, &ConfigStore{}
	handle, err := NewNativeExitRuntime(engine, lock, store)
	if runtime.GOOS != "linux" {
		if !errors.Is(err, ErrNativeExitUnsupported) || handle != nil {
			t.Fatal("unsupported platform constructed native handle", err)
		}
		return
	}
	if err != nil || !handle.BoundTo(engine, lock, store) {
		t.Fatal("native handle not bound", err)
	}
	if handle.BoundTo(new(WireGuardEngine), lock, store) || handle.BoundTo(engine, new(sync.Mutex), store) || handle.BoundTo(engine, lock, new(ConfigStore)) || handle.BoundTo(nil, lock, store) {
		t.Fatal("native handle accepted another runtime identity")
	}
	if _, err := NewNativeExitRuntime(engine, lock, nil); err == nil {
		t.Fatal("missing store accepted")
	}
}

func nativeExitRuntimeTestHandle(t *testing.T) (*NativeExitRuntime, *nativeExitExecutor) {
	t.Helper()
	n, cfg, _ := nativeExitResumeFixture(t)
	lock := new(sync.Mutex)
	executor, err := newNativeExitExecutorWithGuard(n.engine, lock, n.createGuard)
	if err != nil {
		t.Fatal(err)
	}
	return &NativeExitRuntime{engine: n.engine, lock: lock, store: &ConfigStore{config: clonePersistentConfig(cfg)}, executor: executor}, n
}

func TestNativeExitRuntimeResumeUsesRealSavedSelection(t *testing.T) {
	handle, n := nativeExitRuntimeTestHandle(t)
	handle.lock.Lock()
	defer handle.lock.Unlock()
	// A persisted snapshot without process-local store metadata remains valid.
	expected := clonePersistentConfig(handle.store.Read())
	if err := handle.ResumeSavedLocked(t.Context(), handle.lock, expected); err != nil {
		t.Fatal("saved runtime failed to resume", err)
	}
	if !n.engine.configured || n.engine.exitSelection == nil {
		t.Fatal("resume returned success without actual runtime")
	}
}

func TestNativeExitRuntimeResumeRejectsStaleInputsBeforeEffects(t *testing.T) {
	for _, scenario := range []string{"lock", "snapshot", "disconnected", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			handle, n := nativeExitRuntimeTestHandle(t)
			expected := handle.store.Read()
			lock := handle.lock
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "lock":
				lock = new(sync.Mutex)
			case "snapshot":
				expected.NetworkID = "changed"
			case "disconnected":
				handle.store.config.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
				expected = handle.store.Read()
			case "cancelled":
				cancel()
			}
			handle.lock.Lock()
			defer handle.lock.Unlock()
			if err := handle.ResumeSavedLocked(ctx, lock, expected); err == nil {
				t.Fatal("invalid resume accepted")
			}
			if n.engine.configured || n.engine.exitSelection != nil {
				t.Fatal("invalid input applied runtime")
			}
		})
	}
}

func TestNativeExitRuntimeResumeRechecksLatestStoreAndCancellation(t *testing.T) {
	for _, scenario := range []string{"disconnect", "cancel", "failure", "invalid_result"} {
		t.Run(scenario, func(t *testing.T) {
			handle, _ := nativeExitRuntimeTestHandle(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			maintained := false
			handle.executor.ResumeSaved = func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
				switch scenario {
				case "disconnect":
					handle.store.mu.Lock()
					handle.store.config.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
					handle.store.mu.Unlock()
				case "cancel":
					cancel()
				case "failure":
					return nil, errors.New("private native output")
				case "invalid_result":
					return nil, nil
				}
				cfg := handle.store.Read()
				return nativeExitSelectedStatus(cfg.RPCState.ActiveProfileID, cfg.ExitSelection), nil
			}
			handle.executor.Maintain = func(recovery context.Context, cfg Config) error {
				maintained = true
				if recovery.Err() != nil {
					t.Fatal("recovery inherited cancellation")
				}
				if _, bounded := recovery.Deadline(); !bounded {
					t.Fatal("recovery unbounded")
				}
				if scenario == "disconnect" && cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
					t.Fatal("maintenance received stale connection intent")
				}
				return errors.New("private containment output")
			}
			handle.lock.Lock()
			defer handle.lock.Unlock()
			err := handle.ResumeSavedLocked(ctx, handle.lock, handle.store.Read())
			if err == nil || !maintained {
				t.Fatal("failed/stale resume escaped maintenance")
			}
			switch scenario {
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			case "disconnect":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			default:
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
			}
		})
	}
}

func TestNativeExitRuntimeWorkerRejectsForeignStoreOrLock(t *testing.T) {
	handle, _ := nativeExitRuntimeTestHandle(t)
	m, _, _ := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	if _, err := s.StartNativeExitWorker(t.Context(), handle, handle.lock); err == nil {
		t.Fatal("foreign durable store accepted")
	}
	handle.store = m.store
	if _, err := s.StartNativeExitWorker(t.Context(), handle, new(sync.Mutex)); err == nil {
		t.Fatal("foreign effect lock accepted")
	}
	if s.exitWorker != nil {
		t.Fatal("invalid worker started")
	}
	if len(m.capabilityWorkers) != 0 {
		t.Fatal("invalid handle advertised capability")
	}
}

func TestNativeExitRuntimeCapabilityOwnedByPublicWorker(t *testing.T) {
	handle, _ := nativeExitRuntimeTestHandle(t)
	m, _, _ := rpcConnectFixture(t)
	handle.store = m.store
	s := NewClientRPCService(m, nil)
	var previous *clientRPCProfileWorker
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithCancel(t.Context())
		done, err := s.StartNativeExitWorker(ctx, handle, handle.lock)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		s.exitMu.Lock()
		worker := s.exitWorker
		s.exitMu.Unlock()
		m.mu.Lock()
		advertised := m.capabilityWorkers[ipc.Capability_CAPABILITY_EXIT_NODE]
		m.mu.Unlock()
		if worker == nil || advertised != worker {
			cancel()
			t.Fatal("public native worker did not own capability")
		}
		if previous != nil {
			// A delayed cancellation callback from the old public worker must not
			// withdraw the newly installed native executor's capability.
			m.setWorkerCapabilities(previous, false, ipc.Capability_CAPABILITY_EXIT_NODE)
			m.mu.Lock()
			stillOwned := m.capabilityWorkers[ipc.Capability_CAPABILITY_EXIT_NODE] == worker
			m.mu.Unlock()
			if !stillOwned {
				cancel()
				t.Fatal("old worker removed replacement capability")
			}
		}
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("native worker did not join")
		}
		m.mu.Lock()
		remaining := m.capabilityWorkers[ipc.Capability_CAPABILITY_EXIT_NODE]
		m.mu.Unlock()
		if remaining != nil {
			t.Fatal("stopped native worker retained capability")
		}
		previous = worker
	}
}
