package client

import (
	"context"
	"sort"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (m *ClientRPCMutations) runtimeInfoLocked(peer local.Peer, build *ipc.BuildIdentity, cfg Config) *ipc.RuntimeInfo {
	info := &ipc.RuntimeInfo{
		Build: proto.Clone(build).(*ipc.BuildIdentity), InstanceId: m.instanceID,
		CallerAccess: rpcCallerAccess(peer, cfg), Protocol: rpc.Protocol,
		IpcVersion: rpc.Version, ContractSha256: rpc.Digest(),
	}
	keys := make([]int, 0, len(m.capabilityWorkers))
	for capability, worker := range m.capabilityWorkers {
		if worker.ctx.Err() == nil {
			keys = append(keys, int(capability))
		}
	}
	sort.Ints(keys)
	for _, key := range keys {
		info.Capabilities = append(info.Capabilities, &ipc.CapabilityStatus{
			Capability: ipc.Capability(key), Platform: build.Platform,
			Restriction: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE},
		})
	}
	return info
}

// Capabilities are opening-stream context, not persisted intent or a release
// acceptance claim. Rebootstrap instead of sending a second snapshot or letting
// old subscribers keep the readiness of a stopped/replaced worker.
func (m *ClientRPCMutations) setProfileWorkerReadiness(worker *clientRPCProfileWorker, ready bool) {
	capabilities := []ipc.Capability{ipc.Capability_CAPABILITY_CONNECTION, ipc.Capability_CAPABILITY_PROFILES, ipc.Capability_CAPABILITY_LOCAL_FORGET, ipc.Capability_CAPABILITY_PREFERENCES, ipc.Capability_CAPABILITY_RESOURCES}
	if worker.logout {
		capabilities = append(capabilities, ipc.Capability_CAPABILITY_LOGOUT)
	}
	m.setWorkerCapabilities(worker, ready, capabilities...)
}

// Read-only providers share the serving context rather than an executor. The
// token participates only in readiness bookkeeping; it starts no worker.
func (s *ClientRPCService) startReadCapabilities(ctx context.Context) func() {
	capabilities := []ipc.Capability{ipc.Capability_CAPABILITY_SUPPORT_INFO}
	if s.PeersProvider != nil {
		capabilities = append(capabilities, ipc.Capability_CAPABILITY_PEERS)
	}
	token := &clientRPCProfileWorker{ctx: ctx}
	s.mutations.setWorkerCapabilities(token, true, capabilities...)
	clear := func() { s.mutations.setWorkerCapabilities(token, false, capabilities...) }
	stop := context.AfterFunc(ctx, clear)
	return func() { stop(); clear() }
}

func (m *ClientRPCMutations) setWorkerCapabilities(worker *clientRPCProfileWorker, ready bool, capabilities ...ipc.Capability) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ready && m.capabilityWorkers == nil {
		m.capabilityWorkers = make(map[ipc.Capability]*clientRPCProfileWorker)
	}
	changed := false
	for _, capability := range capabilities {
		if ready && m.capabilityWorkers[capability] != worker {
			m.capabilityWorkers[capability] = worker
			changed = true
		} else if !ready && m.capabilityWorkers[capability] == worker {
			delete(m.capabilityWorkers, capability)
			changed = true
		}
	}
	if !changed {
		return
	}
	m.invalidateStreamsLocked()
}

// Caller holds m.mu. Read-model provider lifetime changes require a fresh
// opening snapshot even when they do not advertise a platform capability.
func (m *ClientRPCMutations) invalidateStreamsLocked() {
	for subscriber := range m.subscribers {
		subscriber.mu.Lock()
		if !subscriber.closed {
			subscriber.closed = true
			subscriber.err = rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			close(subscriber.done)
			if subscriber.sending && subscriber.abort != nil {
				subscriber.abort()
			}
		}
		subscriber.mu.Unlock()
	}
}
