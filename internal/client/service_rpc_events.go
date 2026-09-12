package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const rpcEventQueueBytes = 8 << 20
const rpcEventQueueCount = 64

type rpcSubscriber struct {
	mu       sync.Mutex
	peer     local.Peer
	build    *ipc.BuildIdentity
	queue    chan *ipc.WatchEventsResponse
	done     chan struct{}
	closed   bool
	err      error
	bytes    int
	sequence uint64
	abort    func()
	sending  bool
}

func (s *rpcSubscriber) enqueue(event *ipc.WatchEventsResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	event = proto.Clone(event).(*ipc.WatchEventsResponse)
	event.Sequence = s.sequence + 1
	size := proto.Size(event)
	if len(s.queue) >= rpcEventQueueCount || s.bytes+size > rpcEventQueueBytes || size > rpc.MaxResponseBytes {
		s.closed = true
		s.err = rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		close(s.done)
		if s.sending && s.abort != nil {
			s.abort()
		}
		return
	}
	s.sequence++
	s.bytes += size
	s.queue <- event
}

func (s *rpcSubscriber) next(ctx context.Context) (*ipc.WatchEventsResponse, error) {
	select {
	case <-ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return nil, s.err
		}
		return nil, ctx.Err()
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return nil, s.err
	case event := <-s.queue:
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return nil, s.err
		}
		s.bytes -= proto.Size(event)
		return event, nil
	}
}

func (s *rpcSubscriber) send(stream *connect.ServerStream[ipc.WatchEventsResponse], event *ipc.WatchEventsResponse) error {
	s.mu.Lock()
	if s.closed {
		err := s.err
		s.mu.Unlock()
		return err
	}
	s.sending = true
	s.mu.Unlock()
	err := stream.Send(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sending = false
	if s.closed {
		return s.err
	}
	return err
}

func (m *ClientRPCMutations) unsubscribe(s *rpcSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subscribers, s)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.err = context.Canceled
		close(s.done)
	}
}

func rpcCallerAccess(peer local.Peer, cfg Config) ipc.Access {
	if peer.Administrator {
		return ipc.Access_ACCESS_ADMINISTRATOR
	}
	if cfg.LocalOwnerID != "" && strings.EqualFold(cfg.LocalOwnerID, peer.Identity) {
		return ipc.Access_ACCESS_OWNER
	}
	return ipc.Access_ACCESS_OBSERVER
}

func (m *ClientRPCMutations) snapshotLocked(peer local.Peer, build *ipc.BuildIdentity, cfg Config) (*ipc.SnapshotEvent, error) {
	access := rpcCallerAccess(peer, cfg)
	metadata := &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: 1, GeneratedAt: timestamppb.New(m.now())}
	if cfg.RPCState != nil {
		metadata.Revision = cfg.RPCState.Revision
	}
	status := &ipc.Status{} // No provider observation is never proof of connection.
	if m.observedStatus != nil {
		status = proto.Clone(m.observedStatus).(*ipc.Status)
	}
	status.Metadata = metadata
	status.CurrentOperations = nil
	if access == ipc.Access_ACCESS_OBSERVER {
		status = &ipc.Status{Metadata: metadata, ServiceState: status.ServiceState, ControlState: status.ControlState,
			ConnectionPhase: status.ConnectionPhase, UserDisconnected: status.UserDisconnected,
			Intent: &ipc.ConnectionIntent{DesiredState: status.GetIntent().GetDesiredState()}}
	} else if cfg.RPCState != nil {
		status.ActiveProfileId = cfg.RPCState.ActiveProfileID
		for _, record := range cfg.RPCState.Operations {
			if !strings.EqualFold(record.Owner, peer.Identity) {
				continue
			}
			op := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, op) != nil {
				return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(op.State) {
				status.CurrentOperations = append(status.CurrentOperations, op)
			}
		}
		sort.Slice(status.CurrentOperations, func(i, j int) bool { return status.CurrentOperations[i].Id < status.CurrentOperations[j].Id })
		if len(status.CurrentOperations) > rpcMaxNonterminalOperations {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
	}
	return &ipc.SnapshotEvent{Runtime: &ipc.RuntimeInfo{Build: proto.Clone(build).(*ipc.BuildIdentity), InstanceId: m.instanceID,
		CallerAccess: access, Protocol: rpc.Protocol, IpcVersion: rpc.Version, ContractSha256: rpc.Digest()}, Status: status}, nil
}

