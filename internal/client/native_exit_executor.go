package client

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

type nativeExitExecutor struct {
	engine      *WireGuardEngine
	createGuard func(string, string) (*linuxExitGuard, error)
}

// This factory supplies effects and evidence, not host readiness. Its callbacks
// require the caller to hold the same effect lock as map/connection application.
func newNativeExitExecutor(engine *WireGuardEngine, lock *sync.Mutex) (clientRPCExitExecutor, error) {
	if runtime.GOOS != "linux" {
		return clientRPCExitExecutor{}, errors.New("native exit executor requires Linux")
	}
	return newNativeExitExecutorWithGuard(engine, lock, newPlatformExitGuard)
}

func newNativeExitExecutorWithGuard(engine *WireGuardEngine, lock *sync.Mutex, create func(string, string) (*linuxExitGuard, error)) (clientRPCExitExecutor, error) {
	if engine == nil || lock == nil || create == nil || !safeWireGuardInterfaceName(engine.opts.Interface) || engine.opts.Interface == "lo" || strings.TrimSpace(engine.opts.Interface) != engine.opts.Interface {
		return clientRPCExitExecutor{}, errors.New("native exit executor requires owned runtime scope")
	}
	n := &nativeExitExecutor{engine: engine, createGuard: create}
	return clientRPCExitExecutor{InterfaceName: engine.opts.Interface, Lock: lock,
		Modes: []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}, {Family: api.ExitFamilyIPv6Only, LAN: api.ExitLANBlock}, {Family: api.ExitFamilyDualStack, LAN: api.ExitLANBlock}},
		Apply: n.apply, Contain: n.contain, Release: n.release, Observe: n.observe}, nil
}

func nativeExitOperation(cfg Config, id string, requested *ClientExitSelection, releasing bool) (*clientRPCExitChange, error) {
	invalid := errors.New("native exit operation is not durably bound")
	if cfg.RPCState == nil || cfg.RPCState.ExitChange == nil || id == "" {
		return nil, invalid
	}
	plan := cfg.RPCState.ExitChange
	if plan.OperationID != id || plan.Containing || plan.Releasing != releasing || !reflect.DeepEqual(plan.Requested, requested) || plan.Protection == nil || !reflect.DeepEqual(plan.Protection, cfg.RPCState.ExitProtection) {
		return nil, invalid
	}
	found := false
	for _, record := range cfg.RPCState.Operations {
		op := new(ipc.Operation)
		if proto.Unmarshal(record.Operation, op) != nil {
			return nil, invalid
		}
		if op.Id != id {
			continue
		}
		if found || record.CompletedAt != nil || op.State != ipc.OperationState_OPERATION_STATE_RUNNING || !exitChangeBound(&cfg, plan, op) || !strings.EqualFold(record.Owner, plan.OwnerID) {
			return nil, invalid
		}
		found = true
	}
	if !found {
		return nil, invalid
	}
	return plan, nil
}

func (n *nativeExitExecutor) guard(scope *clientRPCExitProtection) (*linuxExitGuard, error) {
	if scope == nil || scope.OperationID == "" || scope.ProfileID == "" || scope.OwnerID == "" || scope.NodeID == "" || scope.NetworkID == "" || scope.InterfaceName != n.engine.opts.Interface {
		return nil, errors.New("native exit protection has no ownership")
	}
	normalized, err := NormalizeWireGuardRouteTable(scope.RouteTable)
	if err != nil || normalized != scope.RouteTable || normalized == "off" {
		return nil, errors.New("native exit protection has invalid route scope")
	}
	expected, err := n.createGuard(scope.InterfaceName, scope.RouteTable)
	if err != nil {
		return nil, err
	}
	if expected == nil || expected.interfaceName != scope.InterfaceName || !validExitPolicyTable(expected.mark) {
		return nil, errors.New("native exit guard differs from durable scope")
	}
	n.engine.mu.Lock()
	defer n.engine.mu.Unlock()
	if guard := n.engine.exitGuard; guard != nil {
		if guard.interfaceName != expected.interfaceName || guard.mark != expected.mark || guard.table != expected.table {
			return nil, errors.New("native exit runtime owns another protection scope")
		}
		return guard, nil
	}
	return expected, nil
}

