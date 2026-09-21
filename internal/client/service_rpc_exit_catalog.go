package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
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
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
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
	ready := s.exitWorker != nil && s.exitWorker.ctx.Err() == nil
	restriction := m.exitMutationRestriction(cfg, request.GetProfile(), ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE, ready)
	if policy := mapState.Network.ClientPolicy; policy != nil {
		if len(policy.ExitNodes) > 4096 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		for _, grant := range policy.ExitNodes {
			if !m.now().Before(grant.ExpiresAt) {
				continue
			}
			item := &ipc.ExitNode{Id: grant.ID, DisplayName: grant.Name, PeerId: grant.Host.NodeID, Selection: proto.Clone(restriction).(*ipc.Restriction)}
			if restriction.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
				exitCatalogModes(item, s.exitModes, func(mode clientRPCExitMode) bool {
					selection := &ClientExitSelection{ID: grant.ID, NodeID: cfg.NodeID, NetworkID: cfg.NetworkID, RouteTable: cfg.WireGuardRouteTable, Host: grant.Host, Family: mode.Family, LAN: mode.LAN}
					_, err := exitRoutePeers(cfg, *mapState, selection, m.now())
					return err == nil
				})
				if len(item.AllowedFamilyModes) == 0 {
					item.Selection = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "exit_mode_unsupported"}
				}
			}
			items = append(items, item)
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

// The wire catalog has two independent lists, not pairs. Expose a safe rectangle:
// prefer all BLOCK-capable families, and include ALLOW only when every exposed
// family supports it. Never advertise an unsupported Cartesian combination.
func exitCatalogModes(item *ipc.ExitNode, supported []clientRPCExitMode, authorized func(clientRPCExitMode) bool) {
	families := []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack}
	allowed := func(family api.ExitFamilyMode, lan api.ExitLANAccess) bool {
		mode := clientRPCExitMode{Family: family, LAN: lan}
		return slices.Contains(supported, mode) && authorized(mode)
	}
	for _, lan := range []api.ExitLANAccess{api.ExitLANBlock, api.ExitLANAllow} {
		allAllow := true
		for i, family := range families {
			if allowed(family, lan) {
				item.AllowedFamilyModes = append(item.AllowedFamilyModes, []ipc.ExitFamilyMode{ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[i])
				allAllow = allAllow && allowed(family, api.ExitLANAllow)
			}
		}
		if len(item.AllowedFamilyModes) != 0 {
			if lan == api.ExitLANBlock {
				item.AllowedLanAccess = append(item.AllowedLanAccess, ipc.LanAccess_LAN_ACCESS_BLOCK)
			}
			if allAllow {
				item.AllowedLanAccess = append(item.AllowedLanAccess, ipc.LanAccess_LAN_ACCESS_ALLOW)
			}
			return
		}
	}
}
