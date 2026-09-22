package client

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ErrNativeExitUnsupported identifies a platform without this native adapter.
var ErrNativeExitUnsupported = errors.New("native exit runtime requires Linux")

// NativeExitRuntime binds one concrete engine, durable store and shared effect
// lock. Its native callbacks stay private; construction starts no worker and
// advertises no capability. The host must restore protection before serving it.
type NativeExitRuntime struct {
	engine   *WireGuardEngine
	lock     *sync.Mutex
	store    *ConfigStore
	executor clientRPCExitExecutor
}

type nativeExitWorkerContextKey struct{}

func NewNativeExitRuntime(engine *WireGuardEngine, lock *sync.Mutex, store *ConfigStore) (*NativeExitRuntime, error) {
	if runtime.GOOS != "linux" {
		return nil, ErrNativeExitUnsupported
	}
	if store == nil {
		return nil, errors.New("native exit runtime requires a config store")
	}
	executor, err := newNativeExitExecutorWithStore(engine, lock, newPlatformExitGuard, store)
	if err != nil {
		return nil, err
	}
	return &NativeExitRuntime{engine: engine, lock: lock, store: store, executor: executor}, nil
}

// BoundTo validates the host's concrete identities without exposing callbacks.
func (r *NativeExitRuntime) BoundTo(engine *WireGuardEngine, lock *sync.Mutex, store *ConfigStore) bool {
	return r != nil && engine != nil && lock != nil && store != nil && r.engine == engine && r.lock == lock && r.store == store && r.executor.Lock == lock && r.executor.InterfaceName == engine.opts.Interface
}

// StartNativeExitWorker owns the adapter lifetime through the returned channel.
// The host must cancel and join it before closing the engine. The supplied lock
// must also be the lock used by ordinary connection and map application.
func (s *ClientRPCService) StartNativeExitWorker(ctx context.Context, r *NativeExitRuntime, lock *sync.Mutex) (<-chan error, error) {
	if s == nil || s.mutations == nil || r == nil || !r.BoundTo(r.engine, lock, s.mutations.store) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	// Bind the capability to this invocation even if its worker completes and a
	// replacement starts before we reacquire exitMu below.
	identity := new(int)
	workerContext := context.WithValue(ctx, nativeExitWorkerContextKey{}, identity)
	workerDone, err := s.startExitWorker(workerContext, r.executor)
	if err != nil {
		return nil, err
	}
	s.exitMu.Lock()
	worker := s.exitWorker
	if worker == nil || worker.ctx.Value(nativeExitWorkerContextKey{}) != identity {
		worker = nil
	} else if worker.ctx.Err() == nil {
		s.mutations.setWorkerCapabilities(worker, true, ipc.Capability_CAPABILITY_EXIT_NODE)
	}
	s.exitMu.Unlock()
	var clearOnce sync.Once
	clear := func() {
		clearOnce.Do(func() {
			if worker != nil {
				s.mutations.setWorkerCapabilities(worker, false, ipc.Capability_CAPABILITY_EXIT_NODE)
			}
		})
	}
	stopCancellation := func() bool { return true }
	if worker != nil {
		stopCancellation = context.AfterFunc(worker.ctx, clear)
	}
	done := make(chan error, 1)
	go func() {
		result := <-workerDone
		stopCancellation()
		clear()
		done <- result
		close(done)
	}()
	return done, nil
}

// ResumeSavedLocked is for a profile driver already holding the shared effect
// lock. It does not acquire that lock again. Pointer identity is checked; Go
// mutexes cannot establish which goroutine owns a lock, so holding it is a
// caller contract. The supplied configuration must still match the store.
func (r *NativeExitRuntime) ResumeSavedLocked(ctx context.Context, lock *sync.Mutex, expected Config) error {
	if r == nil || !r.BoundTo(r.engine, lock, r.store) || r.executor.ResumeSaved == nil || r.executor.Maintain == nil {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before := clonePersistentConfig(r.store.Read())
	if !reflect.DeepEqual(clonePersistentConfig(expected), before) || !nativeExitResumeContext(before, r.executor.InterfaceName) {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	apply, cancel := context.WithTimeout(ctx, 10*time.Second)
	observed, applyErr := r.executor.ResumeSaved(apply, clonePersistentConfig(before))
	applyContextErr := apply.Err()
	cancel()
	after := clonePersistentConfig(r.store.Read())
	changed := !reflect.DeepEqual(exitConfigWithoutLANJournal(before), exitConfigWithoutLANJournal(after))
	confirmed := applyErr == nil && applyContextErr == nil && exitAppliedResultMatches(&clientRPCExitChange{ProfileID: before.RPCState.ActiveProfileID, Requested: before.ExitSelection}, observed)
	if changed || !confirmed || ctx.Err() != nil {
		// A late cancellation must close even an otherwise valid application.
		// Maintenance could successfully observe it and leave LAN open.
		r.engine.mu.Lock()
		guard := r.engine.exitGuard
		r.engine.mu.Unlock()
		if guard != nil {
			_ = (&nativeExitExecutor{engine: r.engine}).containFailure(ctx, guard)
		}
		recovery, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_ = r.executor.Maintain(recovery, after)
		finish()
		if err := ctx.Err(); err != nil {
			return err
		}
		if changed {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
	}
	return nil
}
