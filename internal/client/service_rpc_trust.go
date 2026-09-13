package client

import (
	"encoding/hex"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Admission records confirmation but does not adopt trust or erase credentials.
// The executor must refetch and match the announcement before any side effect.
type clientRPCTrust struct {
	OperationID    string `json:"operation_id"`
	ControlOrigin  string `json:"control_origin"`
	KeyID          string `json:"key_id"`
	AnnouncementID string `json:"announcement_id"`
	Authority      []byte `json:"authority"`
}

func (m *ClientRPCMutations) trustServerIdentityAs(peer local.Peer, request *ipc.TrustServerIdentityRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/TrustServerIdentity", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if request.ConfirmedControlOrigin != profile.ControlOrigin || strings.TrimSpace(request.ConfirmedKeyId) == "" {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		announcement, err := hex.DecodeString(request.ConfirmedAnnouncementId)
		if err != nil || len(announcement) != 32 {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if _, err := SigningTrustBundle(*cfg); err != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
		}
		if cfg.RPCState.Trust != nil || cfg.EnrollmentRecovery != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, pending); err != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		op.ProfileId = profile.ID
		cfg.RPCState.Trust = &clientRPCTrust{OperationID: op.Id, ControlOrigin: request.ConfirmedControlOrigin,
			KeyID: request.ConfirmedKeyId, AnnouncementID: request.ConfirmedAnnouncementId, Authority: logoutAuthority(*cfg)}
		return nil
	})
	return op, err
}
