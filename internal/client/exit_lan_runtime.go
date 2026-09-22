package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const exitLANPacketHook uint32 = 4
const exitLANPacketPriority int32 = 2147483646

var errExitLANPeerHealth = errors.New("selected LAN exit peer is not yet confirmed")

type exitLANRuntimeOps struct {
	capture     func(context.Context, context.Context, string, api.ExitFamilyMode) (*exitLANSource, error)
	session     func(context.Context, *linuxExitGuard, *exitLANPlan, uint32, string, func(context.Context, *exitLANOwnership) error) (*exitLANBPFSession, error)
	clock       func(context.Context) (exitLANClockSample, error)
	observeHook func(context.Context, exitLANBPFLinkIdentity) error
	inspect     func(*WireGuardEngine) (WireGuardInspection, error)
	cleanup     func(context.Context, *linuxExitGuard, *exitLANOwnership) error
}

type exitLANRuntime struct {
	store        *ConfigStore
	ops          exitLANRuntimeOps
	session      *exitLANBPFSession
	plan         *exitLANPlan
	routing      *exitLANRoutingOwnership
	cancel       context.CancelFunc
	anchor       exitLANClockSample
	deadline     *exitLANBootDeadline
	awaitingPeer bool
}

// Transient operation checkpoints do not change packet authority. Keep the
// complete connection/map/preferences configuration plus active profile identity,
// but exclude journals which necessarily change at Apply's durable completion.
func exitLANHealthConfig(cfg Config, selection *ClientExitSelection) [32]byte {
	cfg = clonePersistentConfig(cfg)
	cfg.ExitSelection = cloneExitSelection(selection)
	if cfg.RPCState != nil {
		cfg.RPCState = &ClientRPCState{ActiveProfileID: cfg.RPCState.ActiveProfileID}
	}
	return resourceObservationConfig(cfg)
}

func exitLANRuntimeConfigMatches(stored, candidate Config, selection *ClientExitSelection, g *linuxExitGuard) bool {
	if _, err := nativeExitMaintenanceProfile(stored, selection, g); err != nil {
		return false
	}
	if exitLANHealthConfig(stored, selection) == exitLANHealthConfig(candidate, selection) {
		return true
	}
	if stored.RPCState != nil && stored.RPCState.NetworkPreferenceChange != nil {
		projected, err := nativeExitPreferenceCandidate(stored, g.interfaceName)
		return err == nil && exitLANHealthConfig(projected, selection) == exitLANHealthConfig(candidate, selection)
	}
	return false
}

// Caller holds engine.mu and the shared effect lock. Containment must already
// be installed, including for map/preference reapplication and saved resume.
func (r *exitLANRuntime) cleanup(ctx context.Context, e *WireGuardEngine, g *linuxExitGuard, cfg Config) error {
	if r == nil {
		return nil
	}
	if err := r.stop(); err != nil {
		return err
	}
	if cfg.RPCState == nil || cfg.RPCState.ExitProtection == nil {
		return nil
	}
	current := r.store.Read()
	if current.RPCState == nil || !reflect.DeepEqual(exitProtectionWithoutLAN(cfg.RPCState.ExitProtection), exitProtectionWithoutLAN(current.RPCState.ExitProtection)) {
		return errExitLANPolicy
	}
	n := &nativeExitExecutor{engine: e, store: r.store, cleanupLANObjects: r.ops.cleanup}
	return n.cleanupLAN(ctx, current.RPCState.ExitProtection, g)
}

func (r *exitLANRuntime) stop() error {
	if r == nil {
		return nil
	}
	var err error
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.plan != nil {
		err = errors.Join(err, r.plan.topology.close())
		r.plan = nil
	}
	if r.session != nil {
		err = errors.Join(err, r.session.Close())
		r.session = nil
	}
	r.deadline = nil
	r.routing = nil
	return err
}

func exitProtectionWithoutLAN(scope *clientRPCExitProtection) *clientRPCExitProtection {
	scope = cloneExitProtection(scope)
	if scope != nil {
		scope.LAN = nil
	}
	return scope
}

func exitConfigWithoutLANJournal(cfg Config) Config {
	cfg = clonePersistentConfig(cfg)
	if cfg.RPCState != nil {
		cfg.RPCState.ExitProtection = exitProtectionWithoutLAN(cfg.RPCState.ExitProtection)
		if cfg.RPCState.ExitChange != nil {
			cfg.RPCState.ExitChange.Protection = exitProtectionWithoutLAN(cfg.RPCState.ExitChange.Protection)
		}
	}
	return cfg
}

