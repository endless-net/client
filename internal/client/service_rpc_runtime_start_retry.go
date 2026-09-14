package client

import (
	"context"
	"errors"
	"reflect"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func StartupPolicyContextEqual(a, b Config) bool {
	return a.NodeID == b.NodeID && a.NetworkID == b.NetworkID && a.NodeCredential == b.NodeCredential && a.LocalOwnerID == b.LocalOwnerID &&
		a.NodeApprovalState == b.NodeApprovalState && a.ActiveAccountID == b.ActiveAccountID &&
		a.PrivateKey == b.PrivateKey && a.IdentityPrivateKey == b.IdentityPrivateKey && a.DeviceFingerprint == b.DeviceFingerprint && a.Token == b.Token &&
		a.MapRevision == b.MapRevision && a.MapGlobalRevision == b.MapGlobalRevision && a.MapHash == b.MapHash && reflect.DeepEqual(a.ControlURLs(), b.ControlURLs()) &&
		reflect.DeepEqual(a.ConnectionIntent, b.ConnectionIntent) && reflect.DeepEqual(a.RPCState, b.RPCState) &&
		reflect.DeepEqual(a.EnrollmentRecovery, b.EnrollmentRecovery) &&
		reflect.DeepEqual(a.MapSigningTrust, b.MapSigningTrust) && reflect.DeepEqual(a.CachedMap, b.CachedMap)
}

// ApplyStartupPolicyMap copies verified authority only, never credentials,
// profile/recovery records or a fetched connection intent.
func ApplyStartupPolicyMap(current *Config, candidate Config) {
	current.CachedMap, current.CachedMapSavedAt = candidate.CachedMap, candidate.CachedMapSavedAt
	current.MapRevision, current.MapGlobalRevision = candidate.MapRevision, candidate.MapGlobalRevision
	current.MapHash, current.MapSigningTrust = candidate.MapHash, candidate.MapSigningTrust
	current.NodeApprovalState = candidate.NodeApprovalState
}

func RuntimeStartRecoveryReady(cfg Config) bool {
	if cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected || cfg.ConnectionIntent.Reason != "runtime_start_policy_unavailable" || cfg.NodeID == "" || cfg.NodeCredential == "" || cfg.EnrollmentRecovery != nil || runtimeStartRecoveryPending(cfg) {
		return false
	}
	if cfg.RPCState != nil {
		profile, exists := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
		if !exists || profile.ID != cfg.RPCState.ActiveProfileID {
			return false
		}
	}
	return true
}

// The agent holds its effect lock and confirms Down before calling this method.
// The map and resumed intent become visible together, after a fresh CAS check.
func (s ConnectionIntentStore) RecoverStartupPolicy(ctx context.Context, before, candidate Config) error {
	return s.config.Update(func(current *Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !StartupPolicyContextEqual(before, *current) || !RuntimeStartRecoveryReady(*current) {
			return errors.New("startup recovery context changed")
		}
		if candidate.MapRevision < current.MapRevision || candidate.MapGlobalRevision < current.MapGlobalRevision {
			return errors.New("startup recovery map is stale")
		}
		next := *current
		ApplyStartupPolicyMap(&next, candidate)
		profile := clientRPCProfile{}
		if next.RPCState != nil {
			profile = next.RPCState.Profiles[next.RPCState.ActiveProfileID]
		}
		setting, err := lifecycleSetting(next, profile, api.ClientSettingRuntimeStart, profile.RuntimeStart, s.now())
		if err != nil {
			return err
		}
		if setting.Effective == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED {
			return errors.New("startup policy unavailable")
		}
		if next.CachedMap == nil || next.CachedMap.MapSignature == nil || next.MapHash != next.CachedMap.MapSignature.PayloadHash {
			return errors.New("startup policy map hash mismatch")
		}
		next.ConnectionIntent = runtimeStartWithRecovery(next, s.now())
		if next.ConnectionIntent.DesiredState == ConnectionIntentDesiredDisconnected && next.ConnectionIntent.Reason == "runtime_start_policy_unavailable" && next.ConnectionIntent.StartupRecovery == nil {
			// A valid KEEP_INTENT may intentionally stay down. Mark source
			// recovery complete so that this result does not retrigger forever.
			resolved := *next.ConnectionIntent
			resolved.Reason, resolved.UpdatedAt = "runtime_start", s.now().UTC().Format(time.RFC3339)
			next.ConnectionIntent = &resolved
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		ApplyStartupPolicyMap(current, next)
		current.ConnectionIntent = next.ConnectionIntent
		if current.RPCState != nil {
			current.RPCState.Revision++
		}
		return nil
	})
}

func (m *ClientRPCMutations) RecoverStartupPolicy(ctx context.Context, before, candidate Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store := NewConnectionIntentStore(m.store)
	store.now = m.now
	if err := store.RecoverStartupPolicy(ctx, before, candidate); err != nil {
		return err
	}
	m.publishMutationLocked(nil, ipc.Domain_DOMAIN_PREFERENCES, ipc.Domain_DOMAIN_MANAGED_SETTINGS, ipc.Domain_DOMAIN_RESOURCES, ipc.Domain_DOMAIN_PEERS, ipc.Domain_DOMAIN_NETWORKS, ipc.Domain_DOMAIN_EXIT_NODE)
	return nil
}
