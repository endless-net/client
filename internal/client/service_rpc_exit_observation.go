package client

import (
	"context"
	"reflect"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The source is immutable and lives only as long as its worker. Observations
// are never retained as evidence after a request or a worker has finished.
type clientRPCExitObservationSource struct {
	ctx     context.Context
	lock    *sync.Mutex
	observe func(context.Context, Config) (*ipc.ExitNodeStatus, error)
}

func (s *ClientRPCService) exitNodeAs(ctx context.Context, peer local.Peer, request *ipc.GetExitNodeRequest) (*ipc.ExitNodeStatus, error) {
	s.exitMu.Lock()
	source := s.exitObservation
	s.exitMu.Unlock()
	m := s.mutations
	m.mu.Lock()
	cfg := m.store.Read()
	status, err := s.exitRequestedStatusLocked(ctx, peer, request, cfg)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// Pending operations and inactive profiles describe durable intent only.
	// Their effects must not be mistaken for a settled active selection.
	if source == nil || source.ctx.Err() != nil || source.lock == nil || source.observe == nil || cfg.RPCState.ExitChange != nil || status.ProfileId != cfg.RPCState.ActiveProfileID {
		return status, ctx.Err()
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stop := context.AfterFunc(source.ctx, cancel)
	defer stop()
	locked := lockExitObservation(check, source.lock)
	if locked {
		defer source.lock.Unlock()
	}
	var observed *ipc.ExitNodeStatus
	var observationErr error
	if locked && check.Err() == nil {
		observed, observationErr = source.observe(check, clonePersistentConfig(cfg))
	}
	// Respect exitMu -> mutations.mu ordering. The effect lock remains held
	// through reauthorization and publication, never acquired under mutations.mu.
	s.exitMu.Lock()
	live := s.exitObservation == source && source.ctx.Err() == nil && check.Err() == nil
	m.mu.Lock()
	s.exitMu.Unlock()
	defer m.mu.Unlock()
	current := m.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetExitNode"), current); err != nil {
		return nil, err
	}
	if _, err := rpcFindProfile(&current, request.GetProfile()); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(cfg, current) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !live || observationErr != nil || !exitAppliedResultMatches(&clientRPCExitChange{ProfileID: status.ProfileId, Requested: cfg.ExitSelection}, observed) {
		return status, nil
	}
	// Intent, metadata and mutation restrictions belong to the durable producer.
	// Only validated actual runtime fields come from native observation.
	status.EffectiveExitNodeId = observed.EffectiveExitNodeId
	status.EffectiveLanAccess = observed.EffectiveLanAccess
	status.ApplyState, status.FailClosed, status.Failure = observed.ApplyState, observed.FailClosed, nil
	for _, pair := range [][2]*ipc.ExitFamilyStatus{{status.Ipv4, observed.Ipv4}, {status.Ipv6, observed.Ipv6}} {
		pair[0].EffectiveExitNodeId = pair[1].EffectiveExitNodeId
		pair[0].ApplyState, pair[0].FailClosed, pair[0].Failure = pair[1].ApplyState, pair[1].FailClosed, nil
	}
	status.Metadata.GeneratedAt = timestamppb.New(m.now())
	return proto.Clone(status).(*ipc.ExitNodeStatus), nil
}

func lockExitObservation(ctx context.Context, lock *sync.Mutex) bool {
	for {
		if ctx.Err() != nil {
			return false
		}
		if lock.TryLock() {
			return true
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
