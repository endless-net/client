package client

import (
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// The protected runtime plan, unlike the observable operation, retains the
// enrollment authorization required to resume accepted work after a crash.
// It must be cleared by terminal reconciliation, never returned or logged.
type clientRPCEnrollment struct {
	OperationID     string             `json:"operation_id"`
	Mode            ipc.EnrollmentMode `json:"mode"`
	Hostname        string             `json:"hostname,omitempty"`
	Token           string             `json:"token,omitempty"`
	Browser         bool               `json:"browser,omitempty"`
	CancelRequested bool               `json:"cancel_requested,omitempty"`
}

func (m *ClientRPCMutations) enrollAs(peer local.Peer, request *ipc.EnrollRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/Enroll", request, func(cfg *Config, op *ipc.Operation) error {
		invalid := func() error { return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) }
		if request.Mode < ipc.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION || request.Mode > ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE ||
			!utf8.ValidString(request.Hostname) || len(request.Hostname) > 253 || strings.TrimSpace(request.Hostname) != request.Hostname ||
			strings.IndexFunc(request.Hostname, unicode.IsControl) >= 0 {
			return invalid()
		}
		plan := &clientRPCEnrollment{OperationID: op.Id, Mode: request.Mode, Hostname: request.Hostname}
		switch auth := request.Authentication.(type) {
		case *ipc.EnrollRequest_EnrollmentToken:
			if auth == nil || strings.TrimSpace(auth.EnrollmentToken) == "" || len(auth.EnrollmentToken) > 16384 ||
				strings.IndexFunc(auth.EnrollmentToken, unicode.IsSpace) >= 0 || strings.IndexFunc(auth.EnrollmentToken, unicode.IsControl) >= 0 {
				return invalid()
			}
			plan.Token = auth.EnrollmentToken
		case *ipc.EnrollRequest_BrowserLogin:
			if auth == nil || !auth.BrowserLogin {
				return invalid()
			}
			plan.Browser = true
		default:
			return invalid()
		}
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.RPCState.Enrollment != nil || cfg.RPCState.ProfileSwitch != nil {
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
		if rpcConfigHasEnrollment(*cfg) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED)
		}
		urls := cfg.ControlURLs()
		if len(urls) == 0 {
			return invalid()
		}
		for _, address := range urls {
			origin, err := rpcProfileOrigin(address)
			if err != nil || origin != profile.ControlOrigin {
				return invalid()
			}
		}
		op.ProfileId = profile.ID
		cfg.RPCState.Enrollment = plan
		return nil
	})
	return op, err
}

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
			if current.RPCState.Enrollment != nil && current.RPCState.Enrollment.OperationID == operationID && current.RPCState.Enrollment.CancelRequested {
				return rpc.Error(connect.CodeCanceled, ipc.ErrorCode_ERROR_CODE_CANCELLED)
			}
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
	dst.NodeCredentialSigningTrust = src.NodeCredentialSigningTrust
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
