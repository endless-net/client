package client

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"

	wgkeys "github.com/unng-lab/endlessnet/clientapi/wireguard"

	"github.com/tailscale/wireguard-go/device"
	"github.com/tailscale/wireguard-go/tun"
)

const defaultWireGuardEngineMTU = 1420

type wireGuardEngineApplyStage string

const (
	wireGuardEngineStageStart       wireGuardEngineApplyStage = "start runtime"
	wireGuardEngineStagePathProbe   wireGuardEngineApplyStage = "configure path probing"
	wireGuardEngineStageInitialUAPI wireGuardEngineApplyStage = "configure initial UAPI"
	wireGuardEngineStageDeviceUp    wireGuardEngineApplyStage = "start device"
	wireGuardEngineStageRelay       wireGuardEngineApplyStage = "configure relay bridge"
	wireGuardEngineStageRoutes      wireGuardEngineApplyStage = "configure routes"
	wireGuardEngineStageDesiredUAPI wireGuardEngineApplyStage = "configure desired UAPI"
)

type WireGuardEngineOptions struct {
	Interface        string
	ListenPort       int
	MTU              int
	Runner           CommandRunner
	RelayTimeout     time.Duration
	RelayTLSConfig   *tls.Config
	RelayDirectRetry time.Duration
	inputRunner      commandInputRunner

	tunFactory func(string, int) (tun.Device, error)
	router     wireGuardEngineRouter
	stageHook  func(wireGuardEngineApplyStage) error
}

// WireGuardEngine embeds wireguard-go and supplies MagicBind as its UDP
// transport. The engine owns the TUN device, UDP socket, and platform routes
// for its entire connected lifetime.
type WireGuardEngine struct {
	mu sync.Mutex

	opts        WireGuardEngineOptions
	tun         tun.Device
	device      *device.Device
	bind        *MagicBind
	router      wireGuardEngineRouter
	interface_  string
	configured  bool
	routerCfg   wireGuardEngineRouterConfig
	uapi        string
	discovery   WireGuardEngineEndpointDiscovery
	relayBridge *wireGuardRelayBridge
	relayPaths  *wireGuardRelayPathManager
	portMapping automaticPortMappingState
	pathMap     clientapi.RegisterNodeResponse
	pathKey     string
	pathWake    chan struct{}
	pathCancel  context.CancelFunc
}

type WireGuardEngineEndpointDiscovery struct {
	Results      []STUNCheckResult
	PortMappings []PortMappingResult
	Candidates   []string
	Port         int
}

type wireGuardEngineSnapshot struct {
	configured        bool
	mtu               int
	device            *device.Device
	bind              *MagicBind
	listenPort        int
	routerCfg         wireGuardEngineRouterConfig
	uapi              string
	pathProbe         magicBindPathProbeSnapshot
	pathMap           clientapi.RegisterNodeResponse
	pathKey           string
	relayPaths        *wireGuardRelayPathManager
	relayBridge       wireGuardRelayBridgeSnapshot
	pathMonitorActive bool
}

type wireGuardEnginePlan struct {
	config      Config
	mtu         int
	networkMap  clientapi.RegisterNodeResponse
	privateKey  string
	pathProbe   magicBindPathProbeSnapshot
	routerCfg   wireGuardEngineRouterConfig
	initialUAPI string
}

type wireGuardEngineApplyProgress struct {
	runtimeReplaced bool
	pathProbe       bool
	relayBridge     bool
	routes          bool
	device          bool
}

type magicBindPathProbeSnapshot struct {
	keys        map[[32]byte][32]byte
	localPublic [32]byte
	enabled     bool
}

type wireGuardRelayBridgeSnapshot struct {
	running bool
}

func NewWireGuardEngine(opts WireGuardEngineOptions) (*WireGuardEngine, error) {
	opts.Interface = strings.TrimSpace(opts.Interface)
	if opts.Interface == "" {
		opts.Interface = "endlessnet"
	}
	if !safeWireGuardInterfaceName(opts.Interface) {
		return nil, fmt.Errorf("wireguard-go interface %q is invalid", opts.Interface)
	}
	if opts.ListenPort < 0 || opts.ListenPort > 65535 {
		return nil, fmt.Errorf("wireguard-go listen port must be between 0 and 65535")
	}
	if opts.MTU == 0 {
		opts.MTU = defaultWireGuardEngineMTU
	}
	if _, err := NormalizeWireGuardMTU(opts.MTU); err != nil {
		return nil, err
	}
	if opts.tunFactory == nil {
		opts.tunFactory = createWireGuardEngineTUN
	}
	return &WireGuardEngine{
		opts:        opts,
		relayBridge: newWireGuardRelayBridge(opts.RelayTimeout, opts.RelayTLSConfig),
		relayPaths:  newWireGuardRelayPathManager(opts.RelayDirectRetry),
		pathWake:    make(chan struct{}, 1),
	}, nil
}

func createWireGuardEngineTUN(name string, mtu int) (device tun.Device, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			device = nil
			err = fmt.Errorf("initialize platform TUN driver: %v", recovered)
		}
	}()
	return tun.CreateTUN(name, mtu)
}

func (e *WireGuardEngine) Configure(ctx context.Context, cfg Config, networkMap clientapi.RegisterNodeResponse) (WireGuardApplyResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := WireGuardApplyResult{Method: "wireguard-go", Interface: e.interface_}
	previous := e.snapshotLocked()
	plan, err := e.preflightLocked(cfg, networkMap)
	if err != nil {
		result.UpError = err.Error()
		return result, err
	}
	progress := wireGuardEngineApplyProgress{}
	result, err = e.configureLocked(ctx, plan, previous, &progress)
	if err == nil {
		return result, nil
	}
	if rollbackErr := e.restoreLocked(previous, progress); rollbackErr != nil {
		return result, errors.Join(err, fmt.Errorf("rollback wireguard-go configuration: %w", rollbackErr))
	}
	return result, err
}