func (r *exitLANRuntime) apply(ctx context.Context, e *WireGuardEngine, g *linuxExitGuard, cfg Config, selection *ClientExitSelection) (result error) {
	stage := "context binding"
	defer func() {
		if result != nil {
			result = fmt.Errorf("native LAN %s: %w", stage, result)
		}
	}()
	if r == nil || r.store == nil || r.ops.capture == nil || r.ops.session == nil || r.ops.clock == nil || r.ops.observeHook == nil || selection == nil || selection.LAN != api.ExitLANAllow {
		return errExitLANPolicy
	}
	if r.session != nil || r.plan != nil {
		return errExitLANPolicy
	}
	r.awaitingPeer = false
	cfg.ExitSelection = cloneExitSelection(selection)
	current := r.store.Read()
	if current.RPCState == nil || current.RPCState.ExitProtection == nil || current.RPCState.ExitProtection.LAN != nil || !exitLANRuntimeConfigMatches(current, cfg, selection, g) {
		return errExitLANPolicy
	}
	scope := cloneExitProtection(current.RPCState.ExitProtection)
	if scope.InterfaceName != g.interfaceName {
		return errExitLANPolicy
	}
	anchor, err := r.ops.clock(ctx)
	if err != nil {
		return err
	}
	if !r.anchor.valid() {
		r.anchor = anchor
	} else if anchor.bootBefore < r.anchor.bootAfter || anchor.wall.Before(r.anchor.wall) {
		return errExitLANClock
	}
	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	var source *exitLANSource
	defer func() {
		if result != nil {
			recovery, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			result = errors.Join(result, g.Contain(recovery))
			finish()
			cancel()
			if source != nil {
				_ = source.close()
			}
			if r.session != nil {
				result = errors.Join(result, r.session.Close())
				r.session = nil
			}
			r.plan = nil
			r.deadline = nil
		}
	}()
	source, err = r.ops.capture(ctx, lifetime, g.interfaceName, selection.Family)
	if err != nil {
		return err
	}
	stage = "retained destinations"
	retained, err := e.retainedExitLANDestinationsLocked()
	if err != nil {
		return err
	}
	stage = "destination plan"
	plan, err := compileExitLANPlan(cfg, *cfg.CachedMap, selection, source, retained, anchor.wall)
	if err != nil {
		return err
	}
	routing, err := exitLANRoutingPlan(g, plan)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(scope.OperationID + "\x00" + scope.ProfileID + "\x00" + scope.InterfaceName))
	pinScope := hex.EncodeToString(digest[:12])
	stage = "closed BPF preparation"
	r.session, err = r.ops.session(ctx, g, plan, routing.Mark, pinScope, func(ctx context.Context, owned *exitLANOwnership) error {
		owned.Routing = routing
		return r.store.Update(func(stored *Config) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if stored.RPCState == nil || !reflect.DeepEqual(stored.RPCState.ExitProtection, scope) || !exitLANRuntimeConfigMatches(*stored, cfg, selection, g) {
				return errExitLANPolicy
			}
			if change := stored.RPCState.ExitChange; change != nil && !reflect.DeepEqual(change.Protection, scope) {
				return errExitLANPolicy
			}
			stored.RPCState.ExitProtection.LAN = cloneExitLANOwnership(owned)
			if stored.RPCState.ExitChange != nil {
				stored.RPCState.ExitChange.Protection.LAN = cloneExitLANOwnership(owned)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	stage = "routing"
	if err := applyExitLANRouting(ctx, g, routing); err != nil {
		return err
	}
	// Installing our routes invalidates the first watch. Collect anew while
	// BLOCK is held; the original exact device instances/bindings must agree.
	stage = "topology recheck"
	if err := source.close(); err != nil {
		return err
	}
	old := plan
	source, err = r.ops.capture(ctx, lifetime, g.interfaceName, selection.Family)
	if err != nil {
		return err
	}
	plan, err = compileExitLANPlan(cfg, *cfg.CachedMap, selection, source, retained, time.Now())
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(old.bindings, plan.bindings) || !reflect.DeepEqual(old.topology.Links, plan.topology.Links) {
		return errExitLANPolicy
	}
	plan.expires = earliestExitLANDeadline(plan.expires, old.expires)
	r.plan, r.routing = plan, routing
	stage = "health lease"
	if err := r.refresh(ctx, e, g, cfg, selection); err != nil {
		r.awaitingPeer = errors.Is(err, errExitLANPeerHealth)
		return err
	}
	if !exitLANRuntimeConfigMatches(r.store.Read(), cfg, selection, g) {
		return errExitLANPolicy
	}
	stage = "firewall"
	if err := g.openLAN(ctx, selection.Family, &exitLANFirewallState{plan: plan, routing: routing}); err != nil {
		return err
	}
	if !exitLANRuntimeConfigMatches(r.store.Read(), cfg, selection, g) {
		return errExitLANPolicy
	}
	stage = "final observation"
	return r.observe(ctx, e, g, cfg, selection)
}

func (r *exitLANRuntime) refresh(ctx context.Context, e *WireGuardEngine, g *linuxExitGuard, cfg Config, selection *ClientExitSelection) error {
	if r == nil || r.plan == nil || r.session == nil || !r.plan.topologyCurrent(time.Now()) || !r.journalCurrent() {
		return errExitLANPolicy
	}
	cfg.ExitSelection = cloneExitSelection(selection)
	if err := observeExitLANRouting(ctx, g, r.routing); err != nil {
		return err
	}
	if r.ops.inspect == nil {
		return errExitLANPolicy
	}
	health, err := e.observeExitLANPeerHealthWithInspection(ctx, cfg, selection, time.Now(), r.ops.inspect)
	if err != nil {
		return errors.Join(errExitLANPeerHealth, err)
	}
	deadline, err := newExitLANBootDeadline(r.anchor, earliestExitLANDeadline(r.plan.expires, health.expires))
	if err != nil {
		return err
	}
	// The retained anchor prevents stale evidence from gaining time after a
	// backward wall-clock adjustment. A current sample also caps fresh evidence
	// after a forward adjustment, including if the process dies immediately.
	sample, err := r.ops.clock(ctx)
	if err != nil {
		return err
	}
	currentCap, err := newExitLANBootDeadline(sample, deadline.wallExpires)
	if err != nil {
		return err
	}
	deadline.bootExpires = min(deadline.bootExpires, currentCap.bootExpires)
	if r.deadline != nil && !deadline.wallExpires.After(r.deadline.wallExpires) {
		deadline.bootExpires = min(deadline.bootExpires, r.deadline.bootExpires)
	}
	s := r.session
	err = s.namespace.withOwnership(ctx, s.ownership, func(ctx context.Context) error {
		if err := s.observePins(ctx); err != nil {
			return err
		}
		if !health.currentWithInspection(ctx, cfg, time.Now(), r.ops.inspect) || !r.plan.topologyCurrent(time.Now()) {
			return errExitLANPolicy
		}
		if err := s.preparation.publishBootDeadline(ctx, deadline, r.ops.clock, selection.Family, exitLANPacketHook, exitLANPacketPriority, r.ops.observeHook); err != nil {
			return err
		}
		if err := s.observePins(ctx); err != nil {
			return err
		}
		if !health.currentWithInspection(ctx, cfg, time.Now(), r.ops.inspect) || !r.plan.topologyCurrent(time.Now()) {
			return errExitLANPolicy
		}
		return nil
	})
	if err != nil {
		return err
	}
	r.deadline = deadline
	return nil
}

func (r *exitLANRuntime) observe(ctx context.Context, e *WireGuardEngine, g *linuxExitGuard, cfg Config, selection *ClientExitSelection) error {
	if r == nil || r.session == nil || r.plan == nil || r.deadline == nil || !r.plan.topologyCurrent(time.Now()) || !r.journalCurrent() {
		return errExitLANPolicy
	}
	sample, err := r.ops.clock(ctx)
	if err != nil {
		return err
	}
	if !r.deadline.current(sample) {
		return errExitLANClock
	}
	cfg.ExitSelection = cloneExitSelection(selection)
	if r.ops.inspect == nil {
		return errExitLANPolicy
	}
	if _, err := e.observeExitLANPeerHealthWithInspection(ctx, cfg, selection, time.Now(), r.ops.inspect); err != nil {
		return err
	}
	if err := observeExitLANRouting(ctx, g, r.routing); err != nil {
		return err
	}
	s := r.session
	err = s.namespace.withOwnership(ctx, s.ownership, func(ctx context.Context) error {
		if err := s.observePins(ctx); err != nil {
			return err
		}
		if !lockExitRuntime(ctx, &s.preparation.mu) {
			return ctx.Err()
		}
		defer s.preparation.mu.Unlock()
		return s.preparation.observeHeldLinksLocked(ctx, selection.Family, exitLANPacketHook, exitLANPacketPriority, r.ops.observeHook)
	})
	if err != nil {
		return err
	}
	sample, err = r.ops.clock(ctx)
	if err != nil {
		return err
	}
	if !r.deadline.current(sample) || !r.plan.topologyCurrent(time.Now()) || !r.journalCurrent() {
		return errExitLANPolicy
	}
	return ctx.Err()
}

func (r *exitLANRuntime) journalCurrent() bool {
	if r.store == nil || r.session == nil || r.session.ownership == nil || r.routing == nil {
		return false
	}
	cfg := r.store.Read()
	if cfg.RPCState == nil || cfg.RPCState.ExitProtection == nil {
		return false
	}
	want := cloneExitLANOwnership(r.session.ownership)
	want.Routing = r.routing
	return reflect.DeepEqual(cfg.RPCState.ExitProtection.LAN, want)
}
