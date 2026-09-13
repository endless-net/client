package client

import (
	"reflect"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// ClientRPCLogoutProgress records independently confirmed remote effects.
// A provider must persist a successful step before proceeding to the next one.
type ClientRPCLogoutProgress struct {
	NodeRevoked    bool `json:"node_revoked,omitempty"`
	SessionRevoked bool `json:"session_revoked,omitempty"`
}

type clientRPCLogout struct {
	OperationID string                  `json:"operation_id"`
	Progress    ClientRPCLogoutProgress `json:"progress"`
}

func (m *ClientRPCMutations) logoutAs(peer local.Peer, request *ipc.LogoutRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/Logout", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.RPCState.Logout != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if proto.Unmarshal(record.Operation, pending) != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		if cfg.NodeCredential == "" && cfg.Token == "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
		}
		op.ProfileId = profile.ID
		cfg.RPCState.Logout = &clientRPCLogout{OperationID: op.Id}
		return nil
	})
	return op, err
}

// LogoutProgressCallback is runtime-only. It never removes credentials or
// changes intent. Stale registration context invalidates remote confirmation.
func (m *ClientRPCMutations) LogoutProgressCallback(operationID string, initial Config) func(ClientRPCLogoutProgress) error {
	expected := clonePersistentConfig(initial)
	return func(progress ClientRPCLogoutProgress) error {
		_, err := m.ReconcileOperation(operationID, func(cfg *Config, op *ipc.Operation) error {
			plan := cfg.RPCState.Logout
			if plan == nil || plan.OperationID != operationID || op.Kind != ipc.OperationKind_OPERATION_KIND_LOGOUT || op.State != ipc.OperationState_OPERATION_STATE_RUNNING ||
				expected.RPCState == nil || cfg.RPCState.ActiveProfileID != op.ProfileId || expected.RPCState.ActiveProfileID != op.ProfileId ||
				cfg.LocalOwnerID != expected.LocalOwnerID || cfg.ActiveAccountID != expected.ActiveAccountID || cfg.ManagementURL != expected.ManagementURL ||
				!reflect.DeepEqual(cfg.ControlPlaneURLs, expected.ControlPlaneURLs) || !reflect.DeepEqual(enrollmentFields(*cfg), enrollmentFields(expected)) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			if plan.Progress.NodeRevoked && !progress.NodeRevoked || plan.Progress.SessionRevoked && !progress.SessionRevoked || progress.SessionRevoked && !progress.NodeRevoked {
				return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
			}
			plan.Progress = progress
			return nil
		})
		return err
	}
}