func (e *WireGuardEngine) preflightLocked(cfg Config, networkMap clientapi.RegisterNodeResponse) (wireGuardEnginePlan, error) {
	plan := wireGuardEnginePlan{
		config:     cfg,
		networkMap: cloneRegisterNodeResponse(networkMap),
		privateKey: strings.TrimSpace(cfg.PrivateKey),
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return plan, errors.New("wireguard private key is missing")
	}
	plan.mtu = cfg.WireGuardMTU
	if plan.mtu == 0 {
		plan.mtu = defaultWireGuardEngineMTU
	}
	if _, err := NormalizeWireGuardMTU(plan.mtu); err != nil {
		return plan, err
	}
	exitBlockLAN, err := ExitLANPolicyBlocksLocalLAN(cfg.ExitLANPolicy)
	if err != nil {
		return plan, err
	}
	if _, err := RenderWireGuardWithOptionsChecked(plan.privateKey, plan.networkMap, WireGuardRenderOptions{
		ListenPort:       e.opts.ListenPort,
		MTU:              plan.mtu,
		RouteTable:       cfg.WireGuardRouteTable,
		Interfaces:       LocalInterfaceStatuses(),
		SubnetRouterSNAT: cfg.SubnetRouterSNAT,
		ExitBlockLAN:     exitBlockLAN,
	}); err != nil {
		return plan, err
	}
	probeBind := NewMagicBind()
	if err := probeBind.ConfigurePathProbing(plan.privateKey, plan.networkMap.Node.PublicKey, plan.networkMap.Peers); err != nil {
		return plan, err
	}
	plan.pathProbe = snapshotMagicBindPathProbing(probeBind)
	interfaceName := e.interface_
	if strings.TrimSpace(interfaceName) == "" {
		interfaceName = platformUserspaceTUNName(e.opts.Interface)
	}
	if err := e.completePlanForInterface(&plan, interfaceName); err != nil {
		return plan, err
	}
	return plan, nil
}

func (e *WireGuardEngine) completePlanForInterface(plan *wireGuardEnginePlan, interfaceName string) error {
	routerCfg, err := buildWireGuardEngineRouterConfig(interfaceName, plan.mtu, plan.config, plan.networkMap)
	if err != nil {
		return err
	}
	initialUAPI, err := wireGuardEngineUAPIWithoutEndpoints(plan.privateKey, plan.networkMap, e.opts.ListenPort, false, routerCfg.FirewallMark)
	if err != nil {
		return err
	}
	// Validate the final UAPI shape before touching runtime. Relay loopback
	// endpoints are only known after Device.Up opens MagicBind, so the final
	// endpoint-bearing UAPI is rendered once more at that boundary.
	if _, err := wireGuardEngineUAPI(plan.privateKey, plan.networkMap, e.opts.ListenPort, true, routerCfg.FirewallMark, nil); err != nil {
		return err
	}
	plan.routerCfg = routerCfg
	plan.initialUAPI = initialUAPI
	return nil
}

func (e *WireGuardEngine) configureLocked(ctx context.Context, plan wireGuardEnginePlan, previous wireGuardEngineSnapshot, progress *wireGuardEngineApplyProgress) (WireGuardApplyResult, error) {
	result := WireGuardApplyResult{Method: "wireguard-go", Interface: e.interface_}
	if e.device != nil && plan.mtu != previous.mtu {
		progress.runtimeReplaced = true
		if err := e.closeLocked(ctx); err != nil {
			result.DownError = err.Error()
			return result, err
		}
	}
	runtimeStarted := false
	if e.device == nil {
		if err := e.startLocked(plan.mtu); err != nil {
			result.UpError = err.Error()
			return result, err
		}
		runtimeStarted = true
		progress.runtimeReplaced = progress.runtimeReplaced || previous.device != nil
		result.Interface = e.interface_
		if e.interface_ != plan.routerCfg.Interface {
			if err := e.completePlanForInterface(&plan, e.interface_); err != nil {
				result.RouteError = err.Error()
				return result, err
			}
		}
		if err := e.runApplyStage(wireGuardEngineStageStart); err != nil {
			result.UpError = err.Error()
			return result, err
		}
	}
	progress.pathProbe = true
	restoreMagicBindPathProbing(e.bind, plan.pathProbe)
	if err := e.runApplyStage(wireGuardEngineStagePathProbe); err != nil {
		result.SyncError = err.Error()
		return result, err
	}
	if runtimeStarted {
		progress.device = true
		if err := e.device.IpcSet(plan.initialUAPI); err != nil {
			result.SyncError = err.Error()
			return result, fmt.Errorf("configure wireguard-go peers: %w", err)
		}
		if err := e.runApplyStage(wireGuardEngineStageInitialUAPI); err != nil {
			result.SyncError = err.Error()
			return result, err
		}
		if err := e.device.Up(); err != nil {
			result.UpError = err.Error()
			return result, fmt.Errorf("start wireguard-go device: %w", err)
		}
		if err := e.runApplyStage(wireGuardEngineStageDeviceUp); err != nil {
			result.UpError = err.Error()
			return result, err
		}
	}
	progress.relayBridge = true
	relayOverrides, relayResult, relayErr := e.relayEndpointOverridesLocked(ctx, plan.networkMap)
	if err := e.runApplyStage(wireGuardEngineStageRelay); err != nil {
		result.SyncError = err.Error()
		return result, err
	}
	nextRelayPaths := cloneWireGuardRelayPathManager(previous.relayPaths)
	if nextRelayPaths == nil {
		nextRelayPaths = newWireGuardRelayPathManager(e.opts.RelayDirectRetry)
	}
	var endpointOverrides map[string]string
	if !previous.configured {
		endpointOverrides = nextRelayPaths.Bootstrap(plan.networkMap, relayOverrides, relayResult, relayErr, time.Now().UTC())
	} else {
		endpointOverrides, _ = nextRelayPaths.Reconcile(plan.networkMap, relayOverrides, relayResult, relayErr, nil, time.Now().UTC())
	}
	desiredUAPI, err := wireGuardEngineUAPI(plan.privateKey, plan.networkMap, e.opts.ListenPort, true, plan.routerCfg.FirewallMark, endpointOverrides)
	if err != nil {
		result.SyncError = err.Error()
		return result, err
	}
	routerChanged := runtimeStarted || !previous.configured || !wireGuardEngineRouterConfigsEqual(plan.routerCfg, previous.routerCfg)
	if routerChanged {
		progress.routes = true
		if err := e.router.Configure(ctx, plan.routerCfg); err != nil {
			result.RouteError = err.Error()
			return result, fmt.Errorf("configure wireguard-go routes: %w", err)
		}
		if err := e.runApplyStage(wireGuardEngineStageRoutes); err != nil {
			result.RouteError = err.Error()
			return result, err
		}
	}
	uapiChanged := runtimeStarted || !previous.configured || desiredUAPI != previous.uapi
	if uapiChanged {
		progress.device = true
		if err := e.device.IpcSet(desiredUAPI); err != nil {
			result.SyncError = err.Error()
			return result, fmt.Errorf("configure wireguard-go relay paths: %w", err)
		}
		if err := e.runApplyStage(wireGuardEngineStageDesiredUAPI); err != nil {
			result.SyncError = err.Error()
			return result, err
		}
	}
	// Commit logical state only after every runtime stage has succeeded.
	e.opts.MTU = plan.mtu
	e.routerCfg = cloneWireGuardEngineRouterConfig(plan.routerCfg)
	e.pathMap = cloneRegisterNodeResponse(plan.networkMap)
	e.pathKey = plan.privateKey
	e.relayPaths = nextRelayPaths
	e.uapi = desiredUAPI
	e.configured = true
	e.startPathMonitorLocked()
	e.wakePathMonitorLocked()
	result.OK = true
	result.Changed = uapiChanged || routerChanged
	result.Skipped = !result.Changed
	result.Interface = e.interface_
	if result.Changed {
		result.Reason = fmt.Sprintf("shared UDP socket listening on port %d", e.bind.LocalPort())
	} else {
		result.Reason = "wireguard-go configuration unchanged"
	}
	return result, nil
}