func (m *ClientRPCMutations) snapshotAs(peer local.Peer, build *ipc.BuildIdentity) (*ipc.SnapshotEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if peer.Identity == "" {
		return nil, rpc.Error(connect.CodeUnauthenticated, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}
	return m.snapshotLocked(peer, build, m.store.Read())
}

func (m *ClientRPCMutations) subscribe(peer local.Peer, build *ipc.BuildIdentity, abort func()) (*rpcSubscriber, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if peer.Identity == "" {
		return nil, rpc.Error(connect.CodeUnauthenticated, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}
	snapshot, err := m.snapshotLocked(peer, build, m.store.Read())
	if err != nil {
		return nil, err
	}
	s := &rpcSubscriber{peer: peer, build: proto.Clone(build).(*ipc.BuildIdentity), queue: make(chan *ipc.WatchEventsResponse, rpcEventQueueCount), done: make(chan struct{}), abort: abort}
	s.enqueue(&ipc.WatchEventsResponse{Metadata: snapshot.Status.Metadata, Event: &ipc.WatchEventsResponse_Snapshot{Snapshot: snapshot}})
	if m.subscribers == nil {
		m.subscribers = map[*rpcSubscriber]struct{}{}
	}
	m.subscribers[s] = struct{}{}
	return s, nil
}

func (m *ClientRPCMutations) publishMutationLocked(operation *ipc.Operation) {
	cfg := m.store.Read()
	for subscriber := range m.subscribers {
		snapshot, err := m.snapshotLocked(subscriber.peer, subscriber.build, cfg)
		if err != nil {
			subscriber.mu.Lock()
			if !subscriber.closed {
				subscriber.closed = true
				subscriber.err = err
				close(subscriber.done)
				if subscriber.sending && subscriber.abort != nil {
					subscriber.abort()
				}
			}
			subscriber.mu.Unlock()
			continue
		}
		metadata := snapshot.Status.Metadata
		// Snapshot also refreshes role after an initial ownership claim.
		subscriber.enqueue(&ipc.WatchEventsResponse{Metadata: metadata, Event: &ipc.WatchEventsResponse_Snapshot{Snapshot: snapshot}})
		if snapshot.Runtime.CallerAccess == ipc.Access_ACCESS_OBSERVER {
			continue
		}
		if operation != nil {
			record := cfg.RPCState.Operations[operation.RequestId]
			if strings.EqualFold(record.Owner, subscriber.peer.Identity) {
				subscriber.enqueue(&ipc.WatchEventsResponse{Metadata: metadata, Event: &ipc.WatchEventsResponse_OperationChanged{OperationChanged: operation}})
			}
			switch operation.Kind {
			case ipc.OperationKind_OPERATION_KIND_CREATE_PROFILE, ipc.OperationKind_OPERATION_KIND_RENAME_PROFILE, ipc.OperationKind_OPERATION_KIND_REMOVE_PROFILE, ipc.OperationKind_OPERATION_KIND_SELECT_PROFILE:
				subscriber.enqueue(&ipc.WatchEventsResponse{Metadata: metadata, Event: &ipc.WatchEventsResponse_Invalidated{Invalidated: &ipc.DomainInvalidated{Domain: ipc.Domain_DOMAIN_PROFILES}}})
			}
		}
	}
}

// PublishStatus accepts the runtime provider's observation, not caller input.
// Status is volatile across restart; only the revision is durable. Providers
// must verify maps/deadlines and redact failure details before publishing.
func (m *ClientRPCMutations) PublishStatus(status *ipc.Status) error {
	return m.publishStatus(status, nil)
}

func (m *ClientRPCMutations) publishStatus(status *ipc.Status, configFingerprint *[32]byte) error {
	if status == nil {
		return errors.New("runtime status observation is required")
	}
	status = proto.Clone(status).(*ipc.Status)
	expected := status.Metadata
	status.Metadata = nil
	status.CurrentOperations = nil
	if proto.Size(status) > rpc.MaxResponseBytes-4096 {
		return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	revision := uint64(1)
	if cfg.RPCState != nil {
		revision = cfg.RPCState.Revision
	}
	if expected.GetInstanceId() != m.instanceID || expected.GetRevision() != revision {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	unchanged := proto.Equal(m.observedStatus, status)
	if unchanged && configFingerprint == nil {
		return nil
	}
	if err := m.store.Update(func(cfg *Config) error {
		if configFingerprint != nil {
			raw, err := json.Marshal(cfg)
			if err != nil {
				return err
			}
			if sha256.Sum256(raw) != *configFingerprint {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
		}
		if unchanged {
			return errRPCNoChange
		}
		if cfg.RPCState == nil {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			cfg.RPCState = &ClientRPCState{Revision: 1, DigestKey: key, Operations: map[string]clientRPCOperationRecord{}}
		}
		cfg.RPCState.Revision++
		return nil
	}); err != nil {
		if errors.Is(err, errRPCNoChange) {
			return nil
		}
		return err
	}
	m.observedStatus = status
	m.publishMutationLocked(nil)
	return nil
}

// ObserveStatus builds outside the mutation lock so slow probes cannot block
// Disconnect. Publish rejects an observation if its config snapshot became stale.
func (m *ClientRPCMutations) ObserveStatus(observe func(Config) (*ipc.Status, error)) error {
	if observe == nil {
		return errors.New("runtime observation provider is required")
	}
	m.mu.Lock()
	cfg := m.store.Read()
	revision := uint64(1)
	if cfg.RPCState != nil {
		revision = cfg.RPCState.Revision
	}
	expected := &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: revision}
	m.mu.Unlock()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(raw)
	status, err := observe(cfg)
	if err != nil {
		return err
	}
	if status == nil {
		return errors.New("runtime observation is missing")
	}
	status = proto.Clone(status).(*ipc.Status)
	status.Metadata = expected
	return m.publishStatus(status, &fingerprint)
}
