package client

import (
	"context"
	"sync"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type clientRPCProfileSwitch struct {
	OperationID string `json:"operation_id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Activated   bool   `json:"activated"`
}

// Lock must also serialize the agent's automatic tunnel reconciliation. Stop
// must be idempotent and return only after old routes are removed. Start must
// enforce current platform/policy restrictions and verify the target map.
type ClientRPCProfileDriver struct {
	Lock  sync.Locker
	Stop  func(context.Context) (ipc.ConnectionContinuity, error)
	Start func(context.Context, Config) error
}

func (m *ClientRPCMutations) selectProfileAs(peer local.Peer, request *ipc.SelectProfileRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/SelectProfile", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if cfg.RPCState.ProfileSwitch != nil {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
		}
		// Existing enrollment must be assigned its initial v0 profile by startup
		// adoption before switching; never silently discard unprojected state.
		if cfg.RPCState.ActiveProfileID == "" && rpcConfigHasEnrollment(*cfg) {
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
		op.ProfileId = profile.ID
		cfg.RPCState.ProfileSwitch = &clientRPCProfileSwitch{OperationID: op.Id, From: cfg.RPCState.ActiveProfileID, To: profile.ID}
		return nil
	})
	return op, err
}

func profileConfiguration(cfg Config) Config {
	cfg = clonePersistentConfig(cfg)
	cfg.RPCState = nil
	cfg.LocalOwnerID = ""
	// These identify the installation, not a selected account/network.
	cfg.PrivateKey = ""
	cfg.IdentityPrivateKey = ""
	cfg.DeviceFingerprint = ""
	return cfg
}

// ReconcileProfileSwitch resumes the durable plan after response loss/restart.
// Use a runtime lifecycle context, not the accepting request's context.
func (m *ClientRPCMutations) ReconcileProfileSwitch(ctx context.Context, driver ClientRPCProfileDriver) error {
	if driver.Lock == nil || driver.Stop == nil || driver.Start == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	m.profileWorker.Lock()
	defer m.profileWorker.Unlock()
	driver.Lock.Lock()
	defer driver.Lock.Unlock()
	cfg := m.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.ProfileSwitch == nil {
		return nil
	}
	plan := *cfg.RPCState.ProfileSwitch
	var current *ipc.Operation
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		if op.Id == plan.OperationID {
			current = op
			break
		}
	}
	if current == nil {
		return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	resuming := current.State == ipc.OperationState_OPERATION_STATE_RUNNING
	if current.State == ipc.OperationState_OPERATION_STATE_PENDING {
		var err error
		current, err = m.ReconcileOperation(current.Id, func(_ *Config, op *ipc.Operation) error {
			op.State = ipc.OperationState_OPERATION_STATE_RUNNING
			return nil
		})
		if err != nil {
			return err
		}
	}
	if current.State != ipc.OperationState_OPERATION_STATE_RUNNING {
		return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if plan.From == plan.To && !plan.Activated {
		_, err := m.ReconcileOperation(current.Id, func(cfg *Config, op *ipc.Operation) error {
			cfg.RPCState.ProfileSwitch = nil
			op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED
			op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: plan.To}}
			return nil
		})
		return err
	}
	continuity, err := driver.Stop(ctx)
	if err != nil {
		return m.failProfileSwitch(current.Id, err, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, false)
	}
	if continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE {
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	if plan.Activated {
		continuity = current.Continuity
	} else if resuming && continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
		// A previous process may have stopped the old tunnel before persisting
		// activation. A currently absent tunnel cannot prove no interruption.
		continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
	}
	if !plan.Activated {
		_, err = m.ReconcileOperation(current.Id, func(cfg *Config, op *ipc.Operation) error {
			state := cfg.RPCState
			if state.ProfileSwitch == nil || state.ProfileSwitch.OperationID != current.Id || state.ActiveProfileID != plan.From {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			target, exists := state.Profiles[plan.To]
			if !exists {
				return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if plan.From != "" {
				old := state.Profiles[plan.From]
				old.Configuration = profileConfiguration(*cfg)
				state.Profiles[plan.From] = old
			}
			next := clonePersistentConfig(target.Configuration)
			next.RPCState = state
			next.LocalOwnerID = cfg.LocalOwnerID
			next.PrivateKey = cfg.PrivateKey
			next.IdentityPrivateKey = cfg.IdentityPrivateKey
			next.DeviceFingerprint = cfg.DeviceFingerprint
			next.ControlPlaneURLs = []string{target.ControlOrigin}
			*cfg = next
			state.ActiveProfileID = plan.To
			state.ProfileSwitch.Activated = true
			op.Continuity = continuity
			return nil
		})
		if err != nil {
			return err
		}
	}
	cfg = m.store.Read()
	if cfg.ConnectionIntent != nil && cfg.ConnectionIntent.DesiredState == ConnectionIntentDesiredConnected {
		if err := driver.Start(ctx, cfg); err != nil {
			// A partial apply must be torn down before reporting a safe state.
			_, stopErr := driver.Stop(ctx)
			if stopErr != nil {
				return m.failProfileSwitch(current.Id, stopErr, ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, false)
			}
			return m.failProfileSwitch(current.Id, err, continuity, true)
		}
	}
	_, err = m.ReconcileOperation(current.Id, func(cfg *Config, op *ipc.Operation) error {
		cfg.RPCState.ProfileSwitch = nil
		op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
		op.Continuity = continuity
		op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: plan.To}}
		return nil
	})
	return err
}

func (m *ClientRPCMutations) failProfileSwitch(id string, cause error, continuity ipc.ConnectionContinuity, stopped bool) error {
	failure := rpc.FailureFromError(cause)
	if failure == nil {
		failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_APPLY_FAILED, ReasonKey: "profile_switch_failed"}
	}
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if stopped || (cfg.RPCState.ProfileSwitch != nil && cfg.RPCState.ProfileSwitch.Activated) {
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "profile_switch_failed", UpdatedAt: m.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
		}
		cfg.RPCState.ProfileSwitch = nil
		op.State = ipc.OperationState_OPERATION_STATE_FAILED
		op.Continuity = continuity
		op.Outcome = &ipc.Operation_Failure{Failure: failure}
		return nil
	})
	return err
}