func (e *WireGuardEngine) restoreLocked(previous wireGuardEngineSnapshot, progress wireGuardEngineApplyProgress) error {
	if !previous.configured {
		if e.device == nil && !progress.runtimeReplaced && !progress.pathProbe && !progress.relayBridge && !progress.routes && !progress.device {
			return nil
		}
		cleanupErr := e.closeLocked(context.Background())
		e.opts.MTU = previous.mtu
		return cleanupErr
	}
	if progress.runtimeReplaced || e.device != previous.device || e.bind != previous.bind {
		return e.restoreRecreatedLocked(previous)
	}
	var rollbackErr error
	if progress.pathProbe {
		restoreMagicBindPathProbing(e.bind, previous.pathProbe)
	}
	if progress.routes {
		if err := e.router.Configure(context.Background(), previous.routerCfg); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore wireguard-go routes: %w", err))
		}
	}
	if progress.relayBridge {
		if err := e.restoreRelayBridgeLocked(previous); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	restoredUAPI := previous.uapi
	if progress.device || progress.relayBridge && previous.relayBridge.running {
		var err error
		restoredUAPI, err = e.restoredUAPILocked(previous)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
			restoredUAPI = previous.uapi
		}
		if strings.TrimSpace(restoredUAPI) == "" {
			rollbackErr = errors.Join(rollbackErr, errors.New("restore wireguard-go UAPI: previous UAPI is empty"))
		} else if err := e.device.IpcSet(restoredUAPI); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore wireguard-go UAPI: %w", err))
		}
	}
	if rollbackErr != nil {
		return errors.Join(rollbackErr, e.closeLocked(context.Background()))
	}
	e.uapi = restoredUAPI
	return nil
}

func (e *WireGuardEngine) restoreRecreatedLocked(previous wireGuardEngineSnapshot) error {
	cleanupErr := e.closeLocked(context.Background())
	fail := func(err error) error {
		return errors.Join(cleanupErr, err, e.closeLocked(context.Background()))
	}
	if err := e.startLocked(previous.mtu); err != nil {
		return fail(fmt.Errorf("restore wireguard-go runtime: %w", err))
	}
	restoreMagicBindPathProbing(e.bind, previous.pathProbe)
	restoreListenPort := previous.listenPort
	if restoreListenPort <= 0 {
		restoreListenPort = e.opts.ListenPort
	}
	initialUAPI, err := wireGuardEngineUAPIWithoutEndpoints(previous.pathKey, previous.pathMap, restoreListenPort, false, previous.routerCfg.FirewallMark)
	if err != nil {
		return fail(fmt.Errorf("render restored wireguard-go peers: %w", err))
	}
	if err := e.device.IpcSet(initialUAPI); err != nil {
		return fail(fmt.Errorf("restore wireguard-go peers: %w", err))
	}
	if err := e.device.Up(); err != nil {
		return fail(fmt.Errorf("restart wireguard-go device: %w", err))
	}
	restoredRouterCfg := wireGuardEngineRouterConfigForInterface(previous.routerCfg, e.interface_)
	if err := e.router.Configure(context.Background(), restoredRouterCfg); err != nil {
		return fail(fmt.Errorf("restore wireguard-go routes: %w", err))
	}
	if err := e.restoreRelayBridgeLocked(previous); err != nil {
		return fail(err)
	}
	restoredUAPI, err := e.restoredUAPILocked(previous)
	if err != nil {
		return fail(err)
	}
	if err := e.device.IpcSet(restoredUAPI); err != nil {
		return fail(fmt.Errorf("restore wireguard-go UAPI: %w", err))
	}
	e.opts.MTU = previous.mtu
	e.routerCfg = restoredRouterCfg
	e.uapi = restoredUAPI
	e.pathMap = cloneRegisterNodeResponse(previous.pathMap)
	e.pathKey = previous.pathKey
	e.relayPaths = cloneWireGuardRelayPathManager(previous.relayPaths)
	e.configured = true
	if previous.pathMonitorActive {
		e.startPathMonitorLocked()
		e.wakePathMonitorLocked()
	}
	return cleanupErr
}

