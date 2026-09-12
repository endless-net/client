package testcontrol

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"sort"
	"strings"

	"connectrpc.com/connect"
	rpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	bindings "github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"google.golang.org/protobuf/proto"
)

type rpcServer struct {
	bindings.UnimplementedUserServiceHandler
	bindings.UnimplementedConnectorServiceHandler
	bindings.UnimplementedFlowLogServiceHandler
	s *Server
}

func (s *Server) mountRPC(mux *http.ServeMux) {
	h := &rpcServer{s: s}
	p, v := bindings.NewUserServiceHandler(h)
	mux.Handle(p, v)
	p, v = bindings.NewConnectorServiceHandler(h)
	mux.Handle(p, v)
	p, v = bindings.NewFlowLogServiceHandler(h)
	mux.Handle(p, v)
}
func denied() error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("test authorization denied"))
}
func (h *rpcServer) session(header http.Header) bool {
	return h.s.session != "" && header.Get("Authorization") == "Bearer "+h.s.session
}
func (h *rpcServer) ListAccounts(_ context.Context, r *connect.Request[rpc.ListAccountsRequest]) (*connect.Response[rpc.ListAccountsResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if !h.session(r.Header()) {
		h.s.recordLocked(Event{Kind: "user-accounts-denied"})
		return nil, denied()
	}
	h.s.recordLocked(Event{Kind: "user-accounts-accepted"})
	return connect.NewResponse(&rpc.ListAccountsResponse{Accounts: []*rpc.Account{{AccountId: "test-account", Name: "Test account", Status: "active"}}, Page: &rpc.PageResponse{}}), nil
}
func (h *rpcServer) ListNetworks(_ context.Context, r *connect.Request[rpc.ListNetworksRequest]) (*connect.Response[rpc.ListNetworksResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if !h.session(r.Header()) || r.Msg.AccountId != "test-account" {
		return nil, denied()
	}
	out := &rpc.ListNetworksResponse{Page: &rpc.PageResponse{}}
	for _, n := range h.s.networks {
		out.Networks = append(out.Networks, &rpc.Network{NetworkId: n.ID, Name: n.Name})
	}
	sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].NetworkId < out.Networks[j].NetworkId })
	return connect.NewResponse(out), nil
}
func (h *rpcServer) ListAdvertisedRoutes(_ context.Context, r *connect.Request[rpc.ListAdvertisedRoutesRequest]) (*connect.Response[rpc.ListAdvertisedRoutesResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if !h.session(r.Header()) {
		return nil, denied()
	}
	if _, ok := h.s.networks[r.Msg.NetworkId]; !ok {
		return nil, denied()
	}
	out := &rpc.ListAdvertisedRoutesResponse{Page: &rpc.PageResponse{}}
	for _, n := range h.s.nodes {
		if n.Map.Network.ID == r.Msg.NetworkId {
			for _, cidr := range n.Map.Node.AdvertisedIPs {
				out.Routes = append(out.Routes, &rpc.AdvertisedRoute{NetworkId: r.Msg.NetworkId, NodeId: n.Map.Node.ID, Hostname: n.Map.Node.Hostname, Cidr: cidr})
			}
		}
	}
	return connect.NewResponse(out), nil
}
func (h *rpcServer) ReportApplicationDiscovery(_ context.Context, r *connect.Request[rpc.ReportApplicationDiscoveryRequest]) (*connect.Response[rpc.ReportApplicationDiscoveryResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	n, code := h.s.credentialLocked(strings.TrimPrefix(r.Header().Get("Authorization"), "Bearer "), r.Msg.NodeId, "node:map")
	if code != "" {
		return nil, denied()
	}
	if len(r.Msg.Addresses) == 0 || r.Msg.TtlSeconds == 0 || r.Msg.TtlSeconds > 3600 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid discovery lease"))
	}
	for _, address := range r.Msg.Addresses {
		if _, err := netip.ParseAddr(address); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid discovery address"))
		}
	}
	// No discovery lease is granted by default. Tests must publish an application
	// with an explicit connector before reporting for its current policy.
	for _, a := range n.Map.Network.Applications {
		if a.ID == r.Msg.ApplicationId && a.PolicyHash == r.Msg.PolicyHash {
			for _, c := range a.Connectors {
				if c.NodeID == r.Msg.NodeId {
					h.s.recordLocked(Event{Kind: "discovery", NodeID: r.Msg.NodeId})
					return connect.NewResponse(&rpc.ReportApplicationDiscoveryResponse{}), nil
				}
			}
		}
	}
	return nil, denied()
}
func (h *rpcServer) GetFlowLogPolicy(_ context.Context, r *connect.Request[rpc.GetFlowLogPolicyRequest]) (*connect.Response[rpc.GetFlowLogPolicyResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if _, code := h.s.credentialLocked(strings.TrimPrefix(r.Header().Get("Authorization"), "Bearer "), r.Msg.NodeId, "node:map"); code != "" {
		return nil, denied()
	}
	policy := h.s.flowPolicyLocked(r.Msg.NodeId)
	kind := "flow-policy-disabled"
	if policy.ConsentVersion != 0 {
		kind = "flow-policy-granted"
	}
	h.s.recordLocked(Event{Kind: kind, NodeID: r.Msg.NodeId})
	return connect.NewResponse(policy), nil
}
func (h *rpcServer) ReportFlowLog(_ context.Context, r *connect.Request[rpc.ReportFlowLogRequest]) (*connect.Response[rpc.ReportFlowLogResponse], error) {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if _, code := h.s.credentialLocked(strings.TrimPrefix(r.Header().Get("Authorization"), "Bearer "), r.Msg.NodeId, "node:map"); code != "" {
		return nil, denied()
	}
	h.s.flowReports = append(h.s.flowReports, proto.Clone(r.Msg).(*rpc.ReportFlowLogRequest))
	if err := h.s.acceptFlowLocked(r.Msg); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if h.s.loseFlowAcknowledgement {
		h.s.loseFlowAcknowledgement = false
		h.s.recordLocked(Event{Kind: "flow-ack-lost", NodeID: r.Msg.NodeId, Path: r.Msg.Window.WindowId})
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("test flow acknowledgement unavailable"))
	}
	return connect.NewResponse(&rpc.ReportFlowLogResponse{WindowId: r.Msg.Window.WindowId}), nil
}
