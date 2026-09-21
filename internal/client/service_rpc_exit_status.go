package client

import (
	"context"
	"unicode/utf8"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ClientRPCService) GetExitNode(ctx context.Context, request *connect.Request[ipc.GetExitNodeRequest]) (*connect.Response[ipc.GetExitNodeResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	status, err := s.exitNodeAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&ipc.GetExitNodeResponse{Status: status}), nil
}

// Durable request is readable even when its former grant is withdrawn. It is
// never promoted to effective routing or OS protection without observation.
func (s *ClientRPCService) exitRequestedStatusLocked(ctx context.Context, peer local.Peer, request *ipc.GetExitNodeRequest, cfg Config, workerReady bool) (*ipc.ExitNodeStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := s.mutations
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetExitNode"), cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.GetProfile())
	if err != nil {
		return nil, err
	}
	selection := cfg.ExitSelection
	pending := false
	if profile.ID != cfg.RPCState.ActiveProfileID {
		selection = profile.Configuration.ExitSelection
	} else if plan := cfg.RPCState.ExitChange; plan != nil {
		if plan.ProfileID != profile.ID {
			return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		selection = plan.Requested
		pending = true
	}
	unknown := func() *ipc.ExitFamilyStatus {
		return &ipc.ExitFamilyStatus{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "exit_runtime_observation_unavailable"}}
	}
	status := &ipc.ExitNodeStatus{ProfileId: profile.ID, RequestedFamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_NONE, Ipv4: unknown(), Ipv6: unknown(), Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "exit_runtime_observation_unavailable"}, Control: &ipc.SettingControl{Source: ipc.SettingSource_SETTING_SOURCE_DEFAULT, Mutation: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "exit_executor_unavailable"}}}
	// The aggregate control includes Clear, which intentionally remains possible
	// without a current grant. Per-node selection support belongs to the catalog.
	status.Control.Mutation = m.exitMutationRestriction(cfg, request.GetProfile(), ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE, workerReady)
	if selection != nil {
		family := map[api.ExitFamilyMode]ipc.ExitFamilyMode{api.ExitFamilyIPv4Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, api.ExitFamilyIPv6Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, api.ExitFamilyDualStack: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[selection.Family]
		lan := map[api.ExitLANAccess]ipc.LanAccess{api.ExitLANBlock: ipc.LanAccess_LAN_ACCESS_BLOCK, api.ExitLANAllow: ipc.LanAccess_LAN_ACCESS_ALLOW}[selection.LAN]
		if selection.ID == "" || len(selection.ID) > 256 || !utf8.ValidString(selection.ID) || family == 0 || lan == 0 {
			return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		status.RequestedExitNodeId = proto.String(selection.ID)
		status.RequestedFamilyMode, status.RequestedLanAccess = family, lan
		status.Control.Source = ipc.SettingSource_SETTING_SOURCE_USER
		if selection.Family != api.ExitFamilyIPv6Only {
			status.Ipv4.RequestedExitNodeId = proto.String(selection.ID)
		}
		if selection.Family != api.ExitFamilyIPv4Only {
			status.Ipv6.RequestedExitNodeId = proto.String(selection.ID)
		}
	}
	if pending {
		status.Control.Source = ipc.SettingSource_SETTING_SOURCE_USER
		status.ApplyState = ipc.ApplyState_APPLY_STATE_PENDING
		status.Ipv4.ApplyState = ipc.ApplyState_APPLY_STATE_PENDING
		status.Ipv6.ApplyState = ipc.ApplyState_APPLY_STATE_PENDING
		if plan := cfg.RPCState.ExitChange; plan.Containing {
			status.Failure = &ipc.Failure{Code: plan.FailureCode, ReasonKey: plan.FailureReason}
			status.Control.Mutation = &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: "exit_containment_pending"}
		}
	}
	status.Metadata = &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(m.now())}
	return status, ctx.Err()
}

// Reuse admission's scope and pending-operation checks on a disposable snapshot.
// This projects ability to accept work, never evidence of applied OS effects or
// a promise that a later request with stale CAS metadata will be accepted.
func (m *ClientRPCMutations) exitMutationRestriction(cfg Config, ref *ipc.ProfileRef, kind ipc.OperationKind, workerReady bool) *ipc.Restriction {
	unavailable := func(reason string) *ipc.Restriction {
		return &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE, ReasonKey: reason}
	}
	if !workerReady {
		return unavailable("exit_executor_unavailable")
	}
	if cfg.RPCState == nil {
		return unavailable("exit_context_unavailable")
	}
	retained := 0
	for _, record := range cfg.RPCState.Operations {
		if record.CompletedAt == nil || m.now().Before(record.CompletedAt.Add(rpcOperationRetention)) {
			retained++
		}
	}
	if retained >= rpcMaxOperationRecords {
		return unavailable("exit_operation_capacity")
	}
	copy := clonePersistentConfig(cfg)
	if _, err := m.prepareExitChange(&copy, &ipc.Operation{Kind: kind}, ref); err != nil {
		return unavailable("exit_context_unavailable")
	}
	return &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_AVAILABLE}
}
