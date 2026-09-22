package client

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type exitLANRuntimeTest struct {
	t        *testing.T
	e        *WireGuardEngine
	store    *ConfigStore
	n        *nativeExitExecutor
	kernel   *exitLANBPFPinTestKernel
	routes   *exitLANRouteTestKernel
	streams  []*exitLANTestStream
	detached map[uint32]bool
	code     []byte
	opened   int
}

func newExitLANRuntimeTest(t *testing.T) *exitLANRuntimeTest {
	t.Helper()
	e, cfg, _, inspect := exitLANHealthFixture(t, false)
	const profileID = "7a5a66e0-2347-4eec-b573-8bdf599634d1"
	profile := cfg.RPCState.Profiles["profile"]
	profile.ID = profileID
	cfg.RPCState.ActiveProfileID = profileID
	cfg.RPCState.ExitProtection.ProfileID = profileID
	cfg.RPCState.Profiles = map[string]clientRPCProfile{profileID: profile}
	cfg.RPCState.Revision = 1
	cfg.RPCState.DigestKey = make([]byte, 32)
	cfg.RPCState.Operations = map[string]clientRPCOperationRecord{}
	e.exitConfig.RPCState = clonePersistentConfig(cfg).RPCState
	confirmedPaths := e.relayPaths.Statuses()
	store, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(current *Config) error { *current = clonePersistentConfig(cfg); return nil }); err != nil {
		t.Fatal(err)
	}
	f := &exitLANRuntimeTest{t: t, e: e, store: store, routes: newExitLANRouteTestKernel(t), detached: map[uint32]bool{}}
	g := e.exitGuard
	r := &exitLANRuntime{store: store}
	e.exitLAN = r
	clockStart := time.Now()
	r.ops.clock = func(context.Context) (exitLANClockSample, error) {
		now := time.Now()
		boot := uint64(time.Second + now.Sub(clockStart))
		return exitLANClockSample{boot, boot + 1, now}, nil
	}
	r.ops.inspect = inspect
	r.ops.capture = func(ctx, lifetime context.Context, own string, family api.ExitFamilyMode) (*exitLANSource, error) {
		// Model a fresh successful path observation after native reconfiguration.
		// The UAPI seam supplies its authenticated handshake independently.
		e.relayPaths.statuses = confirmedPaths
		stream := &exitLANTestStream{changed: make(chan struct{})}
		f.streams = append(f.streams, stream)
		return &exitLANSource{OwnInterface: own, Family: family, lifetime: &exitLANSourceLifetime{ctx: lifetime, stream: stream}, Links: []exitLANLink{{Instance: 99, Index: 2, LinkIndex: 2, Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.2/24")}, Routes: []exitLANDirectRoute{{Prefix: netip.MustParsePrefix("192.0.2.0/24")}}}}}, ctx.Err()
	}
	namespace := func(ctx context.Context) (*exitLANBootNamespace, error) {
		return newExitLANBootNamespace(ctx, func(context.Context) (exitLANNamespaceIdentity, error) { return exitLANNamespaceTestIdentity(), nil }, func() error { return nil })
	}
	directory := func(context.Context) (*exitLANBPFDirectory, error) {
		return &exitLANBPFDirectory{fd: 800, check: func(int) error { return nil }, closeFD: func(int) error { return nil }, unlink: func(_ int, name string) error { delete(f.kernel.pins, name); return nil }}, nil
	}
	call := func(command int, attr []byte, buffers ...[]byte) (int, error) {
		if command == exitLANBPFLinkDetach {
			id, ok := f.kernel.link.links[int(binary.LittleEndian.Uint32(attr))]
			if !ok {
				t.Fatal("foreign runtime detach")
			}
			f.detached[id.id] = true
			return 0, nil
		}
		return f.kernel.call(command, attr, buffers...)
	}
	r.ops.observeHook = func(_ context.Context, id exitLANBPFLinkIdentity) error {
		if f.detached[id.id] {
			return errExitLANBPF
		}
		return nil
	}
	r.ops.session = func(ctx context.Context, g *linuxExitGuard, plan *exitLANPlan, mark uint32, scope string, checkpoint func(context.Context, *exitLANOwnership) error) (*exitLANBPFSession, error) {
		return prepareExitLANBPFSession(ctx, mark, plan.topology.Family, exitLANPacketHook, exitLANPacketPriority, scope, checkpoint, g.ObserveContained, exitLANBPFSessionOps{namespace: namespace, directory: directory, observe: r.ops.observeHook, preparation: func(ctx context.Context, mark uint32) (*exitLANBPFPreparation, error) {
			base := newExitLANBPFTestKernel(t, binary.LittleEndian)
			f.detached = map[uint32]bool{}
			link := &exitLANBPFLinkTestKernel{k: base, links: map[int]exitLANBPFLinkIdentity{}}
			f.kernel = &exitLANBPFPinTestKernel{link: link, pins: map[string]int{}}
			layout, err := exitLANBTFPacketLayout(exitLANPacketBTFFixture(binary.LittleEndian).raw())
			if err != nil {
				return nil, err
			}
			return newExitLANBPFPreparationWithProgram(ctx, 32, 45, binary.LittleEndian, call, base.close, func(fd int) ([]byte, error) {
				code, err := buildExitLANPacketProgram(layout, []exitLANPacketDevice{{2, 99}}, mark, fd, binary.LittleEndian)
				f.code = code
				return code, err
			})
		}})
	}
	r.ops.cleanup = func(ctx context.Context, g *linuxExitGuard, owned *exitLANOwnership) error {
		if f.kernel == nil {
			return errExitLANBPF
		}
		recovery, err := recoverExitLANBPF(ctx, owned, g.ObserveContained, exitLANBPFRecoveryOps{namespace: namespace, directory: directory, pins: func(ctx context.Context, d *exitLANBPFDirectory, o *exitLANOwnership) (*exitLANBPFOwnedPins, error) {
			return openExitLANBPFOwnedPins(ctx, d, o, binary.LittleEndian, call, f.kernel.link.k.close)
		}})
		if err != nil {
			return err
		}
		err = recovery.cleanup(ctx, g.ObserveContained, binary.LittleEndian, call, func(_ context.Context, id exitLANBPFLinkIdentity) error {
			if !f.detached[id.id] {
				return errExitLANBPF
			}
			return nil
		})
		if err == nil {
			err = cleanupExitLANRouting(ctx, g, owned.Routing)
		}
		return errors.Join(err, recovery.Close())
	}
	previous := g.run
	lanTable := strconv.FormatUint(uint64(g.mark^0x40000000), 10)
	lanApplied := false
	base := exitGuardReadbackRunner(t, g.interfaceName, g.mark, func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		if name == "nft" {
			lanApplied = strings.Contains(input, " lanroute ")
			if lanApplied {
				f.opened++
			}
			return nil, nil
		}
		if slices.Contains(args, lanTable) {
			return f.routes.run(ctx, input, name, args...)
		}
		if name == "ip" && !e.configured {
			return []byte(`[]`), nil
		}
		return previous(ctx, input, name, args...)
	})
	g.run = func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
		raw, err := base(ctx, input, name, args...)
		if err != nil || name != "nft" || input != "" || !lanApplied || !slices.Contains(args, "table") {
			return raw, err
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		_, out, marking, err := g.lan.rules(g.table, g.mark)
		if err != nil {
			return nil, err
		}
		objects := value["nftables"].([]any)
		objects = append(objects, map[string]any{"chain": map[string]any{"family": "inet", "table": g.table, "name": "lanroute", "type": "route", "hook": "output", "prio": -150, "policy": "accept"}})
		objects = append(objects, map[string]any{"chain": map[string]any{"family": "inet", "table": g.table, "name": "lanfinal", "type": "filter", "hook": "postrouting", "prio": 2147483645, "policy": "accept"}})
		for _, set := range []struct {
			chain string
			rules []any
		}{{"output", out}, {"lanroute", marking}, {"lanfinal", g.lan.finalRules(out)}} {
			for _, rule := range set.rules {
				objects = append(objects, map[string]any{"rule": map[string]any{"family": "inet", "table": g.table, "chain": set.chain, "expr": rule}})
			}
		}
		value["nftables"] = objects
		return json.Marshal(value)
	}
	f.n = &nativeExitExecutor{engine: e, store: store, createGuard: func(string, string) (*linuxExitGuard, error) { return g, nil }, cleanupLANObjects: r.ops.cleanup}
	return f
}

func TestNativeExitLANRuntimeAppliesObservesMaintainsAndClears(t *testing.T) {
	f := newExitLANRuntimeTest(t)
	cfg := f.store.Read()
	result, err := f.e.configureExit(t.Context(), cfg, *cfg.CachedMap, cfg.ExitSelection, f.e.exitGuard)
	if err != nil || !result.OK {
		t.Fatal("integrated LAN apply", err)
	}
	if f.opened != 1 || len(f.code) <= 23*8 || f.store.Read().RPCState.ExitProtection.LAN == nil {
		t.Fatal("LAN bypassed packet program or durable ownership")
	}
	status, err := f.n.observe(t.Context(), f.store.Read())
	if err != nil || status.GetEffectiveLanAccess().String() != "LAN_ACCESS_ALLOW" {
		t.Fatal("LAN not observed", err, status)
	}
	deadline := f.e.exitLAN.deadline.bootExpires
	if err := f.n.maintain(t.Context(), f.store.Read()); err != nil {
		t.Fatal("healthy maintenance", err)
	}
	if f.e.exitLAN.deadline.bootExpires > deadline {
		t.Fatal("same evidence extended kernel authority")
	}
	scope := cloneExitProtection(f.store.Read().RPCState.ExitProtection)
	if err := f.n.stopOwned(t.Context(), scope, f.e.exitGuard, "https://control.example"); err != nil {
		t.Fatal("owned stop/cleanup", err)
	}
	if f.store.Read().RPCState.ExitProtection.LAN != nil || len(f.kernel.pins) != 0 || len(f.routes.routes["-4"])+len(f.routes.rules) != 0 {
		t.Fatal("clear retained LAN artifacts")
	}
	if err := f.e.exitGuard.ObserveContained(t.Context()); err != nil {
		t.Fatal("cleanup released containment", err)
	}
}

func TestNativeExitLANRuntimeContainsTopologyLossAndMissingPins(t *testing.T) {
	for _, failure := range []string{"topology", "pin", "health", "context"} {
		t.Run(failure, func(t *testing.T) {
			f := newExitLANRuntimeTest(t)
			cfg := f.store.Read()
			if result, err := f.e.configureExit(t.Context(), cfg, *cfg.CachedMap, cfg.ExitSelection, f.e.exitGuard); err != nil || !result.OK {
				t.Fatal(err)
			}
			switch failure {
			case "topology":
				_ = f.streams[len(f.streams)-1].Close()
			case "pin":
				for name := range f.kernel.pins {
					delete(f.kernel.pins, name)
					break
				}
			case "health":
				f.e.exitLAN.ops.inspect = func(*WireGuardEngine) (WireGuardInspection, error) { return WireGuardInspection{}, errExitLANPolicy }
			case "context":
				if err := f.store.Update(func(cfg *Config) error {
					cfg.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.n.maintain(t.Context(), f.store.Read()); err == nil {
				t.Fatal("unconfirmed LAN remained applied")
			}
			if err := f.e.exitGuard.ObserveContained(t.Context()); err != nil {
				t.Fatal("LAN loss did not contain", err)
			}
			if f.e.exitLAN.session != nil || f.e.exitLAN.plan != nil || f.store.Read().RPCState.ExitProtection.LAN == nil {
				t.Fatal("containment lost cleanup journal or retained live lease")
			}
		})
	}
}

func TestNativeExitLANRuntimeDurableSelectAndClear(t *testing.T) {
	f := newExitLANRuntimeTest(t)
	if err := f.store.Update(func(cfg *Config) error { cfg.ExitSelection.LAN = api.ExitLANBlock; return nil }); err != nil {
		t.Fatal(err)
	}
	m, err := NewClientRPCMutations(f.store)
	if err != nil {
		t.Fatal(err)
	}
	owner := local.Peer{Identity: f.store.Read().LocalOwnerID}
	profile := &ipc.ProfileRef{ProfileId: f.store.Read().RPCState.ActiveProfileID}
	executor := clientRPCExitExecutor{InterfaceName: f.e.opts.Interface, Modes: []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANAllow}}, Lock: new(sync.Mutex), Apply: f.n.apply, Contain: f.n.contain, Release: f.n.release, Observe: f.n.observe, Maintain: f.n.maintain, ResumeSaved: f.n.resumeSaved}
	selected, err := m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: "exit", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_ALLOW}, []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANAllow}})
	if err != nil {
		t.Fatal("admission", err)
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal("select", err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: selected.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("select did not commit", err, result)
	}
	if f.store.Read().RPCState.ExitProtection.LAN == nil {
		t.Fatal("selection lost LAN cleanup journal")
	}
	if err := f.n.maintain(t.Context(), f.store.Read()); err != nil {
		t.Fatal("durable commit invalidated packet evidence", err)
	}
	cleared, err := m.clearExitNodeAs(owner, &ipc.ClearExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile})
	if err != nil {
		t.Fatal("clear admission", err)
	}
	if err := m.reconcileExitChange(t.Context(), executor); err != nil {
		t.Fatal("clear", err)
	}
	result, err = m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: cleared.Id}})
	if err != nil || result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || f.store.Read().RPCState.ExitProtection != nil || len(f.kernel.pins) != 0 {
		t.Fatal("clear did not retire observed native ownership", err, result)
	}
}