func (e *WireGuardEngine) startLocked(mtu int) error {
	tunDevice, err := e.opts.tunFactory(platformUserspaceTUNName(e.opts.Interface), mtu)
	if err != nil {
		return fmt.Errorf("create wireguard-go TUN: %w", err)
	}
	interfaceName, err := tunDevice.Name()
	if err != nil {
		_ = tunDevice.Close()
		return fmt.Errorf("read wireguard-go TUN name: %w", err)
	}
	bind := NewMagicBind()
	logger := &device.Logger{
		Verbosef: device.DiscardLogf,
		Errorf: func(format string, args ...any) {
			log.Printf("WireGuard engine: "+format, args...)
		},
	}
	wgDevice := device.NewDevice(tunDevice, bind, logger)
	router := e.opts.router
	if router == nil {
		router, err = newWireGuardEngineRouter(interfaceName, e.opts.Runner, e.opts.inputRunner)
		if err != nil {
			wgDevice.Close()
			return err
		}
	}
	e.tun = tunDevice
	e.device = wgDevice
	e.bind = bind
	e.router = router
	e.interface_ = interfaceName
	return nil
}

func (e *WireGuardEngine) runApplyStage(stage wireGuardEngineApplyStage) error {
	if e.opts.stageHook == nil {
		return nil
	}
	if err := e.opts.stageHook(stage); err != nil {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return nil
}

func (e *WireGuardEngine) snapshotLocked() wireGuardEngineSnapshot {
	listenPort := 0
	if e.bind != nil {
		listenPort = e.bind.LocalPort()
	}
	return wireGuardEngineSnapshot{
		configured:        e.configured,
		mtu:               e.opts.MTU,
		device:            e.device,
		bind:              e.bind,
		listenPort:        listenPort,
		routerCfg:         cloneWireGuardEngineRouterConfig(e.routerCfg),
		uapi:              e.uapi,
		pathProbe:         snapshotMagicBindPathProbing(e.bind),
		pathMap:           cloneRegisterNodeResponse(e.pathMap),
		pathKey:           e.pathKey,
		relayPaths:        cloneWireGuardRelayPathManager(e.relayPaths),
		relayBridge:       snapshotWireGuardRelayBridge(e.relayBridge),
		pathMonitorActive: e.pathCancel != nil,
	}
}

func snapshotMagicBindPathProbing(bind *MagicBind) magicBindPathProbeSnapshot {
	if bind == nil {
		return magicBindPathProbeSnapshot{}
	}
	bind.mu.RLock()
	defer bind.mu.RUnlock()
	snapshot := magicBindPathProbeSnapshot{
		keys:        make(map[[32]byte][32]byte, len(bind.pathKeys)),
		localPublic: bind.pathLocalPublic,
		enabled:     bind.pathProbeEnabled,
	}
	for publicKey, sharedKey := range bind.pathKeys {
		snapshot.keys[publicKey] = sharedKey
	}
	return snapshot
}

func restoreMagicBindPathProbing(bind *MagicBind, snapshot magicBindPathProbeSnapshot) {
	if bind == nil {
		return
	}
	bind.mu.Lock()
	bind.pathKeys = make(map[[32]byte][32]byte, len(snapshot.keys))
	for publicKey, sharedKey := range snapshot.keys {
		bind.pathKeys[publicKey] = sharedKey
	}
	bind.pathLocalPublic = snapshot.localPublic
	bind.pathProbeEnabled = snapshot.enabled
	for nonce := range bind.pathWaiters {
		delete(bind.pathWaiters, nonce)
	}
	bind.mu.Unlock()
}

func snapshotWireGuardRelayBridge(bridge *wireGuardRelayBridge) wireGuardRelayBridgeSnapshot {
	if bridge == nil {
		return wireGuardRelayBridgeSnapshot{}
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.reapLocked()
	return wireGuardRelayBridgeSnapshot{
		running: bridge.cancel != nil && bridge.statusOK,
	}
}

func (e *WireGuardEngine) restoreRelayBridgeLocked(previous wireGuardEngineSnapshot) error {
	if e.relayBridge == nil {
		if previous.relayBridge.running {
			return errors.New("restore wireguard-go relay bridge: bridge is unavailable")
		}
		return nil
	}
	if !previous.relayBridge.running {
		e.relayBridge.Stop()
		return nil
	}
	if e.bind == nil {
		return errors.New("restore wireguard-go relay bridge: UDP bind is unavailable")
	}
	if err := e.relayBridge.Ensure(context.Background(), previous.pathMap, e.bind.LoopbackEndpoint()); err != nil {
		return fmt.Errorf("restore wireguard-go relay bridge: %w", err)
	}
	return nil
}

func (e *WireGuardEngine) restoredUAPILocked(previous wireGuardEngineSnapshot) (string, error) {
	if !previous.relayBridge.running {
		return previous.uapi, nil
	}
	status, ok, err := e.relayBridge.Status()
	if err != nil {
		return "", fmt.Errorf("read restored wireguard-go relay bridge: %w", err)
	}
	if !ok {
		return "", errors.New("read restored wireguard-go relay bridge: bridge is not running")
	}
	overrides := wireGuardRelayPathOverrides(previous.pathMap, previous.relayPaths, status.PeerEndpoints)
	uapi, err := wireGuardEngineUAPI(previous.pathKey, previous.pathMap, e.opts.ListenPort, true, previous.routerCfg.FirewallMark, overrides)
	if err != nil {
		return "", fmt.Errorf("render restored wireguard-go UAPI: %w", err)
	}
	return uapi, nil
}

func wireGuardRelayPathOverrides(networkMap clientapi.RegisterNodeResponse, manager *wireGuardRelayPathManager, relayOverrides map[string]string) map[string]string {
	if manager == nil {
		return nil
	}
	overrides := make(map[string]string, len(networkMap.Peers))
	for _, peer := range networkMap.Peers {
		peerID := strings.TrimSpace(peer.ID)
		state := manager.peers[peerID]
		if state == nil {
			continue
		}
		switch state.selectedPath {
		case "relay":
			if endpoint := strings.TrimSpace(relayOverrides[peerID]); endpoint != "" {
				overrides[peerID] = endpoint
			}
		case "direct":
			if endpoint := strings.TrimSpace(state.selectedEndpoint); endpoint != "" {
				overrides[peerID] = endpoint
			}
		}
	}
	return overrides
}

func cloneWireGuardEngineRouterConfig(value wireGuardEngineRouterConfig) wireGuardEngineRouterConfig {
	value.Addresses = append([]netip.Prefix(nil), value.Addresses...)
	value.Routes = append([]netip.Prefix(nil), value.Routes...)
	value.DNS = append([]netip.Addr(nil), value.DNS...)
	value.PostUp = append([]string(nil), value.PostUp...)
	value.PreDown = append([]string(nil), value.PreDown...)
	return value
}

func wireGuardEngineRouterConfigForInterface(value wireGuardEngineRouterConfig, interfaceName string) wireGuardEngineRouterConfig {
	value = cloneWireGuardEngineRouterConfig(value)
	previousInterface := value.Interface
	value.Interface = interfaceName
	if previousInterface == interfaceName || strings.TrimSpace(previousInterface) == "" {
		return value
	}
	for index := range value.PostUp {
		value.PostUp[index] = strings.ReplaceAll(value.PostUp[index], previousInterface, interfaceName)
	}
	for index := range value.PreDown {
		value.PreDown[index] = strings.ReplaceAll(value.PreDown[index], previousInterface, interfaceName)
	}
	return value
}

func cloneRegisterNodeResponse(value clientapi.RegisterNodeResponse) clientapi.RegisterNodeResponse {
	value.Network.DNS = append([]string(nil), value.Network.DNS...)
	value.Node.EndpointCandidates = append([]string(nil), value.Node.EndpointCandidates...)
	value.Node.AdvertisedIPs = append([]string(nil), value.Node.AdvertisedIPs...)
	value.Node.RequestedTags = append([]string(nil), value.Node.RequestedTags...)
	value.Node.Tags = append([]string(nil), value.Node.Tags...)
	value.Node.EndpointExpiresAt = cloneTimePointer(value.Node.EndpointExpiresAt)
	value.Node.KeyExpiresAt = cloneTimePointer(value.Node.KeyExpiresAt)
	value.Peers = append([]clientapi.Peer(nil), value.Peers...)
	for index := range value.Peers {
		value.Peers[index].EndpointCandidates = append([]string(nil), value.Peers[index].EndpointCandidates...)
		value.Peers[index].AllowedIPs = append([]string(nil), value.Peers[index].AllowedIPs...)
		value.Peers[index].AllowedPorts = append([]clientapi.ACLPort(nil), value.Peers[index].AllowedPorts...)
		value.Peers[index].Tags = append([]string(nil), value.Peers[index].Tags...)
		value.Peers[index].EndpointExpiresAt = cloneTimePointer(value.Peers[index].EndpointExpiresAt)
	}
	value.STUNEndpoints = append([]clientapi.STUNEndpoint(nil), value.STUNEndpoints...)
	value.Relays = append(value.Relays[:0:0], value.Relays...)
	if value.RelayCredential != nil {
		credential := *value.RelayCredential
		value.RelayCredential = &credential
	}
	if value.MapSignature != nil {
		signature := *value.MapSignature
		value.MapSignature = &signature
	}
	return value
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneWireGuardRelayPathManager(manager *wireGuardRelayPathManager) *wireGuardRelayPathManager {
	if manager == nil {
		return nil
	}
	cloned := &wireGuardRelayPathManager{
		peers:    make(map[string]*wireGuardPeerPathSelection, len(manager.peers)),
		statuses: manager.Statuses(),
	}
	for peerID, state := range manager.peers {
		if state == nil {
			continue
		}
		stateClone := *state
		stateClone.candidates = make(map[string]*wireGuardDirectCandidateHealth, len(state.candidates))
		for endpoint, health := range state.candidates {
			if health == nil {
				continue
			}
			healthClone := *health
			stateClone.candidates[endpoint] = &healthClone
		}
		cloned.peers[peerID] = &stateClone
	}
	return cloned
}

func (e *WireGuardEngine) DiscoverEndpoints(ctx context.Context, endpoints []clientapi.STUNEndpoint, timeout time.Duration) (WireGuardEngineEndpointDiscovery, error) {
	e.mu.Lock()
	bind := e.bind
	e.mu.Unlock()
	if bind == nil || bind.LocalPort() == 0 {
		return WireGuardEngineEndpointDiscovery{}, errors.New("wireguard-go UDP socket is not running")
	}
	results := bind.CheckSTUN(ctx, endpoints, timeout)
	portMappings := e.refreshAutomaticPortMapping(ctx, results, bind.LocalPort(), timeout)
	candidates := make([]string, 0, len(results)+len(portMappings)+8)
	seen := map[string]bool{}
	for _, mapping := range portMappings {
		candidate := strings.TrimSpace(mapping.MappedEndpoint)
		if !mapping.OK || candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}
	for _, result := range results {
		candidate := strings.TrimSpace(result.MappedAddress)
		if !result.Reachable || candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}
	for _, candidate := range LocalEndpointCandidates(bind.LocalPort(), LocalInterfaceStatuses()) {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}
	discovery := WireGuardEngineEndpointDiscovery{
		Results:      results,
		PortMappings: portMappings,
		Candidates:   candidates,
		Port:         bind.LocalPort(),
	}
	e.mu.Lock()
	e.discovery = discovery
	e.mu.Unlock()
	return discovery, nil
}

func (e *WireGuardEngine) LastEndpointDiscovery() WireGuardEngineEndpointDiscovery {
	e.mu.Lock()
	defer e.mu.Unlock()
	return WireGuardEngineEndpointDiscovery{
		Results:      append([]STUNCheckResult(nil), e.discovery.Results...),
		PortMappings: clonePortMappingResults(e.discovery.PortMappings),
		Candidates:   append([]string(nil), e.discovery.Candidates...),
		Port:         e.discovery.Port,
	}
}

func (e *WireGuardEngine) Down(ctx context.Context) (WireGuardApplyResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := WireGuardApplyResult{Method: "wireguard-go", Interface: e.interface_}
	if e.device == nil {
		result.OK = true
		result.Skipped = true
		result.Reason = "wireguard-go is already stopped"
		return result, nil
	}
	err := e.closeLocked(ctx)
	if err != nil {
		result.DownError = err.Error()
		return result, err
	}
	result.OK = true
	result.Changed = true
	result.Reason = "wireguard-go stopped"
	return result, nil
}

func (e *WireGuardEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closeLocked(context.Background())
}

func (e *WireGuardEngine) closeLocked(ctx context.Context) error {
	if e.relayBridge != nil {
		e.relayBridge.Stop()
	}
	var routeErr error
	if e.router != nil {
		routeErr = e.router.Down(ctx)
	}
	if e.device != nil {
		e.device.Close()
	}
	e.tun = nil
	e.device = nil
	e.bind = nil
	e.router = nil
	e.interface_ = ""
	e.configured = false
	e.routerCfg = wireGuardEngineRouterConfig{}
	e.uapi = ""
	e.discovery = WireGuardEngineEndpointDiscovery{}
	e.portMapping = automaticPortMappingState{}
	e.relayPaths = newWireGuardRelayPathManager(e.opts.RelayDirectRetry)
	e.pathMap = clientapi.RegisterNodeResponse{}
	e.pathKey = ""
	if e.pathCancel != nil {
		e.pathCancel()
		e.pathCancel = nil
	}
	return routeErr
}

func (e *WireGuardEngine) Inspection() WireGuardInspection {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inspectionLocked()
}

func (e *WireGuardEngine) inspectionLocked() WireGuardInspection {
	inspection := WireGuardInspection{Interface: e.interface_, Peers: []WireGuardPeerInspection{}, Routes: []WireGuardRouteInspection{}}
	if e.device == nil {
		inspection.Error = "wireguard-go is not running"
		return inspection
	}
	raw, err := e.device.IpcGet()
	if err != nil {
		inspection.Error = err.Error()
		return inspection
	}
	if err := parseWireGuardEngineIPC(&inspection, raw); err != nil {
		inspection.Error = err.Error()
		return inspection
	}
	inspection.MTU = e.opts.MTU
	for _, target := range WireGuardRouteTargetsForPrefixes(e.routerCfg.Routes) {
		inspection.Routes = append(inspection.Routes, WireGuardRouteInspection{
			Target:        target,
			Interface:     e.interface_,
			UsesInterface: true,
		})
	}
	inspection.OK = true
	return inspection
}

func (e *WireGuardEngine) RelayStatus() (RelayDataplaneBridgeStatus, bool, error) {
	e.mu.Lock()
	bridge := e.relayBridge
	e.mu.Unlock()
	if bridge == nil {
		return RelayDataplaneBridgeStatus{}, false, nil
	}
	return bridge.Status()
}

func (e *WireGuardEngine) PathStatus() []PeerPathStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.relayPaths == nil {
		return nil
	}
	return e.relayPaths.Statuses()
}

func (e *WireGuardEngine) relayEndpointOverridesLocked(ctx context.Context, networkMap clientapi.RegisterNodeResponse) (map[string]string, RelayDialResult, error) {
	if e.relayBridge == nil || e.relayPaths == nil || e.bind == nil {
		return nil, RelayDialResult{}, nil
	}
	if err := e.relayBridge.Ensure(ctx, networkMap, e.bind.LoopbackEndpoint()); err != nil {
		log.Printf("wireguard-go relay bridge unavailable: %v", err)
		return nil, RelayDialResult{}, err
	}
	status, ok, relayErr := e.relayBridge.Status()
	if !ok {
		return nil, RelayDialResult{}, relayErr
	}
	return status.PeerEndpoints, status.Relay, relayErr
}

func (e *WireGuardEngine) startPathMonitorLocked() {
	if e.pathCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.pathCancel = cancel
	go e.runPathMonitor(ctx)
}

func (e *WireGuardEngine) wakePathMonitorLocked() {
	select {
	case e.pathWake <- struct{}{}:
	default:
	}
}

func (e *WireGuardEngine) runPathMonitor(ctx context.Context) {
	interval := e.opts.RelayDirectRetry
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.pathWake:
		case <-ticker.C:
		}
		e.reconcilePaths(ctx, interval)
	}
}

