package client

import (
	"context"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ClientRPCService) GetUpdateInfo(ctx context.Context, request *connect.Request[ipc.GetUpdateInfoRequest]) (*connect.Response[ipc.GetUpdateInfoResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.updateInfoAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) updateInfoAs(peer local.Peer, request *ipc.GetUpdateInfoRequest) (*ipc.GetUpdateInfoResponse, error) {
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	cfg := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetUpdateInfo"), cfg); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	var reported *ipc.BuildIdentity
	if request.ReportedUi != nil {
		if len(request.ReportedUi.ProtoReflect().GetUnknown()) != 0 || request.ReportedUi.Platform.Descriptor().Values().ByNumber(request.ReportedUi.Platform.Number()) == nil {
			return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		reported = proto.Clone(request.ReportedUi).(*ipc.BuildIdentity)
	}
	revision := uint64(1)
	if cfg.RPCState != nil {
		revision = cfg.RPCState.Revision
	}
	// No verified release source is configured in this runtime. In particular,
	// matching version strings or an authenticated IPC connection do not attest
	// an installed UI/core artifact pair. Do not synthesize a compatible result,
	// an up-to-date verdict, URLs or an installer action from caller claims.
	return &ipc.GetUpdateInfoResponse{Info: &ipc.UpdateInfo{
		Metadata:         &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: revision, GeneratedAt: timestamppb.New(s.mutations.now())},
		InstalledRuntime: proto.Clone(s.build).(*ipc.BuildIdentity),
		ReportedUi:       reported,
		InstalledPair:    &ipc.Compatibility{State: ipc.CompatibilityState_COMPATIBILITY_STATE_UNKNOWN, ReasonKey: "installed_pair_not_verified"},
		State:            ipc.UpdateState_UPDATE_STATE_SOURCE_UNAVAILABLE,
		Discovery:        &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "update_source_not_configured", ActionOwner: ipc.ActionOwner_ACTION_OWNER_SUPPORT},
	}}, nil
}
