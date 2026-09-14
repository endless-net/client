package client

import (
	"context"
	"reflect"
	"sync"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// NetworkSelectionSaveCallback persists registration checkpoints only inside
// the prepared target. The provider must verify producer signatures before
// supplying credential/map authority. This callback guards workflow bindings;
// it neither adopts that target nor reports selection success.
func (m *ClientRPCMutations) NetworkSelectionSaveCallback(ctx context.Context, operationID string, initial Config) func(Config) error {
	var mu sync.Mutex
	initial = clonePersistentConfig(initial)
	return func(updated Config) error {
		mu.Lock()
		defer mu.Unlock()
		stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
		if err := ctx.Err(); err != nil {
			return err
		}
		if initial.RPCState == nil || initial.RPCState.NetworkSelection == nil {
			return stale()
		}
		expected := initial.RPCState.NetworkSelection
		if expected.OperationID != operationID || expected.Target == nil || expected.Activated || expected.TargetRevoked || (expected.DownStarted && expected.AbortFailure == nil) {
			return stale()
		}
		next := clonePersistentConfig(updated)
		allowed := clonePersistentConfig(*expected.Target)
		copyEnrollmentFields(&allowed, next)
		if !reflect.DeepEqual(allowed, next) || next.NetworkID != expected.NetworkID ||
			next.Token != expected.Target.Token || next.PrivateKey != expected.Target.PrivateKey || next.IdentityPrivateKey != expected.Target.IdentityPrivateKey {
			return stale()
		}
		if pending := next.PendingDirectRegistration; pending != nil {
			request := pending.Request
			identity, identityErr := IdentityPublicKey(next.IdentityPrivateKey)
			public, publicErr := wgkeys.PublicKey(next.PrivateKey)
			if pending.Origin != expected.Profile.ControlOrigin || request.IdempotencyID != operationID || request.NetworkID != expected.NetworkID ||
				request.NetworkName != "" || request.AccountID != next.ActiveAccountID || request.JoinToken != "" || request.NodeCredential != "" ||
				request.SessionTokenBinding != api.RegistrationSessionTokenBinding(next.Token) || identityErr != nil || publicErr != nil ||
				request.IdentityPublicKey != identity || request.PublicKey != public || request.DeviceFingerprint != next.DeviceFingerprint || request.Validate() != nil {
				return stale()
			}
			if previous := expected.Target.PendingDirectRegistration; previous != nil && !reflect.DeepEqual(previous, pending) {
				return stale()
			}
		}
		if next.EnrollmentRequest != nil || next.EnrollmentRequestID != "" || next.EnrollmentPollToken != "" || next.ApprovalURL != "" {
			return stale()
		}
		if state := next.CachedMap; state != nil && (next.NodeID == "" || state.Node.ID != next.NodeID || state.Node.NetworkID != expected.NetworkID || state.Network.ID != expected.NetworkID || state.Network.AccountID != next.ActiveAccountID) {
			return stale()
		}
		_, err := m.ReconcileOperation(operationID, func(current *Config, op *ipc.Operation) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			pending := current.RPCState.NetworkSelection
			if op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || op.ProfileId != expected.Profile.ID ||
				pending == nil || !reflect.DeepEqual(pending, expected) {
				return stale()
			}
			if expected.AbortFailure == nil && !networkSelectionSourceMatches(*current, expected) {
				return stale()
			}
			if expected.AbortFailure != nil && !networkSelectionCleanupTargetIsolated(*current, expected, next) {
				return stale()
			}
			pending.Target = &next
			return nil
		})
		if err == nil {
			expected.Target = &next
		}
		return err
	}
}
