package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (s *ClientRPCService) SelectExitNode(ctx context.Context, request *connect.Request[ipc.SelectExitNodeRequest]) (*connect.Response[ipc.SelectExitNodeResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.acceptExitOperation(peer, "/client.v0.ClientService/SelectExitNode", request.Msg, func(modes []clientRPCExitMode) (*ipc.Operation, error) {
		return s.mutations.selectExitNodeAs(peer, request.Msg, modes)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.SelectExitNodeResponse{Operation: op}), nil
}

func (s *ClientRPCService) ClearExitNode(ctx context.Context, request *connect.Request[ipc.ClearExitNodeRequest]) (*connect.Response[ipc.ClearExitNodeResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	peer, _ := local.PeerFromContext(ctx)
	op, err := s.acceptExitOperation(peer, "/client.v0.ClientService/ClearExitNode", request.Msg, func([]clientRPCExitMode) (*ipc.Operation, error) {
		return s.mutations.clearExitNodeAs(peer, request.Msg)
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.ClearExitNodeResponse{Operation: op}), nil
}

func (s *ClientRPCService) acceptExitOperation(peer local.Peer, method string, request proto.Message, accept func([]clientRPCExitMode) (*ipc.Operation, error)) (*ipc.Operation, error) {
	// Acceptance plus wake is serialized with worker shutdown. After a crash,
	// startup scanning resumes the journal even if the wake was never delivered.
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
	w := s.exitWorker
	if w == nil || w.ctx.Err() != nil {
		// Authenticate and resolve durable replay before checking readiness. This
		// rejects fresh work but preserves the result of an already accepted UUID,
		// including after restart with a different instance/revision.
		op, _, err := s.mutations.acceptAs(peer, method, request, func(*Config, *ipc.Operation) error {
			return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		})
		return op, err
	}
	op, err := accept(s.exitModes)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return op, nil
}
