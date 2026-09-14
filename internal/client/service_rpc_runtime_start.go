package client

import (
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func runtimeStartIntent(cfg Config, now time.Time) *ConnectionIntent {
	profile := clientRPCProfile{}
	if cfg.RPCState != nil {
		profile = cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
	}
	setting, err := lifecycleSetting(cfg, profile, api.ClientSettingRuntimeStart, profile.RuntimeStart, now)
	desired, reason := ConnectionIntentDesiredDisconnected, "runtime_start_no_saved_intent"
	if err != nil {
		// Never substitute a local CONNECT/KEEP_INTENT for unverifiable policy.
		// GetPreferences reports the source error; the reason explains why the
		// agent stays down without preventing local trust/enrollment recovery.
		reason = "runtime_start_policy_unavailable"
	} else {
		switch setting.Effective {
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_KEEP_INTENT:
			if cfg.ConnectionIntent != nil {
				return cfg.ConnectionIntent
			}
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT:
			if runtimeStartRecoveryPending(cfg) {
				// Startup is not authority to discard an accepted operation's
				// recovery checkpoint, particularly Disconnect or containment.
				if cfg.ConnectionIntent != nil {
					return cfg.ConnectionIntent
				}
				reason = "runtime_start_recovery_pending"
			} else {
				desired, reason = ConnectionIntentDesiredConnected, "runtime_start"
			}
		case ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT:
			reason = "runtime_start"
		}
	}
	if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == desired {
		return cfg.ConnectionIntent
	}
	return &ConnectionIntent{DesiredState: desired, Reason: reason, UpdatedAt: now.UTC().Format(time.RFC3339)}
}

func runtimeStartRecoveryPending(cfg Config) bool {
	if cfg.RPCState == nil {
		return false
	}
	if cfg.RPCState.ExitChange != nil || cfg.RPCState.NetworkPreferenceChange != nil {
		return true
	}
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil || !rpcOperationTerminal(op.State) {
			return true
		}
	}
	return false
}
