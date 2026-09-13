package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (m *ClientRPCMutations) forgetEnrollmentAs(peer local.Peer, request *ipc.ForgetLocalEnrollmentRequest) (*ipc.Operation, error) {
	cfg := m.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/ForgetLocalEnrollment"), cfg); err != nil {
		return nil, err
	}
	if cfg.RPCState != nil && request.GetProfile().GetProfileId() != cfg.RPCState.ActiveProfileID {
		return m.forgetInactiveEnrollmentAs(peer, request)
	}
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/ForgetLocalEnrollment", request, func(cfg *Config, op *ipc.Operation) error {
		if !request.Confirmed {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		// A matching enrollment is cancelled durably and drained before cleanup.
		// Other providers still require their own explicit cancellation protocol.
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				if pending.Kind == ipc.OperationKind_OPERATION_KIND_ENROLL && pending.ProfileId == request.GetProfile().GetProfileId() && cfg.RPCState.Enrollment != nil && cfg.RPCState.Enrollment.OperationID == pending.Id {
					cfg.RPCState.Enrollment.CancelRequested = true
					continue
				}
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		return m.prepareDisconnect(cfg, op, request.Profile)
	})
	return op, err
}

func (m *ClientRPCMutations) forgetInactiveEnrollmentAs(peer local.Peer, request *ipc.ForgetLocalEnrollmentRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/ForgetLocalEnrollment", request, func(cfg *Config, op *ipc.Operation) error {
		if !request.Confirmed {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID == cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if pending := cfg.RPCState.ProfileSwitch; pending != nil && (pending.From == profile.ID || pending.To == profile.ID) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if pending.ProfileId == profile.ID && !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		requestID := ""
		if profile.Configuration.EnrollmentRecovery != nil {
			requestID = profile.Configuration.EnrollmentRecovery.RequestID
		}
		if err := ApplyLocalLogoutCleanup(&profile.Configuration, m.now()); err != nil {
			return err
		}
		cfg.RPCState.Profiles[profile.ID] = profile
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
		op.Outcome = &ipc.Operation_Cleanup{Cleanup: &ipc.CleanupResult{Outcome: ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_UNCONFIRMED, LocalRegistrationRemoved: true, ControlRequestId: requestID}}
		return nil
	}, true)
	return op, err
}

func (s *ClientRPCService) ForgetLocalEnrollment(ctx context.Context, request *connect.Request[ipc.ForgetLocalEnrollmentRequest]) (*connect.Response[ipc.ForgetLocalEnrollmentResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
		cfg := s.mutations.store.Read()
		if cfg.RPCState != nil && request.Msg.GetProfile().GetProfileId() != cfg.RPCState.ActiveProfileID {
			// This path may never accept an active cleanup without an executor.
			// The transaction rechecks inactivity even if selection raced this read.
			op, err := s.mutations.forgetInactiveEnrollmentAs(peer, request.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(&ipc.ForgetLocalEnrollmentResponse{Operation: op}), nil
		}
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.forgetEnrollmentAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.ForgetLocalEnrollmentResponse{Operation: op}), nil
}
