package client

import (
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
	if worker := m.connectionWorker; worker != nil && worker.ctx.Err() == nil {
		info.Capabilities = []*ipc.CapabilityStatus{{
			Capability: ipc.Capability_CAPABILITY_CONNECTION, Platform: build.Platform,
			Restriction: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE},
		}}
	}
	return info
}

// Capabilities are opening-stream context, not persisted intent or a release
// acceptance claim. Rebootstrap instead of sending a second snapshot or letting
// old subscribers keep the readiness of a stopped/replaced worker.
func (m *ClientRPCMutations) setConnectionWorker(worker *clientRPCProfileWorker, ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ready {
		if m.connectionWorker == worker {
			return
		}
		m.connectionWorker = worker
	} else {
		if m.connectionWorker != worker {
			return
		}
		m.connectionWorker = nil
	}
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