// A cancelled operation may already have opened or released the guard. Keep
// the original owned scope recoverable and close it using a bounded context.
func (n *nativeExitExecutor) containFailure(ctx context.Context, guard *linuxExitGuard) error {
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exitGuard != nil && e.exitGuard != guard {
		return errors.New("cannot contain a replacement native scope")
	}
	e.exitGuard = guard
	if e.exitFilter == nil {
		e.exitFilter = &exitPacketFilter{}
	}
	e.exitFilter.withdraw()
	return guard.Contain(recovery)
}

func (n *nativeExitExecutor) stopOwned(ctx context.Context, scope *clientRPCExitProtection, guard *linuxExitGuard) error {
	e := n.engine
	e.mu.Lock()
	live := e.device != nil || e.router != nil || e.tun != nil || e.configured
	if live && (e.exitGuard != guard || e.exitConfig.NodeID != scope.NodeID || e.exitConfig.NetworkID != scope.NetworkID || !strings.EqualFold(e.exitConfig.LocalOwnerID, scope.OwnerID) || e.exitConfig.RPCState == nil || e.exitConfig.RPCState.ActiveProfileID != scope.ProfileID) {
		e.mu.Unlock()
		return errors.New("native cleanup cannot stop another runtime identity")
	}
	if e.exitGuard == nil {
		e.exitGuard = guard
		if e.exitFilter == nil {
			e.exitFilter = &exitPacketFilter{}
		}
		e.exitFilter.withdraw()
	}
	e.mu.Unlock()
	result, err := e.Down(ctx)
	if err != nil {
		return err
	}
	if !result.OK {
		return errors.New("native runtime stop was not confirmed")
	}
	if err := e.recoverStoppedExit(ctx, guard); err != nil {
		return err
	}
	return n.observeStopped(ctx, guard)
}

func (n *nativeExitExecutor) observeStopped(ctx context.Context, guard *linuxExitGuard) error {
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exitGuard != guard || e.device != nil || e.router != nil || e.tun != nil || e.configured || e.exitFilter == nil {
		return errors.New("native exit runtime cleanup is not observed")
	}
	e.exitFilter.mu.RLock()
	closed := e.exitFilter.closed
	e.exitFilter.mu.RUnlock()
	if !closed {
		return errors.New("native exit packet filter is not contained")
	}
	if err := guard.ObserveContained(ctx); err != nil {
		return err
	}
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return guard.run(ctx, "", name, args...)
	}
	empty, err := exitRouteTablesEmpty(ctx, guard.mark, runner)
	if err != nil {
		return err
	}
	if !empty {
		return errors.New("native exit routes remain")
	}
	absent, err := exitPolicyRulesAbsent(ctx, guard.mark, runner)
	if err != nil {
		return err
	}
	if !absent {
		return errors.New("native exit rules remain")
	}
	return ctx.Err()
}

