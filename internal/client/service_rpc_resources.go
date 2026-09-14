package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ClientRPCService) ListResources(ctx context.Context, request *connect.Request[ipc.ListResourcesRequest]) (*connect.Response[ipc.ListResourcesResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	response, err := s.resourcesAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (s *ClientRPCService) resourcesAs(ctx context.Context, peer local.Peer, request *ipc.ListResourcesRequest) (*ipc.ListResourcesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := s.mutations
	s.profileMu.Lock()
	ready := s.profileWorker != nil && s.profileWorker.ctx.Err() == nil
	s.profileMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/ListResources"), cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	invalid := func() (*ipc.ListResourcesResponse, error) {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if len(request.GetSearch()) > 256 || !utf8.ValidString(request.GetSearch()) || strings.ContainsAny(request.GetSearch(), "\x00\r\n") || request.GetPage().GetPageSize() > 500 || len(request.GetPage().GetPageToken()) > 2048 || len(request.GetKinds()) > 4 {
		return invalid()
	}
	kinds := append([]ipc.ResourceKind(nil), request.GetKinds()...)
	slices.Sort(kinds)
	for i, kind := range kinds {
		if kind < ipc.ResourceKind_RESOURCE_KIND_HOST || kind > ipc.ResourceKind_RESOURCE_KIND_APPLICATION || (i > 0 && kinds[i-1] == kind) {
			return invalid()
		}
	}
	state := cfg.CachedMap
	if profile.ID != cfg.RPCState.ActiveProfileID || state == nil || cfg.MapSigningTrust == nil || cfg.NodeID == "" || cfg.NetworkID == "" || state.Node.ID != cfg.NodeID || state.Network.ID != cfg.NetworkID || state.Network.Revision != cfg.MapRevision || state.Revision.Global != cfg.MapGlobalRevision || api.ValidateNetworkMap(*state) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*state, *cfg.MapSigningTrust) != nil || state.MapSignature == nil || !m.now().Before(state.MapSignature.ExpiresAt) {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	search := strings.ToLower(strings.TrimSpace(request.GetSearch()))
	items := []*ipc.Resource{}
	searchTargets := map[string]string{}
	add := func(kind ipc.ResourceKind, key, name, targetText string, resource *ipc.Resource) {
		resource.Id = rpcResourceID(kind, key)
		searchTargets[resource.Id] = strings.ToLower(name + "\n" + targetText)
		resource.Kind, resource.DisplayName, resource.NetworkId = kind, name, state.Network.ID
		// Policy resolution is separate from observed runtime reachability.
		resource.Availability = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "resource_runtime_observation_unavailable"}
		items = append(items, resource)
	}
	// A producer can explicitly identify a /32 or /128 as a subnet resource.
	// Preserve that policy target in addition to the peer's host addresses.
	// Ordinary host addresses without such a declaration remain host-only.
	managedSubnets := map[string]bool{}
	if policy := state.Network.ClientPolicy; policy != nil {
		for _, setting := range policy.Resources {
			if setting.Kind == api.ManagedResourceSubnet {
				prefix, err := netip.ParsePrefix(setting.CIDR)
				if err != nil {
					return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
				}
				managedSubnets[setting.ID+"\x00"+prefix.Masked().String()] = true
			}
		}
	}
	for _, p := range state.Peers {
		addresses := []string{}
		seen := map[string]bool{}
		for _, value := range p.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			}
			prefix = prefix.Masked()
			if prefix.Bits() == 0 || seen[prefix.String()] {
				continue
			}
			seen[prefix.String()] = true
			if prefix.IsSingleIP() {
				addresses = append(addresses, prefix.Addr().String())
				if !managedSubnets[p.ID+"\x00"+prefix.String()] {
					continue
				}
			}
			add(ipc.ResourceKind_RESOURCE_KIND_SUBNET, p.ID+"\x00"+prefix.String(), p.Hostname, prefix.String(), &ipc.Resource{Target: &ipc.Resource_Subnet{Subnet: &ipc.SubnetTarget{Cidr: prefix.String()}}})
		}
		sort.Strings(addresses)
		if len(addresses) > 0 {
			add(ipc.ResourceKind_RESOURCE_KIND_HOST, p.ID, p.Hostname, strings.Join(addresses, " "), &ipc.Resource{Target: &ipc.Resource_Host{Host: &ipc.HostTarget{Addresses: addresses, Hostname: p.Hostname}}})
		}
	}
	for _, service := range state.Network.Services {
		for _, port := range service.Ports {
			portText := strconv.FormatUint(uint64(port.Port), 10)
			key := service.ID + "\x00" + port.Protocol + "\x00" + portText
			targetText := service.DNSName + ":" + portText + " " + port.Protocol
			add(ipc.ResourceKind_RESOURCE_KIND_SERVICE, key, service.Name, targetText, &ipc.Resource{Target: &ipc.Resource_Service{Service: &ipc.ServiceTarget{Hostname: service.DNSName, Port: port.Port, Protocol: port.Protocol}}})
		}
	}
	for _, app := range state.Network.Applications {
		if !slices.Contains(app.Sources, applicationSelf(*state)) {
			continue
		}
		add(ipc.ResourceKind_RESOURCE_KIND_APPLICATION, app.ID, app.Name, app.Target, &ipc.Resource{Target: &ipc.Resource_Application{Application: &ipc.ApplicationTarget{DisplayAddress: app.Target}}})
	}
	if len(items) > 4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Id < items[j].Id })
	if err := projectResourceOverlaps(state, items); err != nil {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	for _, resource := range items {
		identity, err := resolveResourceInAuthenticatedMap(state, resource.Id)
		if err != nil {
			return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		resource.Enabled = rpcResourceSetting(cfg, resource.Id, identity, ready)
	}
	items = slices.DeleteFunc(items, func(resource *ipc.Resource) bool {
		return (len(kinds) > 0 && !slices.Contains(kinds, resource.Kind)) || !strings.Contains(searchTargets[resource.Id], search)
	})
	response := &ipc.ListResourcesResponse{Resources: items}
	if proto.Size(response) > rpc.MaxResponseBytes-4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(response)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	// Bind tokens to query even when two filters happen to return equal rows.
	query, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&ipc.ListResourcesRequest{Search: search, Kinds: kinds})
	if err != nil {
		return nil, err
	}
	queryDigest := sha256.Sum256(query)
	start, end, next, err := m.pageRange(peer, "ListResources\x00"+profile.ID+"\x00"+state.MapSignature.PayloadHash+"\x00"+hex.EncodeToString(queryDigest[:])+"\x00"+hex.EncodeToString(digest[:]), request.GetPage(), cfg, len(items))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	response.Resources = items[start:end]
	response.Page = &ipc.PageResponse{NextPageToken: next, Metadata: &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(m.now())}}
	return response, nil
}
