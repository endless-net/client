package client

import (
	"context"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// This transaction handles only the exact current ID. Different-network
// requests use beginNetworkSelectionAs and the durable native worker.
func (m *ClientRPCMutations) selectNetworkAs(peer local.Peer, request *ipc.SelectNetworkRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/SelectNetwork", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if request.NetworkId == "" || len(request.NetworkId) > 256 || !utf8.ValidString(request.NetworkId) {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
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
		if cfg.NodeID == "" || cfg.NetworkID == "" || cfg.NodeCredential == "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
		}
		if request.NetworkId != cfg.NetworkID {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
		op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: cfg.NetworkID}}
		return nil
	}, true)
	return op, err
}

func (s *ClientRPCService) SelectNetwork(ctx context.Context, request *connect.Request[ipc.SelectNetworkRequest]) (*connect.Response[ipc.SelectNetworkResponse], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	peer, _ := local.PeerFromContext(ctx)
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	var op *ipc.Operation
	var err error
	if request.Msg.GetNetworkId() == s.mutations.store.Read().NetworkID {
		op, err = s.mutations.selectNetworkAs(peer, request.Msg)
	} else {
		w := s.networkWorker
		if w == nil || w.ctx.Err() != nil {
			// Authorization and durable replay precede readiness. A stopped
			// worker prevents new admission, not retrieval of accepted work.
			op, _, err = s.mutations.acceptInternal(peer, "/client.v0.ClientService/SelectNetwork", request.Msg, func(*Config, *ipc.Operation) error {
				return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}, false)
		} else {
			op, err = s.mutations.beginNetworkSelectionAs(peer, request.Msg)
			if err == nil {
				select {
				case w.wake <- struct{}{}:
				default:
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.SelectNetworkResponse{Operation: op}), nil
}
