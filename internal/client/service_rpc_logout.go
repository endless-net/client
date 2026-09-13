package client

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func (s *ClientRPCService) Logout(ctx context.Context, request *connect.Request[ipc.LogoutRequest]) (*connect.Response[ipc.LogoutResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	w := s.profileWorker
	if w == nil || w.ctx.Err() != nil || !w.logout {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	op, err := s.mutations.logoutAs(peer, request.Msg)
	if err != nil {
		return nil, err
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return connect.NewResponse(&ipc.LogoutResponse{Operation: op}), nil
}

// ClientRPCLogoutProgress records independently confirmed remote effects.
// A provider must persist a successful step before proceeding to the next one.
type ClientRPCLogoutProgress struct {
	NodeRevoked    bool `json:"node_revoked,omitempty"`
	SessionRevoked bool `json:"session_revoked,omitempty"`
}

type clientRPCLogout struct {
	OperationID string                  `json:"operation_id"`
	Progress    ClientRPCLogoutProgress `json:"progress"`
	DownStarted bool                    `json:"down_started,omitempty"`
}

type clientRPCLogoutConfirmation struct {
	Authority []byte                  `json:"authority"`
	Progress  ClientRPCLogoutProgress `json:"progress"`
	RequestID string                  `json:"request_id,omitempty"`
}

// Correlation is retained with the profile, independently of journal retention,
// and is only reused for the exact authority that produced the failed cleanup.
func rpcLocalCleanupRequestID(cfg Config, profileID string) string {
	profile := cfg.RPCState.Profiles[profileID]
	registration := cfg
	if profileID != cfg.RPCState.ActiveProfileID {
		registration = profile.Configuration
		registration.LocalOwnerID = cfg.LocalOwnerID
		registration.PrivateKey, registration.IdentityPrivateKey = cfg.PrivateKey, cfg.IdentityPrivateKey
		registration.RPCState = &ClientRPCState{ActiveProfileID: profileID, DigestKey: cfg.RPCState.DigestKey}
	}
	if confirmed := profile.LogoutConfirmation; confirmed != nil && hmac.Equal(confirmed.Authority, logoutAuthority(registration)) {
		return confirmed.RequestID
	}
	if registration.EnrollmentRecovery != nil {
		return registration.EnrollmentRecovery.RequestID
	}
	return ""
}

func logoutAuthority(cfg Config) []byte {
	if cfg.RPCState == nil {
		return nil
	}
	values := []string{cfg.RPCState.ActiveProfileID, cfg.LocalOwnerID, cfg.ActiveAccountID, cfg.ManagementURL, cfg.NodeID, cfg.NodeCredential, cfg.Token, cfg.DeviceFingerprint, cfg.PrivateKey, cfg.IdentityPrivateKey}
	values = append(values, cfg.ControlPlaneURLs...)
	encoded, _ := json.Marshal(values)
	hash := hmac.New(sha256.New, cfg.RPCState.DigestKey)
	_, _ = hash.Write(encoded)
	return hash.Sum(nil)
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
		if previous := profile.LogoutConfirmation; previous != nil && hmac.Equal(previous.Authority, logoutAuthority(*cfg)) {
			cfg.RPCState.Logout.Progress = previous.Progress
		}
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
				!hmac.Equal(logoutAuthority(*cfg), logoutAuthority(expected)) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			if plan.Progress.NodeRevoked && !progress.NodeRevoked || plan.Progress.SessionRevoked && !progress.SessionRevoked || progress.SessionRevoked && !progress.NodeRevoked {
				return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
			}
			plan.Progress = progress
			profile := cfg.RPCState.Profiles[op.ProfileId]
			profile.LogoutConfirmation = &clientRPCLogoutConfirmation{Authority: logoutAuthority(*cfg), Progress: progress}
			cfg.RPCState.Profiles[profile.ID] = profile
			return nil
		})
		return err
	}
}
