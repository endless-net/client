package client

import (
	"crypto/rand"
	"errors"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// AdoptInitialProfile assigns existing installation state to a v0 profile before
// serving. This is a one-time state cutover, not an old IPC compatibility path.
// It never changes ownership, credentials, keys, intent or the live tunnel.
func (m *ClientRPCMutations) AdoptInitialProfile() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.store.Update(func(cfg *Config) error {
		state := cfg.RPCState
		if state != nil && state.ActiveProfileID != "" {
			if _, exists := state.Profiles[state.ActiveProfileID]; !exists {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			return errRPCNoChange
		}
		if !rpcConfigHasEnrollment(*cfg) && len(cfg.ControlPlaneURLs) == 0 {
			return errRPCNoChange
		}
		// Never choose an arbitrary server for existing account credentials.
		origin := ""
		for _, raw := range cfg.ControlPlaneURLs {
			normalized, err := rpcProfileOrigin(raw)
			if err != nil {
				return err
			}
			if origin != "" && origin != normalized {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
			}
			origin = normalized
		}
		if origin == "" {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if state != nil {
			if state.ProfileSwitch != nil || state.DisconnectOperationID != "" {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
			if len(state.Profiles) >= maxRPCProfiles {
				return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
			}
			for _, record := range state.Operations {
				op := new(ipc.Operation)
				if proto.Unmarshal(record.Operation, op) != nil {
					return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
				}
				if !rpcOperationTerminal(op.State) {
					return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
				}
			}
			if state.Revision == 0 || len(state.DigestKey) != 32 || state.Operations == nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
		} else {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			state = &ClientRPCState{Revision: 1, DigestKey: key, Operations: map[string]clientRPCOperationRecord{}}
			cfg.RPCState = state
		}
		id, err := newRPCUUID()
		if err != nil {
			return err
		}
		if state.Profiles == nil {
			state.Profiles = map[string]clientRPCProfile{}
		}
		// Active configuration remains at the root until a verified handover
		// saves it. Do not create a second copy of installation secrets.
		state.Profiles[id] = clientRPCProfile{ID: id, DisplayName: "Default", ControlOrigin: origin}
		state.ActiveProfileID = id
		state.Revision++
		return nil
	})
	if errors.Is(err, errRPCNoChange) {
		return nil
	}
	return err
}
