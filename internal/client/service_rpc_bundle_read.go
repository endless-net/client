package client

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (s *ClientRPCService) ReadDiagnosticsBundle(ctx context.Context, request *connect.Request[ipc.ReadDiagnosticsBundleRequest]) (*connect.Response[ipc.ReadDiagnosticsBundleResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.readDiagnosticsBundleAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) readDiagnosticsBundleAs(ctx context.Context, peer local.Peer, request *ipc.ReadDiagnosticsBundleRequest) (*ipc.ReadDiagnosticsBundleResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Keep authorization, publication proof and chunk copying in one admission
	// critical section with mutations; never acquire this lock from the store.
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	cfg := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/ReadDiagnosticsBundle"), cfg); err != nil {
		return nil, err
	}
	if request == nil || !validRPCUUID(request.BundleId) || request.MaxBytes > 256<<10 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if s.bundleStore == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	profile, metadata, err := publishedRPCBundle(cfg, peer.Identity, request.BundleId)
	if err != nil {
		return nil, err
	}
	s.bundleStore.mu.Lock()
	item, exists := s.bundleStore.items[request.BundleId]
	matches := exists && strings.EqualFold(item.installationOwner, cfg.LocalOwnerID) && proto.Equal(item.metadata, metadata)
	s.bundleStore.mu.Unlock()
	if !matches {
		return nil, rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	result, err := s.bundleStore.read(peer.Identity, profile, request)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, err
}

// The journal outlives the 15-minute archive TTL. An unpublished/orphaned file
// grants no authority. Later logout/forget admission invalidates the old handle
// even if cleanup fails or the same profile is subsequently enrolled again.
func publishedRPCBundle(cfg Config, owner, id string) (string, *ipc.BundleResult, error) {
	notFound := rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	if cfg.RPCState == nil {
		return "", nil, notFound
	}
	var published *ipc.Operation
	revoked := map[string]uint64{}
	for _, record := range cfg.RPCState.Operations {
		op := &ipc.Operation{}
		if proto.Unmarshal(record.Operation, op) != nil {
			return "", nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		switch op.Kind {
		case ipc.OperationKind_OPERATION_KIND_LOGOUT, ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT, ipc.OperationKind_OPERATION_KIND_REMOVE_PROFILE:
			revoked[op.ProfileId] = max(revoked[op.ProfileId], op.GetMetadata().GetRevision())
		case ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE:
			if strings.EqualFold(record.Owner, owner) && op.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED && op.GetBundle().GetBundleId() == id {
				if published != nil {
					return "", nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
				}
				published = op
			}
		}
	}
	if published == nil || published.ProfileId == "" || published.GetMetadata().GetRevision() == 0 {
		return "", nil, notFound
	}
	if _, exists := cfg.RPCState.Profiles[published.ProfileId]; !exists || revoked[published.ProfileId] >= published.Metadata.Revision {
		return "", nil, notFound
	}
	return published.ProfileId, published.GetBundle(), nil
}