func (e *WireGuardEngine) reconcilePaths(ctx context.Context, interval time.Duration) {
	e.mu.Lock()
	if !e.configured || e.device == nil || e.bind == nil || e.relayPaths == nil {
		e.mu.Unlock()
		return
	}
	bind := e.bind
	networkMap := e.pathMap
	mapKey := wireGuardPathMapKey(networkMap)
	e.mu.Unlock()

	probeTimeout := 750 * time.Millisecond
	if interval > 0 && interval/2 < probeTimeout {
		probeTimeout = interval / 2
	}
	if probeTimeout < 250*time.Millisecond {
		probeTimeout = 250 * time.Millisecond
	}
	probes := probeWireGuardPeerCandidates(ctx, bind, networkMap, probeTimeout)
	if ctx.Err() != nil {
		return
	}

	e.mu.Lock()
	if !e.configured || e.device == nil || e.bind != bind || wireGuardPathMapKey(e.pathMap) != mapKey {
		e.mu.Unlock()
		return
	}
	relayOverrides, relayResult, relayErr := e.relayEndpointOverridesLocked(ctx, e.pathMap)
	overrides, triggerTargets := e.relayPaths.Reconcile(e.pathMap, relayOverrides, relayResult, relayErr, probes, time.Now().UTC())
	desiredUAPI, err := wireGuardEngineUAPI(e.pathKey, e.pathMap, e.opts.ListenPort, true, e.routerCfg.FirewallMark, overrides)
	inspection := e.inspectionLocked()
	endpointsMatch := wireGuardEndpointsMatch(e.pathMap, inspection, overrides)
	if err == nil && (desiredUAPI != e.uapi || !endpointsMatch) {
		if setErr := e.device.IpcSet(desiredUAPI); setErr != nil {
			log.Printf("wireguard-go path selection update failed: %v", setErr)
		} else {
			e.uapi = desiredUAPI
		}
	}
	e.mu.Unlock()
	for _, target := range triggerTargets {
		triggerWireGuardHandshake(target)
	}
}

