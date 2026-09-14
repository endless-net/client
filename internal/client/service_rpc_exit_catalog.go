package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ClientRPCService) ListExitNodes(ctx context.Context, request *connect.Request[ipc.ListExitNodesRequest]) (*connect.Response[ipc.ListExitNodesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	response, err := s.exitNodesAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (s *ClientRPCService) exitNodesAs(ctx context.Context, peer local.Peer, request *ipc.ListExitNodesRequest) (*ipc.ListExitNodesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := s.mutations
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/ListExitNodes"), cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	if request.GetPage().GetPageSize() > 500 || len(request.GetPage().GetPageToken()) > 2048 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if profile.ID != cfg.RPCState.ActiveProfileID {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	mapState := cfg.CachedMap
	if mapState == nil || cfg.MapSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" ||
		mapState.Node.ID != cfg.NodeID || mapState.Network.ID != cfg.NetworkID || mapState.Network.Revision != cfg.MapRevision || mapState.Revision.Global != cfg.MapGlobalRevision ||
		api.ValidateNetworkMap(*mapState) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*mapState, *cfg.MapSigningTrust) != nil ||
		mapState.MapSignature == nil || !m.now().Before(mapState.MapSignature.ExpiresAt) {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	items := []*ipc.ExitNode{}
	if policy := mapState.Network.ClientPolicy; policy != nil {
		if len(policy.ExitNodes) > 4096 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		for _, grant := range policy.ExitNodes {
			if !m.now().Before(grant.ExpiresAt) {
				continue
			}
			// Policy grants alone do not prove platform apply support. Selectable
			// modes remain empty until the exit executor supplies that evidence.
			items = append(items, &ipc.ExitNode{Id: grant.ID, DisplayName: grant.Name, PeerId: grant.Host.NodeID, Selection: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "exit_executor_unavailable"}})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Id < items[j].Id })
	response := &ipc.ListExitNodesResponse{ExitNodes: items}
	if proto.Size(response) > rpc.MaxResponseBytes-4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(response)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	start, end, next, err := m.pageRange(peer, "ListExitNodes\x00"+profile.ID+"\x00"+mapState.MapSignature.PayloadHash+"\x00"+hex.EncodeToString(digest[:]), request.GetPage(), cfg, len(items))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response.ExitNodes = items[start:end]
	response.Page = &ipc.PageResponse{NextPageToken: next, Metadata: &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(m.now())}}
	return response, nil
}