func (n *nativeExitExecutor) apply(ctx context.Context, id string, cfg Config, selection *ClientExitSelection) (status *ipc.ExitNodeStatus, continuity ipc.ConnectionContinuity, err error) {
	continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
	plan, err := nativeExitOperation(cfg, id, selection, false)
	if err != nil {
		return nil, continuity, err
	}
	if err = ctx.Err(); err != nil {
		return nil, continuity, err
	}
	guard, err := n.guard(plan.Protection)
	if err != nil {
		return nil, continuity, err
	}
	if selection == nil {
		// Validate a live identity before installing any new guard in its place.
		err = n.stopOwned(ctx, plan.Protection, guard)
		if err != nil {
			return nil, continuity, err
		}
		return nativeExitClearStatus(plan.ProfileID, true), continuity, nil
	}
	validFamily := selection.Family == api.ExitFamilyIPv4Only || selection.Family == api.ExitFamilyIPv6Only || selection.Family == api.ExitFamilyDualStack
	if !validFamily || selection.LAN != api.ExitLANBlock || cfg.CachedMap == nil || selection.NodeID != plan.Protection.NodeID || selection.NetworkID != plan.Protection.NetworkID || selection.RouteTable != plan.Protection.RouteTable || plan.Protection.InterfaceName != n.engine.opts.Interface {
		return nil, continuity, errors.New("native exit selection is unsupported or unbound")
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, n.containFailure(ctx, guard))
			status = nil
		}
	}()
	result, err := n.engine.configureExit(ctx, cfg, *cfg.CachedMap, selection, guard)
	if err != nil {
		return nil, continuity, err
	}
	if !result.OK {
		return nil, continuity, errors.New("native exit application was not confirmed")
	}
	status, err = n.observeSelection(ctx, cfg, selection, plan.ProfileID, guard)
	return status, continuity, err
}

func (n *nativeExitExecutor) release(ctx context.Context, id string, cfg Config) (status *ipc.ExitNodeStatus, continuity ipc.ConnectionContinuity, err error) {
	continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
	plan, err := nativeExitOperation(cfg, id, nil, true)
	if err != nil {
		return nil, continuity, err
	}
	if err = ctx.Err(); err != nil {
		return nil, continuity, err
	}
	guard, err := n.guard(plan.Protection)
	if err != nil {
		return nil, continuity, err
	}
	if err = n.stopOwned(ctx, plan.Protection, guard); err != nil {
		return nil, continuity, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, n.containFailure(ctx, guard))
			status = nil
		}
	}()
	if err = n.engine.releaseClearedExit(ctx, guard); err != nil {
		return nil, continuity, err
	}
	if err = guard.ObserveAbsent(ctx); err != nil {
		return nil, continuity, err
	}
	if err = ctx.Err(); err != nil {
		return nil, continuity, err
	}
	return nativeExitClearStatus(plan.ProfileID, false), continuity, nil
}

func (n *nativeExitExecutor) contain(ctx context.Context, plan clientRPCExitChange) (clientRPCExitContainment, error) {
	if !plan.Containing || plan.OperationID == "" || plan.Protection == nil || plan.ProfileID != plan.Protection.ProfileID || !strings.EqualFold(plan.OwnerID, plan.Protection.OwnerID) {
		return clientRPCExitContainment{}, errors.New("native containment is not bound")
	}
	if err := ctx.Err(); err != nil {
		return clientRPCExitContainment{}, err
	}
	guard, err := n.guard(plan.Protection)
	if err != nil {
		return clientRPCExitContainment{}, err
	}
	if err = n.stopOwned(ctx, plan.Protection, guard); err != nil {
		return clientRPCExitContainment{}, err
	}
	return clientRPCExitContainment{OperationID: plan.OperationID, ProfileID: plan.ProfileID, NodeID: plan.NodeID, NetworkID: plan.NetworkID, IPv4Blocked: true, IPv6Blocked: true, ExitRoutesRemoved: true}, nil
}

