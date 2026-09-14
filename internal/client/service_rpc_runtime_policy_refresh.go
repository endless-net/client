package client

import (
	"context"
	"errors"
	"reflect"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// RefreshRuntimeLifecyclePolicy adopts authority under a fresh context CAS.
// The caller holds the runtime effect gate; this method never resumes a tunnel
// or copies fetched intent/profile/credential fields into durable state.
func (m *ClientRPCMutations) RefreshRuntimeLifecyclePolicy(ctx context.Context, before, candidate Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	err := m.store.Update(func(current *Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !StartupPolicyContextEqual(before, *current) {
			return errors.New("lifecycle policy context changed")
		}
		if candidate.MapRevision < current.MapRevision || candidate.MapGlobalRevision < current.MapGlobalRevision {
			return errors.New("lifecycle policy map is stale")
		}
		next := *current
		ApplyStartupPolicyMap(&next, candidate)
		profile := clientRPCProfile{}
		if next.RPCState != nil && next.RPCState.ActiveProfileID != "" {
			var exists bool
			profile, exists = next.RPCState.Profiles[next.RPCState.ActiveProfileID]
			if !exists || profile.ID != next.RPCState.ActiveProfileID {
				return errors.New("lifecycle policy profile changed")
			}
		}
		setting, err := lifecycleSetting(next, profile, api.ClientSettingResume, profile.Resume, m.now())
		if err != nil {
			return err
		}
		if setting.Effective == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_UNSPECIFIED || next.CachedMap == nil || next.CachedMap.MapSignature == nil || next.MapHash != next.CachedMap.MapSignature.PayloadHash {
			return errors.New("lifecycle policy authority unavailable")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		changed = !reflect.DeepEqual(*current, next)
		if changed {
			ApplyStartupPolicyMap(current, next)
			if current.RPCState != nil {
				current.RPCState.Revision++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		m.publishMutationLocked(nil, ipc.Domain_DOMAIN_PREFERENCES, ipc.Domain_DOMAIN_MANAGED_SETTINGS, ipc.Domain_DOMAIN_RESOURCES, ipc.Domain_DOMAIN_PEERS, ipc.Domain_DOMAIN_NETWORKS, ipc.Domain_DOMAIN_EXIT_NODE)
	}
	return nil
}