func wireGuardEndpointsMatch(networkMap clientapi.RegisterNodeResponse, inspection WireGuardInspection, overrides map[string]string) bool {
	if !inspection.OK {
		return false
	}
	interfaces := LocalInterfaceStatuses()
	for _, peer := range networkMap.Peers {
		expected := strings.TrimSpace(overrides[peer.ID])
		if expected == "" {
			expected = preferredPeerEndpoint(peer, interfaces)
		}
		if expected == "" {
			continue
		}
		live, ok := wireGuardPeerForMapPeer(inspection, peer)
		if !ok || !wireGuardEndpointsEqual(expected, live.Endpoint) {
			return false
		}
	}
	return true
}

func wireGuardEndpointsEqual(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	leftAddr, leftErr := net.ResolveUDPAddr("udp", left)
	rightAddr, rightErr := net.ResolveUDPAddr("udp", right)
	return leftErr == nil && rightErr == nil && leftAddr.Port == rightAddr.Port && leftAddr.IP.Equal(rightAddr.IP)
}

func probeWireGuardPeerCandidates(ctx context.Context, bind *MagicBind, networkMap clientapi.RegisterNodeResponse, timeout time.Duration) map[string][]DirectEndpointProbe {
	results := make(map[string][]DirectEndpointProbe, len(networkMap.Peers))
	if bind == nil || len(networkMap.Peers) == 0 {
		return results
	}
	routes := wireGuardPathProbeExcludedRoutes(networkMap)
	const maxConcurrentPeers = 16
	semaphore := make(chan struct{}, maxConcurrentPeers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, peer := range networkMap.Peers {
		peer := peer
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			peerResults := bind.ProbePeerEndpoints(ctx, peerWithPathProbeEndpoints(peer, routes), timeout)
			mu.Lock()
			results[strings.TrimSpace(peer.ID)] = peerResults
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func wireGuardPathProbeExcludedRoutes(networkMap clientapi.RegisterNodeResponse) []netip.Prefix {
	routes := []netip.Prefix{}
	seen := map[netip.Prefix]bool{}
	for _, value := range []string{networkMap.Network.CIDR, networkMap.Network.IPv6CIDR} {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil {
			prefix = prefix.Masked()
			if !seen[prefix] {
				seen[prefix] = true
				routes = append(routes, prefix)
			}
		}
	}
	for _, peer := range networkMap.Peers {
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil || prefix.Bits() == 0 {
				continue
			}
			prefix = prefix.Masked()
			if !seen[prefix] {
				seen[prefix] = true
				routes = append(routes, prefix)
			}
		}
	}
	return routes
}

func peerWithPathProbeEndpoints(peer clientapi.Peer, excludedRoutes []netip.Prefix) clientapi.Peer {
	filtered := peer
	filtered.Endpoint = ""
	filtered.EndpointCandidates = nil
	for _, endpoint := range orderedPeerEndpointCandidates(peer) {
		addr, ok := endpointAddr(endpoint)
		if ok {
			excluded := false
			for _, route := range excludedRoutes {
				if route.Addr().Is6() == addr.Is6() && route.Contains(addr) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}
		filtered.EndpointCandidates = append(filtered.EndpointCandidates, endpoint)
	}
	return filtered
}

func triggerWireGuardHandshake(target string) {
	addr, err := netip.ParseAddr(strings.TrimSpace(target))
	if err != nil {
		return
	}
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, 9)))
	if err != nil {
		return
	}
	_, _ = conn.Write([]byte{0})
	_ = conn.Close()
}

