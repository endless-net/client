package client

import (
	"context"
	"reflect"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// RuntimeLifecycleEvent is internal to the runtime/platform boundary. These
// events are deliberately absent from the public NotifyLifecycle RPC enum.
type RuntimeLifecycleEvent uint8

// RuntimeLifecycleNotification is produced only by trusted OS sources. The
// session owner is the identity captured for that session, never the UI caller.
type RuntimeLifecycleNotification struct {
	Event        RuntimeLifecycleEvent
	SessionOwner string
	// Completion is owned by an OS source that must retain its sleep inhibitor
	// until the runtime has confirmed the transition. It is never persisted.
	Completion chan<- error
	Deadline   time.Time
}

const (
	RuntimeUserLogoff RuntimeLifecycleEvent = iota + 1
	RuntimeSuspend
	RuntimeResume
	// Source health gates have no user-intent policy effect.
	RuntimeSourceLost
	RuntimeSourceRecovered
)

// ApplyRuntimeLifecycleIntent commits the policy decision before the platform
// worker performs teardown or releases its suspension gate. KEEP_INTENT never
// restores a snapshot taken before a newer Disconnect. This method does not
// establish that any OS effect has completed.
func (m *ClientRPCMutations) ApplyRuntimeLifecycleIntent(ctx context.Context, event RuntimeLifecycleEvent, sessionOwner string) error {
	if !lockExitRuntime(ctx, &m.mu) {
		return ctx.Err()
	}
	defer m.mu.Unlock()
	changed := false
	disconnect := false
	err := m.store.Update(func(cfg *Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var resolve func(Config, clientRPCProfile) (*ipc.LifecycleSetting, error)
		reason := ""
		switch event {
		case RuntimeUserLogoff:
			if sessionOwner == "" || cfg.LocalOwnerID == "" || !strings.EqualFold(sessionOwner, cfg.LocalOwnerID) {
				return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
			}
			resolve, reason = m.userLogoffSetting, "runtime_user_logoff"
		case RuntimeSuspend:
			resolve, reason = m.suspendSetting, "runtime_suspend"
		case RuntimeResume:
			resolve, reason = m.resumeSetting, "runtime_resume"
		default:
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		profile := clientRPCProfile{}
		if cfg.RPCState != nil && cfg.RPCState.ActiveProfileID != "" {
			var exists bool
			profile, exists = cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
			if !exists || profile.ID != cfg.RPCState.ActiveProfileID {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
		}
		setting, err := resolve(*cfg, profile)
		if err != nil {
			return err
		}
		if setting.Effective == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED {
			return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		switch setting.Effective {
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT:
			if cfg.ConnectionIntent == nil {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: reason + "_no_saved_intent", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
				changed = true
			} else if cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected && cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT:
			disconnect = true
			if cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.ConnectionIntent.StartupRecovery != nil {
				cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: reason, UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
				changed = true
			}
			if cfg.RPCState != nil && cfg.RPCState.ProfileSwitch != nil {
				target, exists := cfg.RPCState.Profiles[cfg.RPCState.ProfileSwitch.To]
				if !exists || target.ID != cfg.RPCState.ProfileSwitch.To {
					return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
				}
				if !reflect.DeepEqual(target.Configuration.ConnectionIntent, cfg.ConnectionIntent) {
					intent := *cfg.ConnectionIntent
					target.Configuration.ConnectionIntent = &intent
					cfg.RPCState.Profiles[target.ID] = target
					changed = true
				}
			}
		default:
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		if changed && cfg.RPCState != nil {
			cfg.RPCState.Revision++
		}
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if disconnect && m.cancelApply != nil {
		m.cancelApply()
	}
	if disconnect && m.cancelLogout != nil {
		m.cancelLogout()
	}
	if changed {
		m.publishMutationLocked(nil)
	}
	return nil
}
