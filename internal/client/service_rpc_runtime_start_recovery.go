package client

import (
	"crypto/sha256"
	"encoding/json"
	"time"
)

// Private state carried by the blocked intent. An explicit Connect/Disconnect
// writes a new intent and removes this record; it is never projected over IPC.
type clientRuntimeStartRecovery struct {
	Context           [32]byte `json:"context"`
	PreviousState     string   `json:"previous_state"`
	PreviousReason    string   `json:"previous_reason"`
	PreviousUpdatedAt string   `json:"previous_updated_at"`
}

func runtimeStartRecoveryContext(cfg Config) [32]byte {
	profile := clientRPCProfile{}
	active := ""
	if cfg.RPCState != nil {
		active = cfg.RPCState.ActiveProfileID
		profile = cfg.RPCState.Profiles[active]
	}
	// Length-delimited JSON avoids ambiguous joins. Credential/key material
	// remains in the existing protected config; the checkpoint stores only a hash.
	value, _ := json.Marshal([]any{cfg.LocalOwnerID, cfg.NodeID, cfg.NetworkID, cfg.ControlURLs(), cfg.NodeCredential, cfg.Token,
		cfg.PrivateKey, cfg.IdentityPrivateKey, cfg.DeviceFingerprint, active, profile.ID, profile.ControlOrigin, profile.RuntimeStart})
	return sha256.Sum256(value)
}

func runtimeStartWithRecovery(cfg Config, now time.Time) *ConnectionIntent {
	original := cfg.ConnectionIntent
	if original != nil && original.StartupRecovery != nil {
		saved := original.StartupRecovery
		if original.DesiredState == ConnectionIntentDesiredDisconnected && original.Reason == "runtime_start_policy_unavailable" && saved.PreviousState == ConnectionIntentDesiredConnected && saved.Context == runtimeStartRecoveryContext(cfg) {
			if runtimeStartRecoveryPending(cfg) {
				return original
			}
			probe := cfg
			probe.ConnectionIntent = &ConnectionIntent{DesiredState: saved.PreviousState, Reason: saved.PreviousReason, UpdatedAt: saved.PreviousUpdatedAt}
			next := runtimeStartIntent(probe, now)
			if next.Reason == "runtime_start_policy_unavailable" {
				return original
			}
			return next
		}
		// Identity or a future-start preference changed. An old checkpoint must
		// not provide the new context's KEEP_INTENT value.
		copy := *original
		copy.StartupRecovery = nil
		cfg.ConnectionIntent = &copy
	}
	next := runtimeStartIntent(cfg, now)
	if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected && next.DesiredState == ConnectionIntentDesiredDisconnected && next.Reason == "runtime_start_policy_unavailable" && !runtimeStartRecoveryPending(cfg) {
		next.StartupRecovery = &clientRuntimeStartRecovery{Context: runtimeStartRecoveryContext(cfg), PreviousState: cfg.ConnectionIntent.DesiredState, PreviousReason: cfg.ConnectionIntent.Reason, PreviousUpdatedAt: cfg.ConnectionIntent.UpdatedAt}
	}
	return next
}