func wireGuardPathMapKey(networkMap clientapi.RegisterNodeResponse) string {
	return strings.Join([]string{
		strings.TrimSpace(networkMap.Network.ID),
		strconv.FormatUint(networkMap.Network.Revision, 10),
		strings.TrimSpace(networkMap.Node.ID),
	}, "\x00")
}

func wireGuardEngineUAPI(privateKey string, networkMap clientapi.RegisterNodeResponse, listenPort int, configured bool, firewallMark uint32, endpointOverrides map[string]string) (string, error) {
	return wireGuardEngineUAPIWithEndpoints(privateKey, networkMap, listenPort, configured, firewallMark, endpointOverrides, true)
}

func wireGuardEngineUAPIWithoutEndpoints(privateKey string, networkMap clientapi.RegisterNodeResponse, listenPort int, configured bool, firewallMark uint32) (string, error) {
	return wireGuardEngineUAPIWithEndpoints(privateKey, networkMap, listenPort, configured, firewallMark, nil, false)
}

func wireGuardEngineUAPIWithEndpoints(privateKey string, networkMap clientapi.RegisterNodeResponse, listenPort int, configured bool, firewallMark uint32, endpointOverrides map[string]string, emitEndpoints bool) (string, error) {
	privateHex, err := wireGuardKeyToHex(privateKey, false)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", privateHex)
	if !configured {
		fmt.Fprintf(&b, "listen_port=%d\n", listenPort)
	}
	fmt.Fprintf(&b, "fwmark=%d\n", firewallMark)
	fmt.Fprintln(&b, "replace_peers=true")
	interfaces := LocalInterfaceStatuses()
	for _, peer := range networkMap.Peers {
		publicHex, err := wireGuardKeyToHex(peer.PublicKey, true)
		if err != nil {
			return "", fmt.Errorf("peer %s public key: %w", firstNonEmptyString(peer.Hostname, peer.ID), err)
		}
		fmt.Fprintf(&b, "public_key=%s\n", publicHex)
		fmt.Fprintln(&b, "replace_allowed_ips=true")
		for _, allowedIP := range peer.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", strings.TrimSpace(allowedIP))
		}
		if emitEndpoints {
			endpoint := strings.TrimSpace(endpointOverrides[peer.ID])
			if endpoint == "" {
				endpoint = preferredPeerEndpoint(peer, interfaces)
			}
			if endpoint != "" {
				fmt.Fprintf(&b, "endpoint=%s\n", endpoint)
				fmt.Fprintln(&b, "persistent_keepalive_interval=25")
			}
		}
	}
	fmt.Fprintln(&b)
	return b.String(), nil
}

