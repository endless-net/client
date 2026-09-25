package client

import (
	"context"
	"net/netip"
	"reflect"
	"runtime"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Observations must contain public interface metadata only, never config or
// arbitrary process output. Raw inspection error strings are not serialized.
type ClientRPCDiagnosticsObservation struct {
	OSVersion            string
	Tunnel               WireGuardInspection
	Interfaces           []NetworkInterfaceStatus
	TunnelBusy           bool
	VerifiedMap          bool
	DNS                  *ipc.DnsDiagnostics
	RouteConflicts       []OverlayCIDRConflict
	Peers                []*ipc.Peer
	TunnelPeers          []*ipc.TunnelPeer
	PeerFailures         []*ipc.Failure
	DefaultRoutePresent  bool
	DefaultRouteObserved bool
	// OSRoutes contains actual route lookups, never Tunnel.Routes (which may
	// be synthesized from desired router configuration). A partial collection
	// is permitted and does not establish completeness.
	OSRoutes []WireGuardRouteInspection
}
type ClientRPCDiagnosticsProvider func(context.Context) (ClientRPCDiagnosticsObservation, error)

func (s *ClientRPCService) GetDiagnostics(ctx context.Context, request *connect.Request[ipc.GetDiagnosticsRequest]) (*connect.Response[ipc.GetDiagnosticsResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.diagnosticsAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) diagnosticsAs(ctx context.Context, peer local.Peer, request *ipc.GetDiagnosticsRequest) (*ipc.GetDiagnosticsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/GetDiagnostics")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	// The engine represents only the active profile. Never label its inspection
	// with an inactive profile's identity.
	if profile.ID != cfg.RPCState.ActiveProfileID || s.DiagnosticsProvider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	observation, err := s.DiagnosticsProvider(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
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
	snapshot, err := s.mutations.snapshotLocked(peer, s.build, cfg)
	if err != nil {
		return nil, err
	}
	result := &ipc.Diagnostics{Metadata: snapshot.Status.Metadata, Client: proto.Clone(s.build).(*ipc.BuildIdentity),
		OsName: runtime.GOOS, OsVersion: observation.OSVersion, GoVersion: runtime.Version(), Status: snapshot.Status,
		Truncated: true, DefaultRoutePresent: observation.DefaultRoutePresent, Failures: []*ipc.Failure{
			{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_os_routes_not_collected"},
			{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_resource_observation_not_collected"},
		},
		Tunnel: &ipc.TunnelInspection{Ok: observation.Tunnel.OK, InterfaceName: observation.Tunnel.Interface}}
	if len(observation.OSVersion) > 256 || len(observation.Tunnel.Interface) > 256 || len(observation.Interfaces) > 256 || observation.Tunnel.MTU < 0 || observation.Tunnel.MTU > 65535 || observation.Tunnel.ListenPort < 0 || observation.Tunnel.ListenPort > 65535 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	result.Tunnel.Mtu = uint32(observation.Tunnel.MTU)
	if result.OsVersion == "" {
		result.Failures = append(result.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_os_version_unavailable"})
	}
	if !observation.DefaultRouteObserved {
		result.Failures = append(result.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_default_route_not_observed"})
	}
	result.Tunnel.ListenPort = uint32(observation.Tunnel.ListenPort)
	if !observation.Tunnel.OK || observation.Tunnel.Error != "" {
		result.Tunnel.Ok = false
		result.Tunnel.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "tunnel_inspection_failed"}
	}
	if observation.TunnelBusy {
		result.Tunnel.Ok = false
		result.Tunnel.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_BUSY, ReasonKey: "tunnel_inspection_busy"}
	}
	if !observation.VerifiedMap {
		result.Failures = append(result.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "diagnostics_verified_map_unavailable"})
	} else {
		if len(observation.OSRoutes) > 4096 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		for _, route := range observation.OSRoutes {
			if len(route.Target) > 256 || len(route.Interface) > 256 {
				return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
			}
			target, err := netip.ParseAddr(route.Target)
			if err != nil || target.Zone() != "" {
				return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			native := &ipc.RouteInspection{Target: target.String()}
			if route.Error != "" || route.Interface == "" {
				native.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "route_inspection_failed"}
			} else {
				native.InterfaceName = route.Interface
				// Derive the comparison from the observed interfaces, not a
				// provider boolean that could incorrectly assert tunnel use.
				native.UsesInterface = observation.Tunnel.Interface != "" && route.Interface == observation.Tunnel.Interface
			}
			result.Routes = append(result.Routes, native)
		}
		if len(result.Routes) > 0 {
			result.Failures[0] = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "diagnostics_os_routes_incomplete"}
		}
		if len(observation.Peers) > 4096 || len(observation.TunnelPeers) > 4096 || len(observation.PeerFailures) > 16 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		for _, peer := range observation.Peers {
			if peer != nil {
				result.Peers = append(result.Peers, proto.Clone(peer).(*ipc.Peer))
			}
		}
		for _, peer := range observation.TunnelPeers {
			if peer != nil && !observation.TunnelBusy && observation.Tunnel.OK && observation.Tunnel.Error == "" {
				result.Tunnel.Peers = append(result.Tunnel.Peers, proto.Clone(peer).(*ipc.TunnelPeer))
			}
		}
		for _, failure := range observation.PeerFailures {
			if failure != nil {
				result.Failures = append(result.Failures, proto.Clone(failure).(*ipc.Failure))
			}
		}
		if observation.DNS != nil {
			// This is signed map configuration. No resolver readback or DNS
			// query is available in this observation contract.
			result.Dns = proto.Clone(observation.DNS).(*ipc.DnsDiagnostics)
			result.Failures = append(result.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_os_resolver_not_observed"})
		}
		if len(observation.RouteConflicts) > 4096 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		for _, conflict := range observation.RouteConflicts {
			result.RouteConflicts = append(result.RouteConflicts, &ipc.RouteConflict{OverlayCidr: conflict.OverlayCIDR, LocalPrefix: conflict.LocalPrefix, InterfaceName: conflict.Interface, ReasonKey: "overlay_prefix_overlap"})
		}
	}
	// Peer keys/endpoints, routes and DNS require a verified profile map join.
	// Do not infer identities or absence from a partial engine observation.
	for _, item := range observation.Interfaces {
		// net/interface_windows.go maps the OS sentinel 0xffffffff to -1.
		// Represent unavailable MTU explicitly, never wrap it into uint32.
		mtuUnavailable := item.MTU == -1
		mtu := item.MTU
		if mtuUnavailable {
			mtu = 0
		}
		// OS interface MTU is not a tunnel packet-size limit: Linux loopback
		// uses 65536. Preserve the full uint32 contract range without wrapping.
		if item.Index < 0 || uint64(item.Index) > uint64(^uint32(0)) || mtu < 0 || uint64(mtu) > uint64(^uint32(0)) || len(item.Name) > 256 || len(item.Addresses) > 256 || len(item.Prefixes) > 256 || len(item.Flags) > 32 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		native := &ipc.Interface{Name: item.Name, Index: uint32(item.Index), Mtu: uint32(mtu), Addresses: append([]string(nil), item.Addresses...), Prefixes: append([]string(nil), item.Prefixes...), Flags: append([]string(nil), item.Flags...)}
		if mtuUnavailable {
			native.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "interface_mtu_unavailable"}
		}
		if item.Error != "" {
			native.Failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "interface_inspection_failed"}
		}
		result.Interfaces = append(result.Interfaces, native)
	}
	for _, log := range s.mutations.recentLogs {
		if log.profileID == profile.ID {
			result.RecentLogs = append(result.RecentLogs, proto.Clone(log.entry).(*ipc.LogEntry))
		}
	}
	// The live preview has the same private-field boundary as the archive.
	// Status may contain a browser action URL, and provider strings must never
	// bypass the diagnostic redactor merely because no bundle was requested.
	redactNativeDiagnosticsMessage(result.ProtoReflect())
	if proto.Size(result) > rpc.MaxResponseBytes-4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	return &ipc.GetDiagnosticsResponse{Diagnostics: result}, nil
}
