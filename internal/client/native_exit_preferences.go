package client

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// ApplyPreferenceCandidateLocked applies only the candidate of an already
// dispatched durable preference/resource transaction. The caller holds the
// shared effect lock through this call and the worker's durable completion.
// This method neither completes the operation nor changes saved preferences.
func (r *NativeExitRuntime) ApplyPreferenceCandidateLocked(ctx context.Context, lock *sync.Mutex, candidate Config) error {
	stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
	if r == nil || !r.BoundTo(r.engine, lock, r.store) {
		return stale()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before := clonePersistentConfig(r.store.Read())
	expected, err := nativeExitPreferenceCandidate(before, r.executor.InterfaceName)
	if err != nil || !reflect.DeepEqual(clonePersistentConfig(candidate), expected) {
		return stale()
	}
	e := r.engine
	e.mu.Lock()
	guard := e.exitGuard
	_, scopeErr := nativeExitMaintenanceProfile(before, before.ExitSelection, guard)
	live := e.configured || e.device != nil || e.tun != nil || e.router != nil
	owned := scopeErr == nil && !e.runtimeSuspended
	if live {
		owned = owned && e.configured && e.device != nil && e.tun != nil && e.router != nil && reflect.DeepEqual(e.exitSelection, before.ExitSelection) && sameExitControlIdentity(before, e.exitConfig) && e.runtimeIdentity == nativeExitAppliedIdentity(before, *before.CachedMap, e.opts.Interface)
	}
	e.mu.Unlock()
	if !owned {
		return stale()
	}
	n := &nativeExitExecutor{engine: e}
	apply, cancel := context.WithTimeout(ctx, 10*time.Second)
	result, applyErr := e.configureExit(apply, expected, *expected.CachedMap, expected.ExitSelection, guard)
	if applyErr == nil && result.OK {
		var observed *ipc.ExitNodeStatus
		observed, applyErr = n.observeSelection(apply, expected, expected.ExitSelection, expected.RPCState.ActiveProfileID, guard)
		if applyErr == nil && !exitAppliedResultMatches(&clientRPCExitChange{ProfileID: expected.RPCState.ActiveProfileID, Requested: expected.ExitSelection}, observed) {
			applyErr = errNativeExitResume
		}
	} else if applyErr == nil {
		applyErr = errNativeExitResume
	}
	timedOut := apply.Err()
	cancel()
	changed := !reflect.DeepEqual(exitConfigWithoutLANJournal(before), exitConfigWithoutLANJournal(r.store.Read()))
	if applyErr != nil || timedOut != nil || changed || ctx.Err() != nil {
		// Always withdraw the attempted runtime, even when the latest durable
		// selection still matches. Maintenance alone could observe that selection
		// and leave an uncommitted candidate open after cancellation.
		_ = n.containFailure(ctx, guard)
		if err := ctx.Err(); err != nil {
			return err
		}
		if changed {
			return stale()
		}
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_APPLY_FAILED)
	}
	return nil
}

func nativeExitPreferenceCandidate(cfg Config, iface string) (Config, error) {
	invalid := errNativeExitResume
	if cfg.RPCState == nil || cfg.RPCState.NetworkPreferenceChange == nil {
		return Config{}, invalid
	}
	plan := cfg.RPCState.NetworkPreferenceChange
	if plan.OperationID == "" || plan.Containing {
		return Config{}, invalid
	}
	found := false
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return Config{}, invalid
		}
		if op.Id != plan.OperationID {
			continue
		}
		kind := plan.ResourceID != "" && op.Kind == ipc.OperationKind_OPERATION_KIND_SET_RESOURCE_ENABLED || plan.ResourceID == "" && (op.Kind == ipc.OperationKind_OPERATION_KIND_SET_PREFERENCES || op.Kind == ipc.OperationKind_OPERATION_KIND_RESET_PREFERENCES)
		if found || !kind || record.CompletedAt != nil || !strings.EqualFold(record.Owner, plan.OwnerID) || op.ProfileId != plan.ProfileID || op.State != ipc.OperationState_OPERATION_STATE_RUNNING {
			return Config{}, invalid
		}
		found = true
	}
	if !found {
		return Config{}, invalid
	}
	// Only this authenticated transaction barrier is removed from the temporary
	// context check; every other resume barrier remains authoritative.
	projected := clonePersistentConfig(cfg)
	projected.RPCState.NetworkPreferenceChange = nil
	if !nativeExitResumeContext(projected, iface) || ValidateConfigCurrentDevice(cfg) != nil {
		return Config{}, invalid
	}
	m := &ClientRPCMutations{now: time.Now}
	candidate, err := m.networkPreferenceCandidate(clonePersistentConfig(cfg), plan)
	if err != nil {
		return Config{}, err
	}
	if _, err := exitRoutePeers(candidate, *candidate.CachedMap, candidate.ExitSelection, time.Now()); err != nil {
		return Config{}, err
	}
	return clonePersistentConfig(candidate), nil
}