func wireGuardKeyToHex(value string, public bool) (string, error) {
	var err error
	if public {
		err = wgkeys.ValidatePublicKey(value)
	} else {
		err = wgkeys.ValidatePrivateKey(value)
	}
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func wireGuardHexToKey(value string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != 32 {
		return "", errors.New("wireguard UAPI key is not 32 bytes of hex")
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func parseWireGuardEngineIPC(inspection *WireGuardInspection, raw string) error {
	var peer *WireGuardPeerInspection
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "listen_port":
			inspection.ListenPort, _ = strconv.Atoi(value)
		case "public_key":
			encoded, err := wireGuardHexToKey(value)
			if err != nil {
				return err
			}
			inspection.Peers = append(inspection.Peers, WireGuardPeerInspection{PublicKey: encoded, AllowedIPs: []string{}})
			peer = &inspection.Peers[len(inspection.Peers)-1]
		case "endpoint":
			if peer != nil {
				peer.Endpoint = value
			}
		case "allowed_ip":
			if peer != nil {
				peer.AllowedIPs = append(peer.AllowedIPs, value)
			}
		case "last_handshake_time_sec":
			if peer != nil {
				peer.LatestHandshakeUnix, _ = strconv.ParseInt(value, 10, 64)
			}
		case "rx_bytes":
			if peer != nil {
				peer.TransferRXBytes, _ = strconv.ParseUint(value, 10, 64)
			}
		case "tx_bytes":
			if peer != nil {
				peer.TransferTXBytes, _ = strconv.ParseUint(value, 10, 64)
			}
		case "persistent_keepalive_interval":
			if peer != nil {
				peer.PersistentKeepaliveSeconds, _ = strconv.Atoi(value)
			}
		}
	}
	inspection.PeerCount = len(inspection.Peers)
	return nil
}

func WireGuardRouteTargetsForPrefixes(prefixes []netip.Prefix) []string {
	targets := make([]string, 0, len(prefixes))
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		prefix = prefix.Masked()
		var target netip.Addr
		if prefix.Bits() == prefix.Addr().BitLen() {
			target = prefix.Addr()
		} else {
			target = prefix.Addr().Next()
		}
		if !target.IsValid() || seen[target.String()] {
			continue
		}
		seen[target.String()] = true
		targets = append(targets, target.String())
	}
	sort.Strings(targets)
	return targets
}

func (e *WireGuardEngine) STUNSnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, timeout time.Duration) AgentSTUNSnapshot {
	discovery, err := e.DiscoverEndpoints(ctx, networkMap.STUNEndpoints, timeout)
	if err != nil {
		nat := ClassifySTUN(nil)
		return AgentSTUNSnapshot{Results: []STUNCheckResult{}, NAT: nat, Error: err.Error()}
	}
	nat := ClassifySTUN(discovery.Results)
	return AgentSTUNSnapshot{
		OK:           nat.ReachableEndpoints > 0,
		Results:      discovery.Results,
		NAT:          nat,
		PortMappings: discovery.PortMappings,
		Error: func() string {
			if nat.ReachableEndpoints == 0 {
				return nat.Error
			}
			return ""
		}(),
	}
}
