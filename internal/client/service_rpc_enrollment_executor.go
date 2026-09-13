package client

import (
	"context"
	"net/url"
	"reflect"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ClientRPCEnrollmentInput is runtime-private authorization, never a response.
type ClientRPCEnrollmentInput struct {
	OperationID string
	Mode        ipc.EnrollmentMode
	Hostname    string
	Token       string
	Browser     bool
}

// A provider performs one registration/approval attempt, verifies all returned
// authority and saves via the supplied transaction callback before returning.
// A non-nil action leaves the operation waiting for a later reconciliation.
type ClientRPCEnrollmentProvider func(context.Context, Config, ClientRPCEnrollmentInput, func(Config) error) (*ipc.UserAction, error)

func (m *ClientRPCMutations) ReconcileEnrollment(ctx context.Context, provider ClientRPCEnrollmentProvider) error {
	if provider == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.enrollmentWorker.Lock()
	defer m.enrollmentWorker.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.Enrollment == nil {
		return nil
	}
	plan := *cfg.RPCState.Enrollment
	_, err := m.ReconcileOperation(plan.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if op.Kind != ipc.OperationKind_OPERATION_KIND_ENROLL || op.ProfileId != cfg.RPCState.ActiveProfileID || cfg.RPCState.ProfileSwitch != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		op.UserAction = nil
		return nil
	})
	if err != nil {
		return err
	}
	cfg = m.store.Read()
	var checkpointErr error
	lastSaved := clonePersistentConfig(cfg)
	save := m.EnrollmentSaveCallback(plan.OperationID, cfg)
	action, executeErr := provider(ctx, cfg, ClientRPCEnrollmentInput(plan), func(next Config) error {
		if checkpointErr != nil {
			return checkpointErr
		}
		checkpointErr = save(next)
		if checkpointErr == nil {
			lastSaved = clonePersistentConfig(next)
		}
		return checkpointErr
	})
	// Lifecycle cancellation and checkpoint failures preserve the durable plan;
	// a restarted worker resumes it without needing a replay from the caller.
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkpointErr != nil {
		return checkpointErr
	}
	// An ambiguous registration response must retain its original plan and
	// idempotency key. The worker retries it on its bounded polling cadence.
	if failure := rpc.FailureFromError(executeErr); failure != nil && failure.Code == ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
		return nil
	}
	if action != nil && !validEnrollmentAction(action) {
		executeErr = rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(cfg *Config, op *ipc.Operation) error {
		if op.ProfileId != cfg.RPCState.ActiveProfileID || cfg.RPCState.Enrollment == nil || cfg.RPCState.Enrollment.OperationID != plan.OperationID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if cfg.LocalOwnerID != lastSaved.LocalOwnerID || cfg.ActiveAccountID != lastSaved.ActiveAccountID ||
			!reflect.DeepEqual(cfg.ControlPlaneURLs, lastSaved.ControlPlaneURLs) ||
			!reflect.DeepEqual(enrollmentFields(*cfg), enrollmentFields(lastSaved)) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if executeErr != nil {
			failure := rpc.FailureFromError(executeErr)
			if failure == nil {
				failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_INTERNAL, ReasonKey: "enrollment_failed"}
			}
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
			return nil
		}
		if action != nil {
			op.State = ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER
			op.UserAction = action
			return nil
		}
		if cfg.NodeID == "" || cfg.CachedMap == nil || cfg.CachedMap.Node.ID != cfg.NodeID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
		op.Outcome = &ipc.Operation_Enrollment{Enrollment: &ipc.EnrollmentResult{ProfileId: op.ProfileId, NodeId: cfg.NodeID}}
		return nil
	})
	return err
}

func validEnrollmentAction(action *ipc.UserAction) bool {
	if action.Kind != ipc.UserAction_KIND_OPEN_BROWSER && action.Kind != ipc.UserAction_KIND_WAIT_FOR_APPROVAL {
		return false
	}
	if action.BrowserUrl == "" {
		return action.Kind == ipc.UserAction_KIND_WAIT_FOR_APPROVAL
	}
	u, err := url.Parse(action.BrowserUrl)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(action.BrowserUrl, "\r\n")
}
