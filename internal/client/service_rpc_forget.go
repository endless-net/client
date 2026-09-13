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
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/ForgetLocalEnrollment", request, func(cfg *Config, op *ipc.Operation) error {
		if !request.Confirmed {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		// Until cancellation is coordinated across providers, never let a late
		// response reintroduce registration after local cleanup.
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		return m.prepareDisconnect(cfg, op, request.Profile)
	})
	return op, err
}

func (s *ClientRPCService) ForgetLocalEnrollment(ctx context.Context, request *connect.Request[ipc.ForgetLocalEnrollmentRequest]) (*connect.Response[ipc.ForgetLocalEnrollmentResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil {
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