func (n *nativeExitExecutor) observeSelection(ctx context.Context, cfg Config, selection *ClientExitSelection, profile string, guard *linuxExitGuard) (*ipc.ExitNodeStatus, error) {
	e := n.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.exitConfig.RPCState == nil || e.exitConfig.RPCState.ActiveProfileID != profile {
		return nil, errors.New("native exit runtime profile changed")
	}
	if cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || e.pathMap.MapSignature == nil || !e.configured || e.device == nil || e.router == nil || e.tun == nil || e.exitGuard != guard || e.interface_ != guard.interfaceName || !reflect.DeepEqual(e.exitSelection, selection) || !sameExitControlIdentity(cfg, e.exitConfig) || e.pathMap.MapSignature.PayloadHash != cfg.CachedMap.MapSignature.PayloadHash || e.exitFilter == nil {
		return nil, errors.New("native exit runtime observation differs from requested context")
	}
	if err := confirmExitDefaultRoutes(ctx, guard, selection.Family); err != nil {
		return nil, err
	}
	if err := guard.Observe(ctx, selection.Family); err != nil {
		return nil, err
	}
	if err := nativeExitUAPIObserved(e, selection); err != nil {
		return nil, err
	}
	if underlayDNSRequired(cfg, cfg.CachedMap) {
		current := e.underlayDNSCurrentLocked()
		if current == nil || e.underlayDNS == nil || e.underlayLease == nil {
			return nil, errors.New("native exit underlay source is missing")
		}
		if err := current(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("native exit underlay source is no longer current")
		}
	}
	want, err := compileExitPacketPolicy(cfg, *cfg.CachedMap, selection, time.Now())
	if err != nil {
		return nil, err
	}
	e.exitFilter.mu.RLock()
	matches := !e.exitFilter.closed && !e.exitFilter.applying && e.exitFilter.pending == nil && reflect.DeepEqual(e.exitFilter.current, want)
	e.exitFilter.mu.RUnlock()
	if !matches {
		return nil, errors.New("native exit packet policy is not applied")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nativeExitSelectedStatus(profile, selection), nil
}

func (n *nativeExitExecutor) observe(ctx context.Context, cfg Config) (*ipc.ExitNodeStatus, error) {
	if cfg.RPCState == nil || cfg.RPCState.ExitChange != nil || cfg.ExitSelection == nil || cfg.RPCState.ExitProtection == nil {
		return nil, errors.New("native exit has no settled observed selection")
	}
	scope := cfg.RPCState.ExitProtection
	if scope.ProfileID != cfg.RPCState.ActiveProfileID || scope.NodeID != cfg.NodeID || scope.NetworkID != cfg.NetworkID || scope.RouteTable != cfg.WireGuardRouteTable || scope.RouteTable != cfg.ExitSelection.RouteTable || !strings.EqualFold(scope.OwnerID, cfg.LocalOwnerID) {
		return nil, errors.New("native exit observation scope changed")
	}
	guard, err := n.guard(scope)
	if err != nil {
		return nil, err
	}
	return n.observeSelection(ctx, cfg, cfg.ExitSelection, scope.ProfileID, guard)
}

func nativeExitClearStatus(profile string, contained bool) *ipc.ExitNodeStatus {
	return &ipc.ExitNodeStatus{ProfileId: profile, RequestedFamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_NONE, ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: contained,
		Ipv4: &ipc.ExitFamilyStatus{ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: contained}, Ipv6: &ipc.ExitFamilyStatus{ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: contained}}
}

func nativeExitSelectedStatus(profile string, selection *ClientExitSelection) *ipc.ExitNodeStatus {
	family := map[api.ExitFamilyMode]ipc.ExitFamilyMode{api.ExitFamilyIPv4Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, api.ExitFamilyIPv6Only: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, api.ExitFamilyDualStack: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[selection.Family]
	status := &ipc.ExitNodeStatus{ProfileId: profile, RequestedExitNodeId: proto.String(selection.ID), EffectiveExitNodeId: proto.String(selection.ID), RequestedFamilyMode: family, RequestedLanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK, EffectiveLanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK, ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: true}
	member := func(enabled bool) *ipc.ExitFamilyStatus {
		value := &ipc.ExitFamilyStatus{ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED}
		if enabled {
			value.RequestedExitNodeId = proto.String(selection.ID)
			value.EffectiveExitNodeId = proto.String(selection.ID)
			value.FailClosed = true
		}
		return value
	}
	status.Ipv4 = member(selection.Family != api.ExitFamilyIPv6Only)
	status.Ipv6 = member(selection.Family != api.ExitFamilyIPv4Only)
	return status
}
