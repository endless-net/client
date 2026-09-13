package client

import (
	"context"
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
	OSVersion      string
	Tunnel         WireGuardInspection
	Interfaces     []NetworkInterfaceStatus
	TunnelBusy     bool
	VerifiedMap    bool
	DNS            *ipc.DnsDiagnostics
	RouteConflicts []OverlayCIDRConflict
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
		Truncated: true, Failures: []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_os_routes_peers_not_collected"}},
		Tunnel: &ipc.TunnelInspection{Ok: observation.Tunnel.OK, InterfaceName: observation.Tunnel.Interface}}
	if len(observation.OSVersion) > 256 || len(observation.Tunnel.Interface) > 256 || len(observation.Interfaces) > 256 || observation.Tunnel.MTU < 0 || observation.Tunnel.MTU > 65535 || observation.Tunnel.ListenPort < 0 || observation.Tunnel.ListenPort > 65535 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	result.Tunnel.Mtu = uint32(observation.Tunnel.MTU)
	if result.OsVersion == "" {
		result.Failures = append(result.Failures, &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, ReasonKey: "diagnostics_os_version_unavailable"})
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
		if observation.DNS != nil {
			result.Dns = proto.Clone(observation.DNS).(*ipc.DnsDiagnostics)
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
		if item.Index < 0 || uint64(item.Index) > uint64(^uint32(0)) || item.MTU < 0 || item.MTU > 65535 || len(item.Name) > 256 || len(item.Addresses) > 256 || len(item.Prefixes) > 256 || len(item.Flags) > 32 {
			return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		native := &ipc.Interface{Name: item.Name, Index: uint32(item.Index), Mtu: uint32(item.MTU), Addresses: append([]string(nil), item.Addresses...), Prefixes: append([]string(nil), item.Prefixes...), Flags: append([]string(nil), item.Flags...)}
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
	if proto.Size(result) > rpc.MaxResponseBytes-4096 {
		return nil, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	return &ipc.GetDiagnosticsResponse{Diagnostics: result}, nil
}
