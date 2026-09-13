package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ClientRPCPeerObservation struct {
	ProfileID   string
	MapRevision uint64
	// The global axis can change authorization without changing Network.Revision.
	MapGlobalRevision uint64
	Peers             []*ipc.Peer
}
type ClientRPCPeersProvider func(context.Context) (ClientRPCPeerObservation, error)

func (s *ClientRPCService) ListPeers(ctx context.Context, request *connect.Request[ipc.ListPeersRequest]) (*connect.Response[ipc.ListPeersResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.peersAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) peersAs(ctx context.Context, peer local.Peer, request *ipc.ListPeersRequest) (*ipc.ListPeersResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/ListPeers")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	search := strings.ToLower(strings.TrimSpace(request.GetSearch()))
	if !utf8.ValidString(request.GetSearch()) || len(request.GetSearch()) > 256 || request.GetPage().GetPageSize() > 500 || len(request.GetPage().GetPageToken()) > 2048 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if s.PeersProvider == nil || profile.ID != cfg.RPCState.ActiveProfileID {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	observation, err := s.PeersProvider(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	if observation.ProfileID != profile.ID || observation.MapRevision == 0 || cfg.CachedMap == nil || cfg.CachedMap.Network.Revision != observation.MapRevision || cfg.CachedMap.Revision.Global != observation.MapGlobalRevision {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if len(observation.Peers) > 4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	items := make([]*ipc.Peer, 0, len(observation.Peers))
	ids := map[string]bool{}
	for _, item := range observation.Peers {
		if item == nil || item.Id == "" || ids[item.Id] || len(item.Id) > 256 || !utf8.ValidString(item.Id) || !utf8.ValidString(item.Hostname) {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		ids[item.Id] = true
		if search == "" || strings.Contains(strings.ToLower(item.Id+"\x00"+item.Hostname+"\x00"+strings.Join(item.OverlayAddresses, "\x00")), search) {
			items = append(items, proto.Clone(item).(*ipc.Peer))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Id < items[j].Id })
	whole := &ipc.ListPeersResponse{Peers: items, SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, MapRevision: observation.MapRevision, TargetMapRevision: observation.MapRevision}
	if proto.Size(whole) > rpc.MaxResponseBytes-4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(whole)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	current := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, method, current); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(current)) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start, end, next, err := s.mutations.pageRange(peer, "ListPeers\x00"+profile.ID+"\x00"+search+"\x00"+strconv.FormatUint(observation.MapGlobalRevision, 10)+"\x00"+hex.EncodeToString(digest[:]), request.GetPage(), cfg, len(items))
	if err != nil {
		return nil, err
	}
	whole.Peers = items[start:end]
	whole.Page = &ipc.PageResponse{NextPageToken: next, Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}}
	return whole, nil
}
