package client

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (m *ClientRPCMutations) connectAs(peer local.Peer, request *ipc.ConnectRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/Connect", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.NodeID == "" || cfg.CachedMap == nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		op.ProfileId = profile.ID
		cfg.RPCState.ConnectOperationID = op.Id
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
		return nil
	})
	return op, err
}

func (m *ClientRPCMutations) ReconcileConnect(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Start == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.ConnectOperationID == "" {
		return nil
	}
	id := cfg.RPCState.ConnectOperationID
	var current *ipc.Operation
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		if op.Id == id {
			current = op
			break
		}
	}
	if current == nil || current.Kind != ipc.OperationKind_OPERATION_KIND_CONNECT || current.ProfileId != cfg.RPCState.ActiveProfileID {
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	if current.State == ipc.OperationState_OPERATION_STATE_PENDING {
		if _, err := m.ReconcileOperation(id, func(_ *Config, op *ipc.Operation) error {
			op.State = ipc.OperationState_OPERATION_STATE_RUNNING
			return nil
		}); err != nil {
			return err
		}
	} else if current.State != ipc.OperationState_OPERATION_STATE_RUNNING {
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	cfg = m.store.Read()
	var applyErr error
	if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected {
		applyErr = m.applyProfileConnection(ctx, driver, cfg)
		if err := ctx.Err(); err != nil {
			return err
		}
		if applyErr != nil {
			if _, err := driver.Stop(ctx); err != nil {
				applyErr = err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		cfg.RPCState.ConnectOperationID = ""
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: true}}
		// Recheck under the durable transaction: Disconnect can arrive during
		// Start or between its return and this commit. Never restore intent.
		if cfg.ConnectionIntent == nil || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
			op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
			op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED, ReasonKey: "connect_superseded_by_disconnect"}}
		} else if applyErr != nil {
			failure := rpc.FailureFromError(applyErr)
			if failure == nil {
				failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "connect_failed"}
			}
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "connect_failed", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
		}
		return nil
	})
	return err
}

func (s *ClientRPCService) Connect(ctx context.Context, request *connect.Request[ipc.ConnectRequest]) (*connect.Response[ipc.ConnectResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.connectAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.ConnectResponse{Operation: op}), nil
}
