package client

import (
	"reflect"
	"sync"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// EnrollmentSaveCallback is a runtime-only persistence adapter for a running
// Enroll workflow. Every checkpoint updates the operation and enrollment fields
// atomically, retaining current intent, preferences, owner and the RPC journal.
// initial must be the configuration read for this operation, not a UI payload.
func (m *ClientRPCMutations) EnrollmentSaveCallback(operationID string, initial Config) func(Config) error {
	var mu sync.Mutex
	expected := clonePersistentConfig(initial)
	return func(updated Config) error {
		mu.Lock()
		defer mu.Unlock()
		next := clonePersistentConfig(updated)
		_, err := m.ReconcileOperation(operationID, func(current *Config, op *ipc.Operation) error {
			stale := func() error { return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE) }
			if op.Kind != ipc.OperationKind_OPERATION_KIND_ENROLL || op.State != ipc.OperationState_OPERATION_STATE_RUNNING ||
				expected.RPCState == nil || op.ProfileId == "" || op.ProfileId != expected.RPCState.ActiveProfileID ||
				op.ProfileId != current.RPCState.ActiveProfileID || current.RPCState.ProfileSwitch != nil ||
				current.LocalOwnerID != expected.LocalOwnerID {
				return stale()
			}
			profile, exists := current.RPCState.Profiles[op.ProfileId]
			original, wasPresent := expected.RPCState.Profiles[op.ProfileId]
			if !exists || !wasPresent || profile.ControlOrigin != original.ControlOrigin ||
				!reflect.DeepEqual(current.ControlPlaneURLs, expected.ControlPlaneURLs) ||
				current.ManagementURL != expected.ManagementURL || current.ActiveAccountID != expected.ActiveAccountID ||
				!reflect.DeepEqual(current.EnrollmentRecovery, expected.EnrollmentRecovery) ||
				!reflect.DeepEqual(enrollmentFields(*current), enrollmentFields(expected)) {
				return stale()
			}
			// Registration may create missing installation keys, never rotate an
			// existing identity as a side effect of an enrollment checkpoint.
			if expected.PrivateKey != "" && next.PrivateKey != expected.PrivateKey ||
				expected.IdentityPrivateKey != "" && next.IdentityPrivateKey != expected.IdentityPrivateKey {
				return stale()
			}
			copyEnrollmentFields(current, next)
			return nil
		})
		if err == nil {
			copyEnrollmentFields(&expected, next)
		}
		return err
	}
}

func enrollmentFields(cfg Config) Config {
	var result Config
	copyEnrollmentFields(&result, cfg)
	return result
}

// Explicit allowlist: adding a config field does not silently authorize a
// background enrollment response to overwrite another subsystem's state.
func copyEnrollmentFields(dst *Config, src Config) {
	dst.Token = src.Token
	dst.IdentityPrivateKey = src.IdentityPrivateKey
	dst.PrivateKey = src.PrivateKey
	dst.NodeID = src.NodeID
	dst.NetworkID = src.NetworkID
	dst.NodeCredential = src.NodeCredential
	dst.NodeApprovalState = src.NodeApprovalState
	dst.EnrollmentRequestID = src.EnrollmentRequestID
	dst.EnrollmentPollToken = src.EnrollmentPollToken
	dst.ApprovalURL = src.ApprovalURL
	dst.EnrollmentRequest = src.EnrollmentRequest
	dst.PendingDirectRegistration = src.PendingDirectRegistration
	dst.DeviceFingerprint = src.DeviceFingerprint
	dst.MapSigningTrust = src.MapSigningTrust
	dst.MapRevision = src.MapRevision
	dst.MapGlobalRevision = src.MapGlobalRevision
	dst.MapHash = src.MapHash
	dst.CachedMap = src.CachedMap
	dst.CachedMapSavedAt = src.CachedMapSavedAt
}
