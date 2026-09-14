package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sort"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Input stays within the trusted producer. Only session authorization for the
// profile's account is supplied, never node credentials or private key material.
type ClientRPCNetworksInput struct{ ControlOrigin, AccountID, SessionToken string }
type ClientRPCNetworksProvider func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error)

func (s *ClientRPCService) ListNetworks(ctx context.Context, request *connect.Request[ipc.ListNetworksRequest]) (*connect.Response[ipc.ListNetworksResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.networksAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) networksAs(ctx context.Context, peer local.Peer, request *ipc.ListNetworksRequest) (*ipc.ListNetworksResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/ListNetworks")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	if request.GetPage().GetPageSize() > 500 || len(request.GetPage().GetPageToken()) > 2048 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	selected := profile.Configuration
	if profile.ID == cfg.RPCState.ActiveProfileID {
		selected = cfg
	}
	if selected.ActiveAccountID == "" {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
	}
	if selected.Token == "" {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
	}
	if s.NetworksProvider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	networks, err := s.NetworksProvider(ctx, ClientRPCNetworksInput{ControlOrigin: profile.ControlOrigin, AccountID: selected.ActiveAccountID, SessionToken: selected.Token})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, rpcNetworkCatalogFailure(err)
	}
	if len(networks) > 1000 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	items := make([]*ipc.Network, 0, len(networks))
	ids := map[string]bool{}
	for _, network := range networks {
		if network == nil || network.Id == "" || len(network.Id) > 256 || len(network.Name) > 1024 || !utf8.ValidString(network.Id) || !utf8.ValidString(network.Name) || network.AccountId != selected.ActiveAccountID || ids[network.Id] {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		ids[network.Id] = true
		items = append(items, &ipc.Network{Id: network.Id, Name: network.Name, AccountId: network.AccountId})
	}
	if selected.NetworkID != "" && !ids[selected.NetworkID] {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Id < items[j].Id })
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
	worker := s.mutations.capabilityWorkers[ipc.Capability_CAPABILITY_NETWORK_SELECTION]
	ready := worker != nil && worker.ctx.Err() == nil
	for _, item := range items {
		item.Selection = networkSelectionRestriction(current, profile.ID, item.Id, ready)
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&ipc.ListNetworksResponse{Networks: items, SelectedNetworkId: selected.NetworkID})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	start, end, next, err := s.mutations.pageRange(peer, "ListNetworks\x00"+profile.ID+"\x00"+hex.EncodeToString(digest[:]), request.GetPage(), cfg, len(items))
	if err != nil {
		return nil, err
	}
	return &ipc.ListNetworksResponse{Networks: items[start:end], SelectedNetworkId: selected.NetworkID, Page: &ipc.PageResponse{NextPageToken: next,
		Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}}}, nil
}

func networkSelectionRestriction(cfg Config, profileID, networkID string, ready bool) *ipc.Restriction {
	if !ready {
		return &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "network_selection_provider_not_running"}
	}
	reason := ""
	switch {
	case cfg.RPCState == nil || cfg.RPCState.ActiveProfileID != profileID:
		reason = "network_selection_profile_inactive"
	case cfg.NodeID == "" || cfg.NetworkID == "" || cfg.NodeCredential == "" || (networkID != cfg.NetworkID && !networkSelectionInstallationReady(cfg)):
		reason = "network_selection_needs_enrollment"
	case cfg.RPCState.NetworkSelection != nil || cfg.RPCState.ProfileSwitch != nil:
		reason = "network_selection_busy"
	default:
		for _, record := range cfg.RPCState.Operations {
			op := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, op) != nil || !rpcOperationTerminal(op.State) {
				reason = "network_selection_busy"
				break
			}
		}
	}
	if reason != "" {
		return &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: reason}
	}
	return &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}
}

// Backend user authorization is not local IPC ownership. Preserve the recovery
// category, but rebuild details so upstream diagnostics never reach the UI.
func rpcNetworkCatalogFailure(err error) error {
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
	case connect.CodePermissionDenied:
		return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
	case connect.CodeNotFound:
		return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	case connect.CodeResourceExhausted:
		return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	case connect.CodeDeadlineExceeded:
		return rpc.Error(connect.CodeDeadlineExceeded, ipc.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED)
	case connect.CodeCanceled:
		return rpc.Error(connect.CodeCanceled, ipc.ErrorCode_ERROR_CODE_CANCELLED)
	default:
		return rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
}
