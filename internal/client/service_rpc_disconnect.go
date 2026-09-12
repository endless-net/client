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

func (m *ClientRPCMutations) disconnectAs(peer local.Peer, request *ipc.DisconnectRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/Disconnect", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		state := cfg.RPCState
		if state.DisconnectOperationID != "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		if profile.ID != state.ActiveProfileID && (state.ProfileSwitch == nil || profile.ID != state.ProfileSwitch.To) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.ProfileId = profile.ID
		state.DisconnectOperationID = op.Id
		intent := &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect", UpdatedAt: m.now().UTC().Format(time.RFC3339Nano)}
		cfg.ConnectionIntent = intent
		// A pending switch must not restore the target's saved connected intent.
		if state.ProfileSwitch != nil {
			target := state.Profiles[state.ProfileSwitch.To]
			target.Configuration.ConnectionIntent = &ConnectionIntent{DesiredState: intent.DesiredState, Reason: intent.Reason, UpdatedAt: intent.UpdatedAt}
			state.Profiles[target.ID] = target
		}
		return nil
	})
	return op, err
}

// ReconcileDisconnect stops the actual current tunnel, retaining registration.
// Lifecycle cancellation leaves the plan resumable. Down must finish before a
// successful terminal outcome can become visible.
func (m *ClientRPCMutations) ReconcileDisconnect(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.disconnectWorker.Lock()
	defer m.disconnectWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.DisconnectOperationID == "" {
		return nil
	}
	id := cfg.RPCState.DisconnectOperationID
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
	if current == nil || current.Kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT {
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	resuming := current.State == ipc.OperationState_OPERATION_STATE_RUNNING
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
	continuity, stopErr := driver.Stop(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE {
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	if resuming && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
		// Down may have succeeded before the previous process could commit its
		// outcome. An absent tunnel now cannot prove no earlier interruption.
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		cfg.RPCState.DisconnectOperationID = ""
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Continuity = continuity
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE}}
		if stopErr != nil {
			failure := rpc.FailureFromError(stopErr)
			if failure == nil {
				failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "disconnect_failed"}
			}
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
		}
		return nil
	})
	return err
}

func (s *ClientRPCService) Disconnect(ctx context.Context, request *connect.Request[ipc.DisconnectRequest]) (*connect.Response[ipc.DisconnectResponse], error) {
	// Serialize distinct requests until their outcome is durable, keeping the
	// single reserved active slot available without aliasing request identities.
	select {
	case s.disconnectGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.disconnectGate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		s.profileMu.Unlock()
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.disconnectAs(peer, request.Msg)
	if err == nil {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
	s.profileMu.Unlock()
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if rpcOperationTerminal(op.State) {
			return connect.NewResponse(&ipc.DisconnectResponse{Operation: op}), nil
		}
		// After acceptance only service lifetime controls execution. A lost
		// response is recovered with GetOperation(request_id).
		select {
		case <-w.done:
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		case <-tick.C:
			op, err = s.mutations.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				return nil, err
			}
		}
	}
}