func TestNativeExitLANRuntimeRetriesOnlyUnfinishedPeerHealth(t *testing.T) {
	f := newExitLANRuntimeTest(t)
	healthy := f.e.exitLAN.ops.inspect
	f.e.exitLAN.ops.inspect = func(*WireGuardEngine) (WireGuardInspection, error) { return WireGuardInspection{}, errExitLANPolicy }
	cfg := f.store.Read()
	if result, err := f.e.configureExit(t.Context(), cfg, *cfg.CachedMap, cfg.ExitSelection, f.e.exitGuard); err == nil || result.OK {
		t.Fatal("missing handshake opened LAN")
	}
	if !f.e.exitLAN.awaitingPeer || f.opened != 0 {
		t.Fatal("unfinished cold start lost its state")
	}
	if err := f.e.exitGuard.ObserveContained(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.e.exitLAN.ops.inspect = healthy
	if _, err := f.n.resumeSaved(t.Context(), f.store.Read()); err != nil {
		t.Fatal("new handshake could not finish saved start", err)
	}
	if f.e.exitLAN.awaitingPeer {
		t.Fatal("completed application remained pending")
	}
	f.e.exitLAN.ops.inspect = func(*WireGuardEngine) (WireGuardInspection, error) { return WireGuardInspection{}, errExitLANPolicy }
	if err := f.n.maintain(t.Context(), f.store.Read()); err == nil {
		t.Fatal("lost exit health accepted")
	}
	f.e.exitLAN.ops.inspect = healthy
	if _, err := f.n.resumeSaved(t.Context(), f.store.Read()); err == nil {
		t.Fatal("observation silently reopened a failed applied runtime")
	}
}

func TestNativeExitLANRuntimeContainsPartialApplyAndLateCancellation(t *testing.T) {
	for _, scenario := range []string{"route_failure", "cancel_after_open", "topology_during_open"} {
		t.Run(scenario, func(t *testing.T) {
			f := newExitLANRuntimeTest(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "route_failure" {
				f.routes.fail = 2
			}
			previous := f.e.exitGuard.run
			f.e.exitGuard.run = func(ctx context.Context, input, name string, args ...string) ([]byte, error) {
				raw, err := previous(ctx, input, name, args...)
				if name == "nft" && strings.Contains(input, " lanroute ") {
					if scenario == "cancel_after_open" {
						cancel()
					}
					if scenario == "topology_during_open" {
						_ = f.streams[len(f.streams)-1].Close()
					}
				}
				return raw, err
			}
			cfg := f.store.Read()
			if result, err := f.e.configureExit(ctx, cfg, *cfg.CachedMap, cfg.ExitSelection, f.e.exitGuard); err == nil || result.OK {
				t.Fatal("uncertain application succeeded")
			}
			if f.e.exitLAN.session != nil || f.store.Read().RPCState.ExitProtection.LAN == nil {
				t.Fatal("partial apply lost fail-closed recovery")
			}
			if err := f.e.exitGuard.ObserveContained(t.Context()); err != nil {
				t.Fatal(err)
			}
			scope := cloneExitProtection(f.store.Read().RPCState.ExitProtection)
			if err := f.n.stopOwned(t.Context(), scope, f.e.exitGuard, "https://control.example"); err != nil {
				t.Fatal("partial recovery", err)
			}
			if f.store.Read().RPCState.ExitProtection.LAN != nil || len(f.kernel.pins) != 0 {
				t.Fatal("recovery did not retire confirmed ownership")
			}
		})
	}
}

func TestNativeExitLANRuntimeClockChangesCannotExtendAuthority(t *testing.T) {
	f := newExitLANRuntimeTest(t)
	cfg := f.store.Read()
	if result, err := f.e.configureExit(t.Context(), cfg, *cfg.CachedMap, cfg.ExitSelection, f.e.exitGuard); err != nil || !result.OK {
		t.Fatal(err)
	}
	original := f.e.exitLAN.deadline.bootExpires
	clock := f.e.exitLAN.ops.clock
	f.e.exitLAN.ops.clock = func(ctx context.Context) (exitLANClockSample, error) {
		sample, err := clock(ctx)
		sample.wall = sample.wall.Add(5 * time.Second)
		return sample, err
	}
	if err := f.n.maintain(t.Context(), f.store.Read()); err != nil {
		t.Fatal("forward clock sample", err)
	}
	bounded := f.e.exitLAN.deadline.bootExpires
	if bounded > original-uint64(4*time.Second) {
		t.Fatal("forward wall adjustment did not cap remaining kernel time")
	}
	f.e.exitLAN.ops.clock = clock
	if err := f.n.maintain(t.Context(), f.store.Read()); err != nil {
		t.Fatal(err)
	}
	if f.e.exitLAN.deadline.bootExpires > bounded {
		t.Fatal("repeated evidence regained time")
	}
	f.e.exitLAN.ops.clock = func(ctx context.Context) (exitLANClockSample, error) {
		sample, err := clock(ctx)
		sample.wall = sample.wall.Add(-time.Hour)
		return sample, err
	}
	if err := f.n.maintain(t.Context(), f.store.Read()); err == nil {
		t.Fatal("rollback before original anchor retained authority")
	}
	if err := f.e.exitGuard.ObserveContained(t.Context()); err != nil {
		t.Fatal(err)
	}
}
