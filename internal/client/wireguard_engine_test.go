package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
	"golang.org/x/crypto/curve25519"
)

type testWireGuardEngineRouter struct {
	configured    []wireGuardEngineRouterConfig
	down          int
	configureCall int
	failNext      error
	failAt        map[int]error
}

func TestWireGuardGoConnectsRelayBridgeToMagicBind(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	relayAddr := freeTCPAddrForRelayPathTest(t)
	serverCtx, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	server := newRelayPathTestServer(t, relayAddr, publicKey, serverTLS)
	go func() {
		serverDone <- server.ListenAndServe(serverCtx)
	}()
	waitTCPForRelayPathTest(t, relayAddr)
	defer func() {
		cancelServer()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Errorf("relay server stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("relay server did not stop")
		}
	}()

	credential, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
		Node: clientapi.Node{
			ID:         "node-a",
			NetworkID:  "net-1",
			Hostname:   "node-a",
			PublicKey:  testWireGuardEnginePublicKey(1),
			AssignedIP: "100.64.0.2",
		},
		Peers: []clientapi.Peer{{
			ID:         "node-b",
			Hostname:   "node-b",
			PublicKey:  testWireGuardEnginePublicKey(2),
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
		Relays:          []relay.Endpoint{{ID: "relay-1", Addr: relayAddr, Protocol: "relay-v1-tls"}},
		RelayCredential: credential,
	}
	fakeTUN := tuntest.NewChannelTUN()
	injectRelayFailure := false
	relayApplyErr := errors.New("relay apply failed")
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface:      "endlessnet",
		RelayTimeout:   time.Second,
		RelayTLSConfig: clientTLS,
		tunFactory: func(string, int) (tun.Device, error) {
			return fakeTUN.TUN(), nil
		},
		router: &testWireGuardEngineRouter{},
		stageHook: func(stage wireGuardEngineApplyStage) error {
			if injectRelayFailure && stage == wireGuardEngineStageRelay {
				injectRelayFailure = false
				return relayApplyErr
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()

	configureCtx, cancelConfigure := context.WithCancel(context.Background())
	result, err := engine.Configure(configureCtx, Config{PrivateKey: testWireGuardEngineKey(1)}, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	cancelConfigure()
	time.Sleep(50 * time.Millisecond)
	if !result.OK || result.Method != "wireguard-go" {
		t.Fatalf("configure result = %#v", result)
	}
	status, ok, statusErr := engine.RelayStatus()
	if statusErr != nil || !ok || status.Relay.Selected == nil || status.PeerEndpoints["node-b"] == "" {
		t.Fatalf("relay status = %#v ok=%t err=%v", status, ok, statusErr)
	}
	inspection := engine.Inspection()
	if len(inspection.Peers) != 1 || inspection.Peers[0].Endpoint != status.PeerEndpoints["node-b"] {
		t.Fatalf("wireguard-go inspection = %#v, relay endpoint = %q", inspection, status.PeerEndpoints["node-b"])
	}
	updatedMap := cloneRegisterNodeResponse(networkMap)
	updatedMap.Network.Revision++
	injectRelayFailure = true
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: testWireGuardEngineKey(1)}, updatedMap); !errors.Is(err, relayApplyErr) {
		t.Fatalf("relay reconfigure error = %v", err)
	}
	restoredStatus, restoredOK, restoredErr := engine.RelayStatus()
	if restoredErr != nil || !restoredOK || engine.pathMap.Network.Revision != networkMap.Network.Revision {
		t.Fatalf("restored relay status = %#v ok=%t err=%v revision=%d", restoredStatus, restoredOK, restoredErr, engine.pathMap.Network.Revision)
	}
	restoredInspection := engine.Inspection()
	if len(restoredInspection.Peers) != 1 || restoredInspection.Peers[0].Endpoint != restoredStatus.PeerEndpoints["node-b"] {
		t.Fatalf("restored wireguard-go inspection = %#v, relay endpoint = %q", restoredInspection, restoredStatus.PeerEndpoints["node-b"])
	}
}

func TestWireGuardGoPromotesAuthenticatedDirectPathWithoutDroppingRelayBootstrap(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	relayAddr := freeTCPAddrForRelayPathTest(t)
	serverCtx, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	server := newRelayPathTestServer(t, relayAddr, publicKey, serverTLS)
	go func() {
		serverDone <- server.ListenAndServe(serverCtx)
	}()
	waitTCPForRelayPathTest(t, relayAddr)
	defer func() {
		cancelServer()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Errorf("relay server stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("relay server did not stop")
		}
	}()

	credentialA, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	credentialB, err := relay.Sign(privateKey, "net-1", "node-b", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	privateA, publicA := testWireGuardEngineKey(31), testWireGuardEnginePublicKey(31)
	privateB, publicB := testWireGuardEngineKey(32), testWireGuardEnginePublicKey(32)
	mapA := clientapi.RegisterNodeResponse{
		Network:         clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
		Node:            clientapi.Node{ID: "node-a", NetworkID: "net-1", Hostname: "node-a", PublicKey: publicA, AssignedIP: "100.64.0.2"},
		Peers:           []clientapi.Peer{{ID: "node-b", Hostname: "node-b", PublicKey: publicB, AllowedIPs: []string{"100.64.0.3/32"}}},
		Relays:          []relay.Endpoint{{ID: "relay-1", Addr: relayAddr, Protocol: "relay-v1-tls"}},
		RelayCredential: credentialA,
	}
	mapB := clientapi.RegisterNodeResponse{
		Network:         clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
		Node:            clientapi.Node{ID: "node-b", NetworkID: "net-1", Hostname: "node-b", PublicKey: publicB, AssignedIP: "100.64.0.3"},
		Peers:           []clientapi.Peer{{ID: "node-a", Hostname: "node-a", PublicKey: publicA, AllowedIPs: []string{"100.64.0.2/32"}}},
		Relays:          []relay.Endpoint{{ID: "relay-1", Addr: relayAddr, Protocol: "relay-v1-tls"}},
		RelayCredential: credentialB,
	}
	newEngine := func() *WireGuardEngine {
		fakeTUN := tuntest.NewChannelTUN()
		engine, engineErr := NewWireGuardEngine(WireGuardEngineOptions{
			Interface:        "endlessnet",
			RelayTimeout:     time.Second,
			RelayTLSConfig:   clientTLS,
			RelayDirectRetry: 100 * time.Millisecond,
			tunFactory: func(string, int) (tun.Device, error) {
				return fakeTUN.TUN(), nil
			},
			router: &testWireGuardEngineRouter{},
		})
		if engineErr != nil {
			t.Fatal(engineErr)
		}
		return engine
	}
	engineA, engineB := newEngine(), newEngine()
	defer func() { _ = engineA.Close() }()
	defer func() { _ = engineB.Close() }()
	if _, err := engineA.Configure(context.Background(), Config{PrivateKey: privateA}, mapA); err != nil {
		t.Fatal(err)
	}
	if _, err := engineB.Configure(context.Background(), Config{PrivateKey: privateB}, mapB); err != nil {
		t.Fatal(err)
	}
	if status := engineA.PathStatus(); len(status) != 1 || status[0].SelectedPath != "relay" {
		t.Fatalf("initial A path status = %#v", status)
	}
	if status := engineB.PathStatus(); len(status) != 1 || status[0].SelectedPath != "relay" {
		t.Fatalf("initial B path status = %#v", status)
	}

	endpointA := net.JoinHostPort("127.0.0.1", strconv.Itoa(engineA.Inspection().ListenPort))
	endpointB := net.JoinHostPort("127.0.0.1", strconv.Itoa(engineB.Inspection().ListenPort))
	mapA.Network.Revision = 2
	mapA.Peers[0].Endpoint = endpointB
	mapA.Peers[0].EndpointCandidates = []string{endpointB}
	mapB.Network.Revision = 2
	mapB.Peers[0].Endpoint = endpointA
	mapB.Peers[0].EndpointCandidates = []string{endpointA}
	if _, err := engineA.Configure(context.Background(), Config{PrivateKey: privateA}, mapA); err != nil {
		t.Fatal(err)
	}
	if _, err := engineB.Configure(context.Background(), Config{PrivateKey: privateB}, mapB); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		statusA, statusB := engineA.PathStatus(), engineB.PathStatus()
		if len(statusA) == 1 && len(statusB) == 1 && statusA[0].SelectedPath == "direct" && statusB[0].SelectedPath == "direct" {
			if statusA[0].SelectedEndpoint != endpointB || statusB[0].SelectedEndpoint != endpointA || statusA[0].Direct.RTTMS <= 0 || statusB[0].Direct.RTTMS <= 0 {
				t.Fatalf("direct path statuses = %#v / %#v", statusA, statusB)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("direct paths were not promoted: A=%#v B=%#v", engineA.PathStatus(), engineB.PathStatus())
}

func (r *testWireGuardEngineRouter) Configure(_ context.Context, cfg wireGuardEngineRouterConfig) error {
	r.configureCall++
	if r.failNext != nil {
		err := r.failNext
		r.failNext = nil
		return err
	}
	if err := r.failAt[r.configureCall]; err != nil {
		return err
	}
	r.configured = append(r.configured, cfg)
	return nil
}

func (r *testWireGuardEngineRouter) Down(context.Context) error {
	r.down++
	return nil
}

func TestWireGuardEngineOwnsSharedSTUNSocket(t *testing.T) {
	privateKey := testWireGuardEngineKey(1)
	peerKey := testWireGuardEnginePublicKey(2)
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", DNS: []string{"1.1.1.1"}},
		Node: clientapi.Node{
			ID:         "node-1",
			NetworkID:  "net-1",
			Hostname:   "node-1",
			PublicKey:  testWireGuardEnginePublicKey(1),
			AssignedIP: "100.64.0.2",
		},
		Peers: []clientapi.Peer{{
			ID:         "node-2",
			Hostname:   "node-2",
			PublicKey:  peerKey,
			Endpoint:   "127.0.0.1:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	fakeTUN := tuntest.NewChannelTUN()
	router := &testWireGuardEngineRouter{}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) {
			return fakeTUN.TUN(), nil
		},
		router: router,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()

	result, err := engine.Configure(context.Background(), Config{PrivateKey: privateKey}, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Method != "wireguard-go" || len(router.configured) != 1 {
		t.Fatalf("configure result/router = %#v / %#v", result, router.configured)
	}
	inspection := engine.Inspection()
	if !inspection.OK || inspection.ListenPort == 0 || inspection.PeerCount != 1 || inspection.Peers[0].PublicKey != peerKey {
		t.Fatalf("inspection = %#v", inspection)
	}
	unchanged, err := engine.Configure(context.Background(), Config{PrivateKey: privateKey}, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.OK || !unchanged.Skipped || unchanged.Changed || len(router.configured) != 1 {
		t.Fatalf("unchanged configure result/router = %#v / %#v", unchanged, router.configured)
	}

	stunServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stunServer.Close() }()
	go func() {
		buf := make([]byte, 2048)
		n, remote, readErr := stunServer.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		response, buildErr := buildSTUNBindingResponseForTest(buf[:n], remote)
		if buildErr == nil {
			_, _ = stunServer.WriteToUDP(response, remote)
		}
	}()
	discovery, err := engine.DiscoverEndpoints(context.Background(), []clientapi.STUNEndpoint{{ID: "local", Addr: stunServer.LocalAddr().String()}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Port != inspection.ListenPort || len(discovery.Candidates) == 0 {
		t.Fatalf("endpoint discovery = %#v, inspection port = %d", discovery, inspection.ListenPort)
	}
	mapped, err := netip.ParseAddrPort(discovery.Candidates[0])
	if err != nil || int(mapped.Port()) != inspection.ListenPort {
		t.Fatalf("mapped endpoint = %q, %v; want port %d", discovery.Candidates[0], err, inspection.ListenPort)
	}
	down, err := engine.Down(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !down.OK || down.Method != "wireguard-go" || !down.Changed || router.down != 1 {
		t.Fatalf("down result/router = %#v / %#v", down, router)
	}
}

func TestWireGuardEngineRollsBackFailedReconfigure(t *testing.T) {
	privateKey := testWireGuardEngineKey(41)
	networkMap := testWireGuardEngineNetworkMap(41, 42)
	router := &testWireGuardEngineRouter{}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) {
			fakeTUN := tuntest.NewChannelTUN()
			return fakeTUN.TUN(), nil
		},
		router: router,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: privateKey}, networkMap); err != nil {
		t.Fatal(err)
	}

	updated := networkMap
	updated.Network.Revision = 2
	updated.Peers = append([]clientapi.Peer(nil), networkMap.Peers...)
	updated.Peers[0].AllowedIPs = []string{"100.64.0.4/32"}
	router.failNext = errors.New("route update failed")
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: privateKey}, updated); err == nil || !strings.Contains(err.Error(), "route update failed") {
		t.Fatalf("failed reconfigure error = %v", err)
	}
	if !engine.configured || engine.pathMap.Network.Revision != networkMap.Network.Revision || !wireGuardEngineRouterConfigsEqual(engine.routerCfg, router.configured[0]) {
		t.Fatalf("engine did not restore previous state: configured=%t revision=%d router=%#v", engine.configured, engine.pathMap.Network.Revision, engine.routerCfg)
	}
	inspection := engine.Inspection()
	if !inspection.OK || len(inspection.Peers) != 1 || len(inspection.Peers[0].AllowedIPs) != 1 || inspection.Peers[0].AllowedIPs[0] != "100.64.0.3/32" {
		t.Fatalf("restored inspection = %#v", inspection)
	}
}

func TestWireGuardEngineRestoresPreviousRuntimeAfterEveryApplyStageFailure(t *testing.T) {
	stages := []wireGuardEngineApplyStage{
		wireGuardEngineStageStart,
		wireGuardEngineStagePathProbe,
		wireGuardEngineStageInitialUAPI,
		wireGuardEngineStageDeviceUp,
		wireGuardEngineStageRelay,
		wireGuardEngineStageRoutes,
		wireGuardEngineStageDesiredUAPI,
	}
	for _, stage := range stages {
		t.Run(string(stage), func(t *testing.T) {
			injectedErr := errors.New("injected apply failure")
			inject := false
			router := &testWireGuardEngineRouter{}
			engine, err := NewWireGuardEngine(WireGuardEngineOptions{
				Interface: "endlessnet",
				tunFactory: func(string, int) (tun.Device, error) {
					fakeTUN := tuntest.NewChannelTUN()
					return fakeTUN.TUN(), nil
				},
				router: router,
				stageHook: func(current wireGuardEngineApplyStage) error {
					if inject && current == stage {
						inject = false
						return injectedErr
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = engine.Close() }()

			privateKey := testWireGuardEngineKey(71)
			networkMap := testWireGuardEngineNetworkMap(71, 72)
			originalConfig := Config{PrivateKey: privateKey, WireGuardMTU: 1280}
			if _, err := engine.Configure(context.Background(), originalConfig, networkMap); err != nil {
				t.Fatal(err)
			}
			previousUAPI := engine.uapi
			previousRouterCfg := cloneWireGuardEngineRouterConfig(engine.routerCfg)
			previousPathStatuses := engine.PathStatus()
			previousPathProbe := snapshotMagicBindPathProbing(engine.bind)
			previousListenPort := engine.Inspection().ListenPort

			updated := cloneRegisterNodeResponse(networkMap)
			updated.Network.Revision++
			updated.Peers[0].AllowedIPs = []string{"100.64.0.4/32"}
			inject = true
			_, err = engine.Configure(context.Background(), Config{PrivateKey: privateKey, WireGuardMTU: 1300}, updated)
			if !errors.Is(err, injectedErr) {
				t.Fatalf("configure error = %v, want injected failure", err)
			}
			if inject {
				t.Fatalf("apply stage %q was not reached", stage)
			}
			if !engine.configured || engine.device == nil || engine.bind == nil || engine.pathCancel == nil {
				t.Fatalf("previous runtime was not restored: configured=%t device=%v bind=%v monitor=%v", engine.configured, engine.device, engine.bind, engine.pathCancel)
			}
			if engine.opts.MTU != originalConfig.WireGuardMTU || engine.pathMap.Network.Revision != networkMap.Network.Revision || engine.uapi != previousUAPI {
				t.Fatalf("restored logical state = mtu:%d revision:%d uapi_equal:%t", engine.opts.MTU, engine.pathMap.Network.Revision, engine.uapi == previousUAPI)
			}
			if !wireGuardEngineRouterConfigsEqual(engine.routerCfg, previousRouterCfg) {
				t.Fatalf("restored router config = %#v, want %#v", engine.routerCfg, previousRouterCfg)
			}
			if got := snapshotMagicBindPathProbing(engine.bind); !magicBindPathProbeSnapshotsEqual(got, previousPathProbe) {
				t.Fatalf("path-probe state was not restored (enabled=%t keys=%d, want enabled=%t keys=%d)", got.enabled, len(got.keys), previousPathProbe.enabled, len(previousPathProbe.keys))
			}
			if got := engine.PathStatus(); len(got) != len(previousPathStatuses) || len(got) > 0 && (got[0].SelectedPath != previousPathStatuses[0].SelectedPath || got[0].SelectedEndpoint != previousPathStatuses[0].SelectedEndpoint) {
				t.Fatalf("restored path selection = %#v, want %#v", got, previousPathStatuses)
			}
			inspection := engine.Inspection()
			if !inspection.OK || inspection.ListenPort != previousListenPort || len(inspection.Peers) != 1 || len(inspection.Peers[0].AllowedIPs) != 1 || inspection.Peers[0].AllowedIPs[0] != "100.64.0.3/32" {
				t.Fatalf("restored inspection = %#v", inspection)
			}
		})
	}
}

func TestWireGuardEngineJoinsApplyAndRollbackErrors(t *testing.T) {
	applyErr := errors.New("desired UAPI apply failed")
	rollbackErr := errors.New("route rollback failed")
	inject := false
	router := &testWireGuardEngineRouter{failAt: map[int]error{3: rollbackErr}}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) {
			fakeTUN := tuntest.NewChannelTUN()
			return fakeTUN.TUN(), nil
		},
		router: router,
		stageHook: func(stage wireGuardEngineApplyStage) error {
			if inject && stage == wireGuardEngineStageDesiredUAPI {
				inject = false
				return applyErr
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	privateKey := testWireGuardEngineKey(81)
	networkMap := testWireGuardEngineNetworkMap(81, 82)
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: privateKey}, networkMap); err != nil {
		t.Fatal(err)
	}
	updated := cloneRegisterNodeResponse(networkMap)
	updated.Network.Revision++
	updated.Peers[0].AllowedIPs = []string{"100.64.0.4/32"}
	inject = true
	_, err = engine.Configure(context.Background(), Config{PrivateKey: privateKey}, updated)
	if !errors.Is(err, applyErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("joined error = %v, want apply and rollback causes", err)
	}
	if !strings.Contains(err.Error(), "rollback wireguard-go configuration") {
		t.Fatalf("joined error does not identify rollback: %v", err)
	}
	if engine.configured || engine.device != nil || engine.bind != nil {
		t.Fatalf("failed rollback left an uncertain runtime active: configured=%t device=%v bind=%v", engine.configured, engine.device, engine.bind)
	}
}

func TestWireGuardEnginePreflightFailureKeepsLiveDevice(t *testing.T) {
	router := &testWireGuardEngineRouter{}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) {
			fakeTUN := tuntest.NewChannelTUN()
			return fakeTUN.TUN(), nil
		},
		router: router,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	networkMap := testWireGuardEngineNetworkMap(61, 62)
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: testWireGuardEngineKey(61)}, networkMap); err != nil {
		t.Fatal(err)
	}
	deviceBefore, bindBefore, downBefore := engine.device, engine.bind, router.down
	if _, err := engine.Configure(context.Background(), Config{}, networkMap); err == nil || !strings.Contains(err.Error(), "private key is missing") {
		t.Fatalf("invalid reconfigure error = %v", err)
	}
	if engine.device != deviceBefore || engine.bind != bindBefore || router.down != downBefore || !engine.configured || engine.pathMap.Network.Revision != networkMap.Network.Revision {
		t.Fatalf("preflight failure restarted live engine: device=%p/%p bind=%p/%p down=%d/%d configured=%t", engine.device, deviceBefore, engine.bind, bindBefore, router.down, downBefore, engine.configured)
	}
	if _, err := engine.Configure(context.Background(), Config{PrivateKey: testWireGuardEngineKey(63)}, networkMap); err == nil || !strings.Contains(err.Error(), "does not match node public key") {
		t.Fatalf("path-probe preflight error = %v", err)
	}
	if engine.device != deviceBefore || engine.bind != bindBefore || router.down != downBefore || !engine.configured {
		t.Fatalf("path-probe preflight failure restarted live engine: device=%p/%p bind=%p/%p down=%d/%d configured=%t", engine.device, deviceBefore, engine.bind, bindBefore, router.down, downBefore, engine.configured)
	}
}

func TestWireGuardEngineCleansUpFailedInitialConfigure(t *testing.T) {
	router := &testWireGuardEngineRouter{failNext: errors.New("initial route failure")}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{
		Interface: "endlessnet",
		tunFactory: func(string, int) (tun.Device, error) {
			fakeTUN := tuntest.NewChannelTUN()
			return fakeTUN.TUN(), nil
		},
		router: router,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	networkMap := testWireGuardEngineNetworkMap(51, 52)
	cfg := Config{PrivateKey: testWireGuardEngineKey(51)}
	if _, err := engine.Configure(context.Background(), cfg, networkMap); err == nil {
		t.Fatal("initial configure unexpectedly succeeded")
	}
	if engine.configured || engine.device != nil || engine.bind != nil || engine.router != nil {
		t.Fatalf("failed initial configure leaked engine state: configured=%t device=%v bind=%v router=%v", engine.configured, engine.device, engine.bind, engine.router)
	}
	if _, err := engine.Configure(context.Background(), cfg, networkMap); err != nil {
		t.Fatalf("retry after failed initial configure: %v", err)
	}
}

func testWireGuardEngineNetworkMap(localKey, peerKey byte) clientapi.RegisterNodeResponse {
	return clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
		Node: clientapi.Node{
			ID:         "node-1",
			NetworkID:  "net-1",
			Hostname:   "node-1",
			PublicKey:  testWireGuardEnginePublicKey(localKey),
			AssignedIP: "100.64.0.2",
		},
		Peers: []clientapi.Peer{{
			ID:         "node-2",
			Hostname:   "node-2",
			PublicKey:  testWireGuardEnginePublicKey(peerKey),
			Endpoint:   "127.0.0.1:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
}

func magicBindPathProbeSnapshotsEqual(left, right magicBindPathProbeSnapshot) bool {
	if left.enabled != right.enabled || left.localPublic != right.localPublic || len(left.keys) != len(right.keys) {
		return false
	}
	for publicKey, sharedKey := range left.keys {
		if right.keys[publicKey] != sharedKey {
			return false
		}
	}
	return true
}

func TestBuildWireGuardEngineRouterConfig(t *testing.T) {
	cfg, err := buildWireGuardEngineRouterConfig("endlessnet", 1280, Config{}, clientapi.RegisterNodeResponse{
		Node:    clientapi.Node{AssignedIP: "100.64.0.2", AssignedIPv6: "fd7a:115c:a1e0::2"},
		Network: clientapi.Network{DNS: []string{"100.64.0.1"}},
		Peers: []clientapi.Peer{{
			AllowedIPs:    []string{"100.64.0.3/32", "10.0.0.0/8"},
			AllowedPorts:  []clientapi.ACLPort{{Protocol: "tcp", Port: 443}},
			ACLRestricted: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Addresses) != 2 || len(cfg.Routes) != 2 || len(cfg.DNS) != 1 || cfg.MTU != 1280 {
		t.Fatalf("router config = %#v", cfg)
	}
	if strings.Contains(strings.Join(append(cfg.PostUp, cfg.PreDown...), "\n"), "ENACL-") {
		t.Fatal("userspace peer ACL must not depend on OS firewall hooks")
	}
	if strings.Contains(strings.Join(append(cfg.PostUp, cfg.PreDown...), "\n"), "%i") {
		t.Fatalf("userspace firewall hooks retain interface placeholder: %#v / %#v", cfg.PostUp, cfg.PreDown)
	}
}

func TestBuildWireGuardEngineRouterConfigRestoresEffectiveDNS(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Revision: clientapi.MapRevision{Network: 7},
		Node:     clientapi.Node{AssignedIP: "100.64.0.2"},
		Network: clientapi.Network{
			Name: "prod",
			DNS:  []string{"192.0.2.53"},
			DNSConfig: &clientapi.DNSConfig{
				MagicDNSEnabled: true, OverrideLocalDNS: false, Suffix: "prod.endlessnet",
				Nameservers: []clientapi.DNSNameserver{
					{ID: "global", Address: "192.0.2.53", Scope: "global", Priority: 100},
					{ID: "global-backup", Address: "192.0.2.54", Scope: "global", Priority: 110},
					{ID: "split", Address: "198.51.100.53", Scope: "split", Priority: 10, SplitDomains: []string{"corp.example"}},
					{ID: "split-backup", Address: "198.51.100.54", Scope: "split", Priority: 20, SplitDomains: []string{"corp.example"}},
				},
				SearchDomains: []string{"prod.endlessnet", "corp.example"},
			},
		},
	}
	cfg, err := buildWireGuardEngineRouterConfig("endlessnet", 1280, Config{}, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DNSConfigPresent || cfg.DNSOverride || len(cfg.DNS) != 1 || cfg.DNS[0].String() != "127.0.0.1" || cfg.DNSProxy == nil {
		t.Fatalf("effective DNS router config = %#v", cfg)
	}
	if !cfg.DNSProxy.ServePeerDNS || !slices.Equal(cfg.DNSProxy.UpstreamAddrs, []string{"192.0.2.53:53", "192.0.2.54:53"}) || cfg.DNSProxy.SearchDomain != "prod.endlessnet" || len(cfg.DNSProxy.SplitRules) != 1 || !reflect.DeepEqual(cfg.DNSProxy.SplitRules[0], SplitDNSRule{Domain: "corp.example", Upstreams: []string{"198.51.100.53:53", "198.51.100.54:53"}}) {
		t.Fatalf("DNS proxy config = %#v", cfg.DNSProxy)
	}
	if !slices.Equal(cfg.DNSDomains, []string{"corp.example", "prod.endlessnet"}) || !slices.Equal(cfg.SearchDomains, []string{"prod.endlessnet", "corp.example"}) {
		t.Fatalf("DNS domains = routes %#v search %#v", cfg.DNSDomains, cfg.SearchDomains)
	}
}

func TestPeerWithPathProbeEndpointsExcludesOverlayAndRoutedCandidates(t *testing.T) {
	peer := clientapi.Peer{
		EndpointCandidates: []string{
			"203.0.113.10:51820",
			"100.64.0.3:51820",
			"10.20.0.5:51820",
		},
	}
	filtered := peerWithPathProbeEndpoints(peer, []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/24"),
		netip.MustParsePrefix("10.20.0.0/16"),
	})
	if len(filtered.EndpointCandidates) != 1 || filtered.EndpointCandidates[0] != "203.0.113.10:51820" || filtered.Endpoint != "" {
		t.Fatalf("filtered path probe peer = %#v", filtered)
	}
}

func testWireGuardEngineKey(fill byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill
	}
	return base64.StdEncoding.EncodeToString(key)
}

func testWireGuardEnginePublicKey(fill byte) string {
	privateRaw, err := base64.StdEncoding.DecodeString(testWireGuardEngineKey(fill))
	if err != nil {
		panic(err)
	}
	publicRaw, err := curve25519.X25519(privateRaw, curve25519.Basepoint)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(publicRaw)
}
