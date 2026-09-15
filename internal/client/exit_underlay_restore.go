package client

import (
	"context"
	"errors"
	"reflect"
	"strings"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// restoreExitUnderlay is the native adapter's pre-network recovery step for an
// existing durable selection. It restores containment and control authority,
// not exit routes or an applied selection. Call under the shared effect lock.
func (e *WireGuardEngine) restoreExitUnderlay(ctx context.Context, cfg Config, guard *linuxExitGuard) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard == nil || guard.mark == 0 || (e.exitGuard != nil && e.exitGuard != guard) || e.device != nil || e.router != nil || e.tun != nil || e.configured {
		return errors.New("exit underlay recovery requires an owned guard and stopped runtime")
	}
	selection, err := exitUnderlayRecoverySelection(cfg)
	if err != nil {
		return err
	}
	if selection == nil || selection.ID == "" || cfg.NodeID == "" || cfg.NodeCredential == "" || cfg.NetworkID == "" || selection.NodeID != cfg.NodeID || selection.NetworkID != cfg.NetworkID {
		return errors.New("exit underlay recovery requires a bound durable selection")
	}
	if err := ValidateConfigCurrentDevice(cfg); err != nil {
		return err
	}
	// Validate the entire origin set before taking ownership; construction opens
	// no sockets. The guard supplies the mark even with no router configuration.
	control, err := newControlUnderlayHTTPClient(cfg.ControlURLs(), guard.mark, nil)
	if err != nil {
		return err
	}
	control.CloseIdleConnections()
	if e.exitGuard != nil && e.exitConfig.NodeID != "" && !sameExitControlIdentity(cfg, e.exitConfig) {
		return errors.New("exit underlay recovery cannot replace an owned identity")
	}
	if e.exitRestoreConfig.NodeID != "" && !sameExitControlIdentity(cfg, e.exitRestoreConfig) {
		return errors.New("exit underlay recovery cannot replace its pending identity")
	}
	e.exitGuard = guard
	e.exitRestoreConfig = clonePersistentConfig(cfg)
	e.exitSelection = nil
	e.exitConfig = Config{}
	if e.exitFilter == nil {
		e.exitFilter = &exitPacketFilter{}
	}
	e.exitFilter.withdraw()
	if err := guard.Contain(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.exitConfig = clonePersistentConfig(cfg)
	e.exitRestoreConfig = Config{}
	return nil
}

// A first selection may have touched the OS before ExitSelection committed.
// Recover control authority from its dispatched journal, without treating the
// requested selection or an expired/missing cached map as an applied grant.
func exitUnderlayRecoverySelection(cfg Config) (*ClientExitSelection, error) {
	if cfg.ExitSelection != nil {
		return cfg.ExitSelection, nil
	}
	invalid := errors.New("exit underlay recovery requires a bound dispatched operation")
	if cfg.RPCState == nil || cfg.RPCState.ExitChange == nil {
		return nil, invalid
	}
	plan := cfg.RPCState.ExitChange
	profile, exists := cfg.RPCState.Profiles[plan.ProfileID]
	if !exists || profile.ID != plan.ProfileID || plan.OperationID == "" || plan.ProfileID != cfg.RPCState.ActiveProfileID || plan.ControlOrigin != profile.ControlOrigin || plan.OwnerID == "" || !strings.EqualFold(plan.OwnerID, cfg.LocalOwnerID) || plan.NodeID != cfg.NodeID || plan.NetworkID != cfg.NetworkID || plan.Previous != nil || plan.Requested == nil || !reflect.DeepEqual(plan.PreviousIntent, cfg.ConnectionIntent) {
		return nil, invalid
	}
	origin, err := rpcProfileOrigin(plan.ControlOrigin)
	if err != nil || len(cfg.ControlURLs()) == 0 {
		return nil, invalid
	}
	primary, err := rpcProfileOrigin(cfg.ControlURLs()[0])
	if err != nil || primary != origin {
		return nil, invalid
	}
	var found *ipc.Operation
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return nil, invalid
		}
		if op.Id != plan.OperationID {
			continue
		}
		if found != nil || record.CompletedAt != nil || !strings.EqualFold(record.Owner, plan.OwnerID) || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || op.Kind != ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE || op.ProfileId != plan.ProfileID {
			return nil, invalid
		}
		found = op
	}
	if found == nil {
		return nil, invalid
	}
	return plan.Requested, nil
}
