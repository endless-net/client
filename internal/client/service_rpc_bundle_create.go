package client

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Owner is the authenticated requester, not necessarily the installation owner.
// The admitted role is internal durable command authority, never caller input;
// the installation-owner snapshot separately revokes work on ownership changes.
type clientRPCBundlePlan struct {
	Owner             string `json:"owner"`
	InstallationOwner string `json:"installation_owner"`
	Administrator     bool   `json:"administrator"`
	ProfileID         string `json:"profile_id"`
	AcceptedRevision  uint64 `json:"accepted_revision"`
}

func (m *ClientRPCMutations) createBundleAs(peer local.Peer, request *ipc.CreateDiagnosticsBundleRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptAs(peer, "/client.v0.ClientService/CreateDiagnosticsBundle", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.GetProfile())
		if err != nil {
			return err
		}
		if profile.ID != cfg.RPCState.ActiveProfileID {
			return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
		}
		if cfg.RPCState.Bundles == nil {
			cfg.RPCState.Bundles = map[string]clientRPCBundlePlan{}
		}
		op.ProfileId = profile.ID
		cfg.RPCState.Bundles[op.Id] = clientRPCBundlePlan{Owner: peer.Identity, InstallationOwner: cfg.LocalOwnerID, Administrator: peer.Administrator, ProfileID: profile.ID, AcceptedRevision: cfg.RPCState.Revision + 1}
		return nil
	})
	return op, err
}

func bundlePlanAllowed(cfg Config, plan clientRPCBundlePlan) bool {
	if cfg.RPCState == nil || !strings.EqualFold(cfg.LocalOwnerID, plan.InstallationOwner) || plan.ProfileID != cfg.RPCState.ActiveProfileID {
		return false
	}
	if _, exists := cfg.RPCState.Profiles[plan.ProfileID]; !exists {
		return false
	}
	for _, record := range cfg.RPCState.Operations {
		op := &ipc.Operation{}
		if proto.Unmarshal(record.Operation, op) != nil {
			return false
		}
		if op.ProfileId != plan.ProfileID || op.GetMetadata().GetRevision() <= plan.AcceptedRevision {
			continue
		}
		switch op.Kind {
		case ipc.OperationKind_OPERATION_KIND_LOGOUT, ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT, ipc.OperationKind_OPERATION_KIND_REMOVE_PROFILE:
			return false
		}
	}
	return true
}

// Executes durable plans serially. The operation UUID is also the artifact UUID:
// a restart between artifact persistence and result publication reuses the same
// immutable artifact instead of collecting again. Request cancellation is absent.
func (s *ClientRPCService) reconcileBundles(ctx context.Context) error {
	cfg := s.mutations.store.Read()
	if cfg.RPCState == nil {
		return nil
	}
	ids := make([]string, 0, len(cfg.RPCState.Bundles))
	for id := range cfg.RPCState.Bundles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		plan := cfg.RPCState.Bundles[id]
		if err := s.executeBundle(ctx, id, plan); err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientRPCService) executeBundle(ctx context.Context, id string, plan clientRPCBundlePlan) error {
	m := s.mutations
	if _, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if !bundlePlanAllowed(*cfg, plan) {
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "bundle_scope_changed"}}
		} else {
			op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		}
		return nil
	}); err != nil {
		return err
	}
	baseline := clonePersistentConfig(m.store.Read())
	if _, exists := baseline.RPCState.Bundles[id]; !exists {
		return nil
	}
	s.bundleStore.mu.Lock()
	s.bundleStore.pruneLocked()
	item, exists := s.bundleStore.items[id]
	var metadata *ipc.BundleResult
	if exists && strings.EqualFold(item.owner, plan.Owner) && strings.EqualFold(item.installationOwner, plan.InstallationOwner) && item.profile == plan.ProfileID {
		metadata = proto.Clone(item.metadata).(*ipc.BundleResult)
	}
	s.bundleStore.mu.Unlock()
	var failure *ipc.Failure
	if exists && metadata == nil {
		failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "bundle_scope_changed"}
	}
	collected := false
	if !exists {
		observation, err := s.diagnosticsAs(ctx, local.Peer{Identity: plan.Owner, Administrator: plan.Administrator}, &ipc.GetDiagnosticsRequest{Profile: &ipc.ProfileRef{ProfileId: plan.ProfileID}})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failure = rpc.FailureFromError(runtimeRPCFailure(err))
		} else {
			data, err := buildNativeDiagnosticsArchive(observation.Diagnostics)
			if err != nil {
				failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED, ReasonKey: "bundle_archive_unavailable"}
			} else {
				metadata, err = s.bundleStore.putID(id, plan.Owner, plan.InstallationOwner, plan.ProfileID, data)
				if err != nil {
					if connect.CodeOf(err) != connect.CodeResourceExhausted {
						return err
					}
					failure = rpc.FailureFromError(err)
				} // Storage failure leaves a recoverable plan.
				collected = true
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := m.ReconcileOperation(id, func(cfg *Config, op *ipc.Operation) error {
		if metadata != nil && !s.bundleStore.timeNow().Before(metadata.ExpiresAt.AsTime()) {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_NOT_FOUND, ReasonKey: "bundle_expired_before_publication"}
		}
		if !bundlePlanAllowed(*cfg, plan) {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "bundle_scope_changed"}
		} else if collected && !reflect.DeepEqual(*cfg, baseline) {
			failure = &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_STALE_STATE, ReasonKey: "bundle_snapshot_changed"}
		}
		if failure != nil {
			op.State = ipc.OperationState_OPERATION_STATE_FAILED
			op.Outcome = &ipc.Operation_Failure{Failure: failure}
		} else {
			op.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			op.Outcome = &ipc.Operation_Bundle{Bundle: metadata}
		}
		return nil
	})
	return err
}
