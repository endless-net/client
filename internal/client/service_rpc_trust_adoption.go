package client

import (
	"context"
	"crypto/hmac"
	"reflect"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// ReconcileTrustAdoption stops the old tunnel before atomically adopting the
// confirmed bundle and preparing credential recovery. It does not perform
// recovery or restart the tunnel; enrolled operations remain RUNNING.
func (m *ClientRPCMutations) ReconcileTrustAdoption(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Stop == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	// An idle trust worker must stay available for a new announcement instead
	// of waiting behind an unrelated tunnel iteration. Recheck after acquiring
	// the locks below before acting on any actual plan.
	initial := m.store.Read()
	if initial.RPCState == nil || initial.RPCState.Trust == nil || initial.RPCState.Trust.Adopted {
		return ctx.Err()
	}
	m.trustWorker.Lock()
	defer m.trustWorker.Unlock()
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.Trust == nil || cfg.RPCState.Trust.Adopted {
		return nil
	}
	plan := *cfg.RPCState.Trust
	if plan.Announced == nil {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	ready := false
	_, err := m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !rpcTrustConfirmationMatches(*current, op, plan) {
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "trust_confirmation_stale"}}
			return nil
		}
		if !plan.DownStarted && reflect.DeepEqual(current.MapSigningTrust, plan.Announced) {
			op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
			op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: false}}
			return nil
		}
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		current.RPCState.Trust.DownStarted = true
		ready = true
		return nil
	})
	if err != nil || !ready {
		return err
	}
	continuity, stopErr := driver.Stop(ctx)
	if err := ctx.Err(); err != nil {
		return err // Durable DownStarted makes uncertain completion resumable.
	}
	if continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED &&
		(plan.DownStarted || continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE) {
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	_, err = m.ReconcileOperation(plan.OperationID, func(current *Config, op *ipc.Operation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		op.Continuity = continuity
		if stopErr != nil || !rpcTrustConfirmationMatches(*current, op, plan) {
			failure := &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "trust_confirmation_stale"}
			if stopErr != nil {
				failure.Code, failure.ReasonKey = ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, "trust_down_failed"
			}
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
			return nil
		}
		if err := ReplaceSigningTrustBundle(current, *plan.Announced); err != nil {
			return err
		}
		profile := current.RPCState.Profiles[op.ProfileId]
		if err := ReplaceSigningTrustBundle(&profile.Configuration, *plan.Announced); err != nil {
			return err
		}
		if current.NodeCredential != "" {
			recovery, err := NewEnrollmentRecovery(op.Id, op.Id, plan.ControlOrigin, plan.KeyID, m.now())
			if err != nil {
				return err
			}
			current.EnrollmentRecovery = &recovery
			savedRecovery := recovery
			profile.Configuration.EnrollmentRecovery = &savedRecovery
			current.RPCState.Trust.Adopted = true
		} else {
			op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: true}}
		}
		current.RPCState.Profiles[profile.ID] = profile
		return nil
	})
	return err
}

func rpcTrustConfirmationMatches(cfg Config, op *ipc.Operation, plan clientRPCTrust) bool {
	if cfg.RPCState == nil || cfg.RPCState.Trust == nil || cfg.RPCState.Trust.OperationID != plan.OperationID ||
		op.Kind != ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY || op.ProfileId != cfg.RPCState.ActiveProfileID ||
		cfg.RPCState.Profiles[op.ProfileId].ControlOrigin != plan.ControlOrigin || cfg.EnrollmentRecovery != nil ||
		!hmac.Equal(plan.Authority, logoutAuthority(cfg)) || plan.Announced == nil || plan.Announced.ActiveKeyID != plan.KeyID {
		return false
	}
	trusted, err := SigningTrustBundle(cfg)
	if err != nil || plan.Announced.Validate() != nil {
		return false
	}
	if _, err := plan.Announced.Resolve(plan.KeyID, time.Now().UTC()); err != nil {
		return false
	}
	id, err := rpcIdentityAnnouncementID(op.ProfileId, plan.ControlOrigin, trusted, *plan.Announced)
	return err == nil && id == plan.AnnouncementID
}
