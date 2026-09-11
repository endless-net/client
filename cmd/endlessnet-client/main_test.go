package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"

	relayauth "github.com/endless-net/relay/protocol/v1"
)

func testWireGuardPublicKey(label string) string {
	sum := sha256.Sum256([]byte(label))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func diagnosticsPayload(cfg client.Config) map[string]any {
	return diagnosticsPayloadWithAgentState(cfg, nil)
}

type testAgentWireGuard struct {
	configureCalls int
	downCalls      int
	configure      func(client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error)
	down           func() (client.WireGuardApplyResult, error)
}

func (w *testAgentWireGuard) Configure(_ context.Context, cfg client.Config, networkMap clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
	w.configureCalls++
	if w.configure != nil {
		return w.configure(cfg, networkMap)
	}
	return client.WireGuardApplyResult{OK: true, Method: "wireguard-go", Interface: "endlessnet", Changed: true}, nil
}

func (w *testAgentWireGuard) Down(context.Context) (client.WireGuardApplyResult, error) {
	w.downCalls++
	if w.down != nil {
		return w.down()
	}
	return client.WireGuardApplyResult{OK: true, Method: "wireguard-go", Interface: "endlessnet", Changed: true}, nil
}

func (*testAgentWireGuard) LastEndpointDiscovery() client.WireGuardEngineEndpointDiscovery {
	return client.WireGuardEngineEndpointDiscovery{}
}

func (*testAgentWireGuard) STUNSnapshot(context.Context, clientapi.RegisterNodeResponse, time.Duration) client.AgentSTUNSnapshot {
	return client.AgentSTUNSnapshot{}
}

func (*testAgentWireGuard) Inspection() client.WireGuardInspection {
	return client.WireGuardInspection{Interface: "endlessnet"}
}

func testSuccessfulIPCWireGuardApply() *ipc.WireGuardApplyResult {
	return &ipc.WireGuardApplyResult{OK: true, Method: "wireguard-go", Interface: "endlessnet", Changed: true}
}

func (*testAgentWireGuard) PathStatus() []client.PeerPathStatus { return nil }

func (*testAgentWireGuard) RelayStatus() (client.RelayDataplaneBridgeStatus, bool, error) {
	return client.RelayDataplaneBridgeStatus{}, false, nil
}

func setInstallationStateDirForTest(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("PROGRAMDATA", dir)
		return
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
}

func TestResolveNetworkFlagDefaultsToDefaultNetwork(t *testing.T) {
	if got := resolveNetworkFlag(""); got != defaultNetworkName {
		t.Fatalf("resolveNetworkFlag(\"\") = %q, want %q", got, defaultNetworkName)
	}
	if got := resolveNetworkFlag(" office "); got != "office" {
		t.Fatalf("resolveNetworkFlag trims value to %q", got)
	}
}

func TestCmdJoinTokenForwardsLifecycleOptions(t *testing.T) {
	var received clientapi.CreateJoinTokenRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/nodes/join-tokens" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			t.Fatalf("authorization = %q", got)
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&received); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(clientapi.CreateJoinTokenResponse{
			ID: "jtk_1", Token: "join-secret", NetworkID: received.NetworkID, ExpiresAt: time.Now().Add(time.Hour),
		})
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{Token: "session-token", ControlPlaneURLs: []string{server.URL}}); err != nil {
		t.Fatal(err)
	}
	if err := cmdJoinToken([]string{
		"create", "--config", configPath, "--network", "net_1", "--ttl", "2h",
		"--idempotency-key", "command-1", "--reusable", "--ephemeral", "--preauthorized",
		"--tag", "role:test", "--tag", "env:system",
	}); err != nil {
		t.Fatal(err)
	}
	if received.NetworkID != "net_1" || received.TTL != "2h" || !received.Reusable || !received.Ephemeral || !received.Preauthorized {
		t.Fatalf("join token request = %#v", received)
	}
	if got, want := strings.Join(received.Tags, ","), "role:test,env:system"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
}

func TestUpdateWireGuardMTUFromFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	mtu := fs.Int("mtu", 0, "")
	if err := fs.Parse([]string{"--mtu", "1280"}); err != nil {
		t.Fatal(err)
	}
	var cfg client.Config
	if err := updateWireGuardMTUFromFlag(fs, *mtu, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.WireGuardMTU != 1280 {
		t.Fatalf("wireguard mtu = %d, want 1280", cfg.WireGuardMTU)
	}

	invalid := flag.NewFlagSet("test-invalid", flag.ContinueOnError)
	invalidMTU := invalid.Int("mtu", 0, "")
	if err := invalid.Parse([]string{"--mtu", "1279"}); err != nil {
		t.Fatal(err)
	}
	if err := updateWireGuardMTUFromFlag(invalid, *invalidMTU, &cfg); err == nil || !strings.Contains(err.Error(), "wireguard_mtu") {
		t.Fatalf("invalid mtu error = %v, want wireguard_mtu validation", err)
	}
}

func TestSetNetworkRefKeepsIDsDistinctFromNames(t *testing.T) {
	var byName clientapi.RegisterNodeRequest
	setNetworkRef(&byName, "office")
	if byName.NetworkName != "office" || byName.NetworkID != "" {
		t.Fatalf("byName = %#v", byName)
	}

	var byID clientapi.RegisterNodeRequest
	setNetworkRef(&byID, "net_123")
	if byID.NetworkID != "net_123" || byID.NetworkName != "" {
		t.Fatalf("byID = %#v", byID)
	}
}

func TestMapStreamActionNamesResync(t *testing.T) {
	if got := mapStreamAction("snapshot"); got != "synced" {
		t.Fatalf("snapshot action = %q, want synced", got)
	}
	if got := mapStreamAction("resync"); got != "resynced" {
		t.Fatalf("resync action = %q, want resynced", got)
	}
}

func TestAllPeerPathsSelected(t *testing.T) {
	if !allPeerPathsSelected([]client.PeerPathStatus{{SelectedPath: "direct"}, {SelectedPath: "relay"}}) {
		t.Fatal("allPeerPathsSelected rejected selected paths")
	}
	if allPeerPathsSelected(nil) {
		t.Fatal("allPeerPathsSelected accepted empty paths")
	}
	if allPeerPathsSelected([]client.PeerPathStatus{{SelectedPath: "none"}}) {
		t.Fatal("allPeerPathsSelected accepted none")
	}
}

func TestRelayCheckPayloadFromMapReportsMissingRelayConfig(t *testing.T) {
	noRelay, err := relayCheckPayloadFromMap(clientapi.RegisterNodeResponse{}, time.Second, nil)
	if err == nil || noRelay.OK || noRelay.Error == "" || len(noRelay.Attempts) != 0 {
		t.Fatalf("no-relay payload = %#v, err=%v", noRelay, err)
	}
	missingCredential, err := relayCheckPayloadFromMap(clientapi.RegisterNodeResponse{
		Relays: []relayauth.Endpoint{{ID: "relay-1", Addr: "127.0.0.1:1", Protocol: relayauth.EndpointProtocolTLS}},
	}, time.Second, nil)
	if err == nil || missingCredential.OK || !strings.Contains(missingCredential.Error, "relay credential is missing") || len(missingCredential.Attempts) != 0 {
		t.Fatalf("missing-credential payload = %#v, err=%v", missingCredential, err)
	}
}

func TestCmdPathCheckProbeRTTRequiresInterface(t *testing.T) {
	err := cmdPathCheck([]string{"--probe-rtt"})
	if err == nil || !strings.Contains(err.Error(), "wg-interface is required") {
		t.Fatalf("cmdPathCheck error = %v, want wg-interface required", err)
	}
}

func TestCmdPingRequiresSingleTarget(t *testing.T) {
	err := cmdPing([]string{})
	if err == nil || !strings.Contains(err.Error(), "exactly one peer target") {
		t.Fatalf("cmdPing error = %v, want peer target error", err)
	}
}

func TestFindPeerByPingTargetMatchesHostnameIDAndOverlayIP(t *testing.T) {
	peers := []clientapi.Peer{{
		ID:         "peer-1",
		Hostname:   "peer-a",
		AllowedIPs: []string{"100.64.0.3/32", "fd7a:115c:a1e0::3/128", "10.10.0.0/24"},
	}, {
		ID:         "peer-2",
		Hostname:   "peer-b",
		AllowedIPs: []string{"100.64.0.4/32"},
	}}
	for _, target := range []string{"peer-1", "PEER-A", "100.64.0.3", "fd7a:115c:a1e0::3"} {
		peer, err := findPeerByPingTarget(peers, target)
		if err != nil {
			t.Fatalf("findPeerByPingTarget(%q): %v", target, err)
		}
		if peer.ID != "peer-1" {
			t.Fatalf("findPeerByPingTarget(%q) = %#v, want peer-1", target, peer)
		}
	}
	if _, err := findPeerByPingTarget(peers, "10.10.0.1"); err == nil {
		t.Fatal("findPeerByPingTarget matched a subnet host route target")
	}
}

func TestFindPeerByPingTargetReportsAmbiguousTargets(t *testing.T) {
	_, err := findPeerByPingTarget([]clientapi.Peer{
		{ID: "peer-1", Hostname: "same"},
		{ID: "peer-2", Hostname: "same"},
	}, "same")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("findPeerByPingTarget error = %v, want ambiguous", err)
	}
}

func TestPingPayloadReportsDirectAndRelayLatency(t *testing.T) {
	peer := clientapi.Peer{
		ID:         "peer-1",
		Hostname:   "peer-a",
		AllowedIPs: []string{"100.64.0.3/32"},
	}
	directPayload := pingPayloadFromPath("peer-a", peer, client.PeerPathStatus{
		PeerID:       "peer-1",
		Hostname:     "peer-a",
		SelectedPath: "direct",
		Direct: client.PathCandidateStatus{
			State:    "reachable",
			Endpoint: "peer.example.test:51820",
			RTTMS:    0.75,
		},
	}, client.RelayDialResult{}, nil, nil)
	if !directPayload.OK || directPayload.SelectedPath != "direct" || directPayload.LatencyMS != 0.75 || directPayload.Endpoint != "peer.example.test:51820" {
		t.Fatalf("direct ping payload = %#v", directPayload)
	}
	selectedRelay := relayauth.Endpoint{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}
	relayPayload := pingPayloadFromPath("peer-a", peer, client.PeerPathStatus{
		PeerID:       "peer-1",
		Hostname:     "peer-a",
		SelectedPath: "relay",
		Relay: client.PathCandidateStatus{
			State:    "reachable",
			Endpoint: "127.0.0.1:8443",
			RelayID:  "relay-1",
			Protocol: relayauth.EndpointProtocolTLS,
		},
	}, client.RelayDialResult{
		Selected: &selectedRelay,
		Duration: 42 * time.Millisecond,
	}, nil, nil)
	if !relayPayload.OK || relayPayload.SelectedPath != "relay" || relayPayload.LatencyMS != 42 || relayPayload.Relay.DurationMS != 42 || relayPayload.Endpoint != "127.0.0.1:8443" {
		t.Fatalf("relay ping payload = %#v", relayPayload)
	}
	tinyRelayPayload := pingPayloadFromPath("peer-a", peer, client.PeerPathStatus{
		PeerID:       "peer-1",
		Hostname:     "peer-a",
		SelectedPath: "relay",
		Relay: client.PathCandidateStatus{
			State:    "reachable",
			Endpoint: "127.0.0.1:8443",
		},
	}, client.RelayDialResult{
		Selected: &selectedRelay,
		Duration: time.Nanosecond,
	}, nil, nil)
	if tinyRelayPayload.LatencyMS <= 0 || tinyRelayPayload.Relay.DurationMS <= 0 {
		t.Fatalf("tiny relay ping payload = %#v, want positive durations", tinyRelayPayload)
	}
}

func TestCmdStatusProbeRTTRequiresInterface(t *testing.T) {
	err := cmdStatus([]string{"--probe-rtt"})
	if err == nil || !strings.Contains(err.Error(), "wg-interface is required") {
		t.Fatalf("cmdStatus error = %v, want wg-interface required", err)
	}
}

func TestCmdDiagnosticsProbeRTTRequiresInterface(t *testing.T) {
	err := cmdDiagnostics([]string{"--probe-rtt"})
	if err == nil || !strings.Contains(err.Error(), "wg-interface is required") {
		t.Fatalf("cmdDiagnostics error = %v, want wg-interface required", err)
	}
}

func TestRouteConflictErrorNamesOverlayLocalPrefixAndInterface(t *testing.T) {
	err := routeConflictError([]client.OverlayCIDRConflict{{
		OverlayCIDR: "100.64.0.0/24",
		LocalPrefix: "100.64.0.0/16",
		Interface:   "eth0",
	}})
	if err == nil || !strings.Contains(err.Error(), "100.64.0.0/24") || !strings.Contains(err.Error(), "100.64.0.0/16") || !strings.Contains(err.Error(), "eth0") {
		t.Fatalf("route conflict error = %v", err)
	}
}

func TestCmdAgentProbeRTTRequiresInterface(t *testing.T) {
	err := cmdAgent([]string{"--probe-rtt", "--once"})
	if err == nil || !strings.Contains(err.Error(), "wg-interface is required") {
		t.Fatalf("cmdAgent error = %v, want wg-interface required", err)
	}
}

func TestJSONCommandErrorsHaveStableCodes(t *testing.T) {
	tests := []struct {
		name    string
		run     func() error
		want    string
		message string
	}{
		{
			name:    "status probe-rtt requires interface",
			run:     func() error { return cmdStatus([]string{"--json", "--probe-rtt"}) },
			want:    cliErrorWGInterfaceRequired,
			message: "wg-interface is required",
		},
		{
			name:    "diagnostics probe-rtt requires interface",
			run:     func() error { return cmdDiagnostics([]string{"--probe-rtt"}) },
			want:    cliErrorWGInterfaceRequired,
			message: "wg-interface is required",
		},
		{
			name:    "relay-check invalid timeout",
			run:     func() error { return cmdRelayCheck([]string{"--json", "--timeout", "0s"}) },
			want:    cliErrorInvalidTimeout,
			message: "timeout must be a positive duration",
		},
		{
			name:    "relay-check invalid watch duration",
			run:     func() error { return cmdRelayCheck([]string{"--json", "--watch-duration", "0s"}) },
			want:    cliErrorInvalidTimeout,
			message: "watch-duration must be a positive duration",
		},
		{
			name: "relay-check invalid heartbeat interval",
			run: func() error {
				return cmdRelayCheck([]string{"--json", "--watch-duration", "1s", "--heartbeat-interval", "0s"})
			},
			want:    cliErrorInvalidTimeout,
			message: "heartbeat-interval must be a positive duration",
		},
		{
			name:    "path-check invalid timeout",
			run:     func() error { return cmdPathCheck([]string{"--json", "--timeout", "0s"}) },
			want:    cliErrorInvalidTimeout,
			message: "timeout must be a positive duration",
		},
		{
			name:    "ping missing target",
			run:     func() error { return cmdPing([]string{"--json"}) },
			want:    cliErrorInvalidArguments,
			message: "exactly one peer target",
		},
		{
			name:    "netcheck invalid timeout",
			run:     func() error { return cmdNetcheck([]string{"--json", "--timeout", "0s"}) },
			want:    cliErrorInvalidTimeout,
			message: "timeout must be a positive duration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := captureStdout(t, tt.run)
			if err == nil {
				t.Fatalf("%s unexpectedly succeeded\n%s", tt.name, out)
			}
			var payload struct {
				OK        bool   `json:"ok"`
				ErrorCode string `json:"error_code"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("decode JSON error payload: %v\n%s", err, out)
			}
			if payload.OK || payload.ErrorCode != tt.want || !strings.Contains(payload.Error, tt.message) {
				t.Fatalf("JSON error payload = %#v, want code %s and message containing %q", payload, tt.want, tt.message)
			}
		})
	}
}

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	runErr := run()
	_ = write.Close()
	os.Stdout = original
	raw, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(raw), runErr
}

func TestCmdVersionIncludesTargetMetadata(t *testing.T) {
	out, err := captureStdout(t, cmdVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"endlessnet-client",
		"commit:",
		"built:",
		"target: " + runtime.GOOS + "/" + runtime.GOARCH,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cmdVersion output missing %q:\n%s", want, out)
		}
	}
}

func TestFriendlyCLIExecutable(t *testing.T) {
	for _, path := range []string{"endlessnet", "/usr/bin/endlessnet", `C:\Program Files\EndlessNet\endlessnet.exe`} {
		if !isFriendlyCLIExecutable(path) {
			t.Fatalf("isFriendlyCLIExecutable(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"endlessnet-client", "/opt/endlessnet/bin/endlessnet-client", "endlessnet-client.exe"} {
		if isFriendlyCLIExecutable(path) {
			t.Fatalf("isFriendlyCLIExecutable(%q) = true, want false", path)
		}
	}
}

func TestCmdManagedUpUsesProductionDefaults(t *testing.T) {
	var got ipc.EnrollRequest
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			got = req
			return ipc.EnrollResponse{
				StatusResponse: ipc.StatusResponse{
					State: ipc.StateConnected, ControlState: ipc.ControlStateReady, OverlayIP: "100.64.0.2",
				},
				WireGuardApply: testSuccessfulIPCWireGuardApply(),
			}, nil
		},
	}))
	defer server.Close()
	original := newServiceIPCClient
	newServiceIPCClient = func(pipe string) *ipc.Client {
		if pipe != "test-pipe" {
			t.Fatalf("service IPC pipe = %q, want test-pipe", pipe)
		}
		ipc := ipc.NewClient(server.Client())
		ipc.BaseURL = server.URL
		return ipc
	}
	defer func() { newServiceIPCClient = original }()

	out, err := captureStdout(t, func() error {
		return cmdManagedUp([]string{"--ipc-pipe", "test-pipe", "--approval-timeout", "2s"})
	})
	if err != nil {
		t.Fatalf("cmdManagedUp failed: %v\n%s", err, out)
	}
	if got.Server != defaultPublicServerURL || got.Mode != "server" || strings.TrimSpace(got.Hostname) == "" {
		t.Fatalf("managed up request = %#v", got)
	}
	if got.EnrollToken != "" {
		t.Fatalf("managed up unexpectedly sent an enrollment token: %#v", got)
	}
	if !strings.Contains(out, "EndlessNet connected.") || !strings.Contains(out, "100.64.0.2") {
		t.Fatalf("managed up output = %q", out)
	}
	if strings.Contains(out, "Warning:") {
		t.Fatalf("healthy managed up output contains warning: %q", out)
	}
}

func TestRunManagedUpWaitsForBrowserApproval(t *testing.T) {
	var calls int
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			calls++
			if calls == 1 {
				return ipc.EnrollResponse{
					StatusResponse: ipc.StatusResponse{State: ipc.StateNeedsApproval, ApprovalURL: "https://admin.example.test/approve/device-1"},
				}, nil
			}
			return ipc.EnrollResponse{
				StatusResponse: ipc.StatusResponse{State: ipc.StateConnected, ControlState: ipc.ControlStateReady},
				WireGuardApply: testSuccessfulIPCWireGuardApply(),
			}, nil
		},
	}))
	defer server.Close()
	ipc := ipc.NewClient(server.Client())
	ipc.BaseURL = server.URL

	out, err := captureStdout(t, func() error {
		return runManagedUp(context.Background(), ipc, "", defaultPublicServerURL, "server", "node-a", "", true, time.Millisecond)
	})
	if err != nil {
		t.Fatalf("runManagedUp failed: %v\n%s", err, out)
	}
	if calls != 2 {
		t.Fatalf("managed enrollment calls = %d, want 2", calls)
	}
	if strings.Count(out, "https://admin.example.test/approve/device-1") != 1 || !strings.Contains(out, "EndlessNet connected.") {
		t.Fatalf("managed up approval output = %q", out)
	}
}

func TestRunManagedUpAcceptsDegradedHealthAfterSuccessfulApply(t *testing.T) {
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Enroll: func(context.Context, ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			return ipc.EnrollResponse{
				StatusResponse: ipc.StatusResponse{
					State:        ipc.StateDegraded,
					ControlState: ipc.ControlStateDegraded,
					OverlayIP:    "100.64.0.3",
				},
				WireGuardApply: testSuccessfulIPCWireGuardApply(),
			}, nil
		},
	}))
	defer server.Close()
	ipcClient := ipc.NewClient(server.Client())
	ipcClient.BaseURL = server.URL

	out, err := captureStdout(t, func() error {
		return runManagedUp(context.Background(), ipcClient, "", defaultPublicServerURL, "server", "latvia", "", true, time.Millisecond)
	})
	if err != nil {
		t.Fatalf("runManagedUp failed: %v\n%s", err, out)
	}
	for _, want := range []string{"EndlessNet connected.", "IP: 100.64.0.3", "service/control health is degraded"} {
		if !strings.Contains(out, want) {
			t.Fatalf("managed up output = %q, want %q", out, want)
		}
	}
}

func TestRunManagedUpRequiresSuccessfulApplyResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply *ipc.WireGuardApplyResult
	}{
		{name: "missing"},
		{name: "unsuccessful", apply: &ipc.WireGuardApplyResult{Method: "wireguard-go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
				Enroll: func(context.Context, ipc.EnrollRequest) (ipc.EnrollResponse, error) {
					return ipc.EnrollResponse{
						StatusResponse: ipc.StatusResponse{
							State: ipc.StateConnected, ControlState: ipc.ControlStateReady,
						},
						WireGuardApply: tc.apply,
					}, nil
				},
			}))
			defer server.Close()
			ipcClient := ipc.NewClient(server.Client())
			ipcClient.BaseURL = server.URL

			out, err := captureStdout(t, func() error {
				return runManagedUp(context.Background(), ipcClient, "", defaultPublicServerURL, "server", "node-a", "", true, time.Millisecond)
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "wireguard apply") {
				t.Fatalf("runManagedUp error = %v, want WireGuard apply failure", err)
			}
			if strings.Contains(out, "EndlessNet connected.") {
				t.Fatalf("managed up reported success without successful apply: %q", out)
			}
		})
	}
}

func TestWaitForBrowserEnrollmentCompletesApprovedSavedRequestWithoutWaiting(t *testing.T) {
	pollToken := "poll-secret"
	var statusCalls, completeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+pollToken {
			http.Error(w, "missing poll authorization", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/nodes/enrollment-requests/request-1":
			statusCalls++
			_ = json.NewEncoder(w).Encode(clientapi.NodeEnrollmentRequestStatusResponse{
				Request: clientapi.NodeEnrollmentRequest{ID: "request-1", Status: clientapi.NodeEnrollmentRequestApproved},
			})
		case "/nodes/enrollment-requests/request-1/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(clientapi.CompleteNodeEnrollmentRequestResponse{
				Registration: &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: "node-1"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "client.json")
	identityPrivateKey, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	identityPublicKey, err := client.IdentityPublicKey(identityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	savedRequest := clientapi.RegisterNodeRequest{
		IdempotencyID:     "saved-idempotency-key",
		Hostname:          "node-a",
		IdentityPublicKey: identityPublicKey,
		PublicKey:         testWireGuardPublicKey("saved-browser-request"),
		DeviceFingerprint: "saved-device-fingerprint",
	}
	savedRequest.IdentitySignature, err = client.SignIdentity(identityPrivateKey, clientapi.RegistrationIdentityProofPayload(savedRequest))
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{
		EnrollmentRequestID: "request-1",
		EnrollmentPollToken: pollToken,
		ApprovalURL:         "https://admin.example.test/approve/device-1",
		EnrollmentRequest:   &savedRequest,
	}
	req := savedRequest
	req.IdempotencyID = "new-idempotency-key-that-must-not-replace-the-saved-request"
	response, err := waitForBrowserEnrollmentApproval(clientapi.NewAPI(server.URL, ""), &cfg, configPath, &req, 0)
	if err != nil {
		t.Fatal(err)
	}
	if response.Node.ID != "node-1" || statusCalls != 1 || completeCalls != 1 || req.IdempotencyID != savedRequest.IdempotencyID {
		t.Fatalf("approved completion response=%#v status_calls=%d complete_calls=%d", response, statusCalls, completeCalls)
	}
}

func TestWaitForBrowserEnrollmentReplacesExpiredSavedRequest(t *testing.T) {
	const (
		oldPollToken = "old-poll-secret"
		newPollToken = "new-poll-secret"
		newURL       = "https://admin.example.test/?enrollment_request=request-2"
	)
	var statusCalls, createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/nodes/enrollment-requests/request-1":
			statusCalls++
			if r.Header.Get("Authorization") != "Bearer "+oldPollToken {
				http.Error(w, "missing poll authorization", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(clientapi.NodeEnrollmentRequestStatusResponse{
				Request: clientapi.NodeEnrollmentRequest{
					ID:     "request-1",
					Status: clientapi.NodeEnrollmentRequestExpired,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/nodes/enrollment-requests":
			createCalls++
			var replacement clientapi.RegisterNodeRequest
			if err := json.NewDecoder(r.Body).Decode(&replacement); err != nil || replacement.IdempotencyID == "saved-idempotency-key" || clientapi.VerifyRegisterNodeIdentityProof(replacement) != nil {
				t.Error("replacement must use a new operation ID and a valid identity proof")
				http.Error(w, "invalid replacement", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(clientapi.CreateNodeEnrollmentRequestResponse{
				Request: clientapi.NodeEnrollmentRequest{
					ID:          "request-2",
					Status:      clientapi.NodeEnrollmentRequestPending,
					ApprovalURL: newURL,
				},
				PollToken: newPollToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "client.json")
	identityPrivateKey, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	identityPublicKey, err := client.IdentityPublicKey(identityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	savedRequest := clientapi.RegisterNodeRequest{
		IdempotencyID:     "saved-idempotency-key",
		Hostname:          "node-a",
		IdentityPublicKey: identityPublicKey,
		PublicKey:         testWireGuardPublicKey("expired-browser-request"),
		DeviceFingerprint: "saved-device-fingerprint",
	}
	savedRequest.IdentitySignature, err = client.SignIdentity(identityPrivateKey, clientapi.RegistrationIdentityProofPayload(savedRequest))
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{
		EnrollmentRequestID: "request-1",
		EnrollmentPollToken: oldPollToken,
		ApprovalURL:         "https://admin.example.test/?enrollment_request=request-1",
		EnrollmentRequest:   &savedRequest,
		NodeApprovalState:   clientapi.NodeEnrollmentRequestPending,
		IdentityPrivateKey:  identityPrivateKey,
	}
	req := savedRequest
	_, err = waitForBrowserEnrollmentApproval(
		clientapi.NewAPI(server.URL, ""),
		&cfg,
		configPath,
		&req,
		0,
	)
	var approvalRequired enrollmentApprovalRequiredError
	if !errors.As(err, &approvalRequired) {
		t.Fatalf("expired enrollment replacement error = %v, want approval required", err)
	}
	if approvalRequired.RequestID != "request-2" || approvalRequired.ApprovalURL != newURL {
		t.Fatalf("replacement approval = %#v", approvalRequired)
	}
	if statusCalls != 1 || createCalls != 1 {
		t.Fatalf("expired status calls=%d create calls=%d, want 1 each", statusCalls, createCalls)
	}
	if cfg.EnrollmentRequestID != "request-2" || cfg.EnrollmentPollToken != newPollToken || cfg.ApprovalURL != newURL {
		t.Fatalf("replacement enrollment state = %#v", cfg)
	}
}

func TestWaitForBrowserEnrollmentReplacesMissingSavedRequest(t *testing.T) {
	const (
		oldPollToken = "old-poll-secret"
		newPollToken = "new-poll-secret"
		newURL       = "https://admin.example.test/?enrollment_request=request-2"
	)
	var statusCalls, createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/nodes/enrollment-requests/request-1":
			statusCalls++
			if r.Header.Get("Authorization") != "Bearer "+oldPollToken {
				http.Error(w, "missing poll authorization", http.StatusUnauthorized)
				return
			}
			http.Error(w, `{"error":"enrollment request not found"}`, http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/nodes/enrollment-requests":
			createCalls++
			_ = json.NewEncoder(w).Encode(clientapi.CreateNodeEnrollmentRequestResponse{
				Request: clientapi.NodeEnrollmentRequest{
					ID:          "request-2",
					Status:      clientapi.NodeEnrollmentRequestPending,
					ApprovalURL: newURL,
				},
				PollToken: newPollToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "client.json")
	req := clientapi.RegisterNodeRequest{Hostname: "node-a", IdempotencyID: "new-idempotency-key"}
	cfg := client.Config{
		EnrollmentRequestID: "request-1",
		EnrollmentPollToken: oldPollToken,
		ApprovalURL:         "https://admin.example.test/?enrollment_request=request-1",
		NodeApprovalState:   clientapi.NodeEnrollmentRequestPending,
	}
	_, err := waitForBrowserEnrollmentApproval(
		clientapi.NewAPI(server.URL, ""),
		&cfg,
		configPath,
		&req,
		0,
	)
	var approvalRequired enrollmentApprovalRequiredError
	if !errors.As(err, &approvalRequired) {
		t.Fatalf("missing enrollment replacement error = %v, want approval required", err)
	}
	if approvalRequired.RequestID != "request-2" || approvalRequired.ApprovalURL != newURL {
		t.Fatalf("replacement approval = %#v", approvalRequired)
	}
	if statusCalls != 1 || createCalls != 1 {
		t.Fatalf("missing status calls=%d create calls=%d, want 1 each", statusCalls, createCalls)
	}
	if cfg.EnrollmentRequestID != "request-2" || cfg.EnrollmentPollToken != newPollToken || cfg.ApprovalURL != newURL {
		t.Fatalf("replacement enrollment state = %#v", cfg)
	}
	if cfg.EnrollmentRequest == nil || cfg.EnrollmentRequest.IdempotencyID != req.IdempotencyID {
		t.Fatalf("replacement enrollment proof = %#v", cfg.EnrollmentRequest)
	}
}

func TestAgentOnlineNetworkMapRefreshesSnapshotOnIdleStream(t *testing.T) {
	mapKey := testMapSigningKey(t)
	relayCredential, err := relayauth.Sign(mapKey, "net-1", "node-1", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	networkMap := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 7},
		Node:    clientapi.Node{ID: "node-1", NetworkID: "net-1", Hostname: "node-a", PublicKey: testWireGuardPublicKey("pub-a"), AssignedIP: "100.64.0.2"},
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  testWireGuardPublicKey("pub-peer-a"),
			Endpoint:   "peer-a.example.test:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
		Relays: []relayauth.Endpoint{{
			ID:       "relay-1",
			Addr:     "127.0.0.1:12345",
			Protocol: relayauth.EndpointProtocolTLS,
		}},
		RelayCredential: relayCredential,
	}
	signature, err := clientapi.SignNetworkMap(mapKey, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	networkMap.MapSignature = signature

	var mu sync.Mutex
	streamQueries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, signature)))
		case "/nodes/node-1/endpoint":
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(networkMap)
		case "/maps/node-1/stream":
			setTestMapStreamResponseHeaders(w)
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			query := r.URL.RawQuery
			mu.Lock()
			streamQueries = append(streamQueries, query)
			mu.Unlock()
			switch r.URL.Query().Get("from_network_revision") {
			case "7":
				w.Header().Set("Content-Type", "application/x-ndjson")
				return
			case "0":
				_ = json.NewEncoder(w).Encode(testMapStreamSnapshotEvent(t, networkMap))
			default:
				http.Error(w, fmt.Sprintf("unexpected from_network_revision %q", r.URL.Query().Get("from_network_revision")), http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cached := networkMap
	cached.NodeCredential = ""
	cached.RelayCredential = nil
	savedAt := time.Now().UTC()
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
		NetworkID:        "net-1",
		MapRevision:      7,
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, signature)),
		CachedMap:        &cached,
		CachedMapSavedAt: &savedAt,
	}); err != nil {
		t.Fatal(err)
	}

	_, got, unchanged, err := agentOnlineNetworkMap(configPath, 50*time.Millisecond, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("agentOnlineNetworkMap did not report idle stream refresh")
	}
	if got.RelayCredential == nil || got.RelayCredential.Signature != relayCredential.Signature {
		t.Fatalf("idle refresh map missing fresh relay credential: %#v", got.RelayCredential)
	}
	if got.Network.Revision != 7 || len(got.Peers) != 1 {
		t.Fatalf("idle refresh map = %#v", got)
	}
	mu.Lock()
	queries := append([]string(nil), streamQueries...)
	mu.Unlock()
	if len(queries) != 2 || !strings.Contains(queries[0], "from_network_revision=7") || !strings.Contains(queries[1], "from_network_revision=0") {
		t.Fatalf("stream queries = %#v, want current revision followed by resnapshot", queries)
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CachedMap == nil || saved.CachedMap.RelayCredential != nil {
		t.Fatalf("saved cached map leaked relay credential: %#v", saved.CachedMap)
	}
}

func TestAgentOnlineNetworkMapFailsOverToConfiguredCoordinator(t *testing.T) {
	mapKey := testMapSigningKey(t)
	heartbeatMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 10)
	networkMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 11)
	primaryCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		http.Error(w, "primary unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()

	secondaryServerKeyCalls := 0
	secondaryEndpointCalls := 0
	secondaryStreamCalls := 0
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			secondaryServerKeyCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, networkMap.MapSignature)))
		case "/nodes/node-1/endpoint":
			secondaryEndpointCalls++
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(heartbeatMap)
		case "/maps/node-1/stream":
			setTestMapStreamResponseHeaders(w)
			secondaryStreamCalls++
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_ = json.NewEncoder(w).Encode(testMapStreamSnapshotEvent(t, networkMap))
		default:
			http.NotFound(w, r)
		}
	}))
	defer secondary.Close()

	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{primary.URL, secondary.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
		NetworkID:        "net-1",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		MapRevision:      10,
	}); err != nil {
		t.Fatal(err)
	}

	_, got, unchanged, err := agentOnlineNetworkMap(configPath, 200*time.Millisecond, 10)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged {
		t.Fatal("fallback stream unexpectedly reported unchanged map")
	}
	if got.Network.Revision != networkMap.Network.Revision {
		t.Fatalf("fallback map revision = %d, want %d", got.Network.Revision, networkMap.Network.Revision)
	}
	if primaryCalls == 0 || secondaryServerKeyCalls != 1 || secondaryEndpointCalls != 1 || secondaryStreamCalls != 1 {
		t.Fatalf("calls primary=%d secondary-key=%d secondary-endpoint=%d secondary-stream=%d, want failover through fallback", primaryCalls, secondaryServerKeyCalls, secondaryEndpointCalls, secondaryStreamCalls)
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MapRevision != networkMap.Network.Revision {
		t.Fatalf("saved map revision = %d, want %d", saved.MapRevision, networkMap.Network.Revision)
	}
}

func TestCmdSyncRejectsTamperedStreamUpdateWithoutCacheMutation(t *testing.T) {
	mapKey := testMapSigningKey(t)
	base := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 7)
	tampered := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 8)
	tampered.Peers = []clientapi.Peer{{
		ID:         "peer-tampered",
		Hostname:   "peer-tampered",
		PublicKey:  testWireGuardPublicKey("pub-tampered"),
		Endpoint:   "evil.example.test:51820",
		AllowedIPs: []string{"100.64.0.99/32"},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, base.MapSignature)))
		case "/maps/node-1/stream":
			setTestMapStreamResponseHeaders(w)
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_ = json.NewEncoder(w).Encode(testMapStreamSnapshotEventWithSignature(tampered, base.MapSignature))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "wg.conf")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
		NetworkID:        "net-1",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, base.MapSignature)),
		MapRevision:      7,
		CachedMap:        &base,
	}); err != nil {
		t.Fatal(err)
	}

	err := cmdSync([]string{"--config", configPath, "--timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("cmdSync error = %v, want payload hash mismatch", err)
	}
	saved, loadErr := client.LoadConfig(configPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.MapRevision != 7 || saved.CachedMap == nil || saved.CachedMap.Network.Revision != 7 {
		t.Fatalf("tampered stream update mutated cache: %#v", saved)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output path stat error = %v, want not exist", statErr)
	}
}

func TestCmdAgentWritesFailureStateForTamperedMapWithoutReplacingOutput(t *testing.T) {
	mapKey := testMapSigningKey(t)
	base := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 7)
	tampered := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 8)
	tampered.Peers = []clientapi.Peer{{
		ID:         "peer-tampered",
		Hostname:   "peer-tampered",
		PublicKey:  testWireGuardPublicKey("pub-tampered"),
		Endpoint:   "evil.example.test:51820",
		AllowedIPs: []string{"100.64.0.99/32"},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			_, _ = w.Write([]byte("ok"))
		case "/server-key":
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, base.MapSignature)))
		case "/nodes/node-1/endpoint":
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			var req clientapi.UpdateNodeEndpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.Status != clientapi.NodeStatusOnline {
				http.Error(w, "unexpected node status", http.StatusBadRequest)
				return
			}
			if req.ClientVersion != strings.TrimSpace(version) {
				http.Error(w, "unexpected client version", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(base)
		case "/maps/node-1/stream":
			setTestMapStreamResponseHeaders(w)
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				http.Error(w, "missing node credential", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_ = json.NewEncoder(w).Encode(testMapStreamSnapshotEventWithSignature(tampered, base.MapSignature))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "wg.conf")
	statePath := filepath.Join(tmp, "agent-state.json")
	previousOutput := []byte("[Interface]\nPrivateKey = previous-secret\n")
	if err := os.WriteFile(outputPath, previousOutput, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
		NetworkID:        "net-1",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, base.MapSignature)),
		MapRevision:      7,
		CachedMap:        &base,
	}); err != nil {
		t.Fatal(err)
	}

	err := cmdAgent([]string{"--once", "--config", configPath, "--state-output", statePath, "--timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("cmdAgent error = %v, want payload hash mismatch", err)
	}
	rawOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawOutput) != string(previousOutput) {
		t.Fatalf("agent replaced output after tampered map: %q", rawOutput)
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MapRevision != 7 || saved.CachedMap == nil || saved.CachedMap.Network.Revision != 7 {
		t.Fatalf("tampered agent update mutated cache: %#v", saved)
	}
	snapshot, err := client.LoadAgentSnapshot(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.LastError, "payload hash mismatch") {
		t.Fatalf("agent failure state last_error = %q, want payload hash mismatch", snapshot.LastError)
	}
	for _, secret := range []string{"credential-1", "private-key", "previous-secret"} {
		if strings.Contains(snapshot.LastError, secret) {
			t.Fatalf("agent failure state leaked %q: %#v", secret, snapshot)
		}
	}
	status, err := agentIPCStatus(context.Background(), agentIPCOptions{ConfigPath: configPath, StateOutput: statePath})
	if err != nil {
		t.Fatal(err)
	}
	if status.ControlState != ipc.ControlStateDegraded || status.State != ipc.StateDegraded {
		t.Fatalf("IPC status after tampered map failure = %#v, want degraded", status)
	}
	if status.Agent == nil || !strings.Contains(status.Agent.LastError, "payload hash mismatch") {
		t.Fatalf("IPC status missing agent last_error: %#v", status)
	}
}

func TestAgentReconnectDelayUsesExponentialBackoffJitterAndMax(t *testing.T) {
	base := time.Second
	maxDelay := 10 * time.Second
	if got := agentReconnectDelay(base, maxDelay, 1, 0, 0); got != time.Second {
		t.Fatalf("first reconnect delay = %s, want 1s", got)
	}
	if got := agentReconnectDelay(base, maxDelay, 2, 0, 0); got != 2*time.Second {
		t.Fatalf("second reconnect delay = %s, want 2s", got)
	}
	if got := agentReconnectDelay(base, maxDelay, 5, 0, 0); got != maxDelay {
		t.Fatalf("capped reconnect delay = %s, want %s", got, maxDelay)
	}
	if got := agentReconnectDelay(base, maxDelay, 2, 0.5, 0.5); got != 2500*time.Millisecond {
		t.Fatalf("jittered reconnect delay = %s, want 2.5s", got)
	}
	if got := agentReconnectDelay(base, 3*time.Second, 5, 0.5, 1); got != 3*time.Second {
		t.Fatalf("max jittered reconnect delay = %s, want 3s", got)
	}
}

func TestAgentReconnectDelayDistributesHundredClientsByJitter(t *testing.T) {
	base := time.Second
	maxDelay := 10 * time.Second
	const clients = 100
	delays := make(map[time.Duration]bool, clients)
	for i := 0; i < clients; i++ {
		jitterUnit := float64(i) / float64(clients-1)
		delay := agentReconnectDelay(base, maxDelay, 1, 0.2, jitterUnit)
		if delay < base || delay > 1200*time.Millisecond {
			t.Fatalf("client %d reconnect delay = %s, want [1s, 1.2s]", i, delay)
		}
		delays[delay] = true
	}
	if len(delays) < 90 {
		t.Fatalf("100-client reconnect jitter produced %d distinct delays, want at least 90", len(delays))
	}
}

func TestWaitForAgentSyncInterruptsReconnectBackoff(t *testing.T) {
	wake := make(chan struct{}, 1)
	wake <- struct{}{}
	started := time.Now()
	proceed, woken := waitForAgentSync(context.Background(), time.Minute, wake)
	if !proceed || !woken {
		t.Fatalf("agent sync wait result = proceed:%t woken:%t, want true/true", proceed, woken)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("agent sync wake took %s, want less than 1s", elapsed)
	}
}

func TestEndpointUpdateStateDebouncesAndCoalescesChanges(t *testing.T) {
	state := endpointUpdateState{}
	start := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	if endpoint, ok := state.NextEndpointUpdate("a.example.test:51820", "old.example.test:51820", start, 5*time.Second); ok {
		t.Fatalf("first flapping endpoint update = %q, want debounce hold", endpoint)
	}
	if endpoint, ok := state.NextEndpointUpdate("b.example.test:51820", "old.example.test:51820", start.Add(2*time.Second), 5*time.Second); ok {
		t.Fatalf("coalesced endpoint update = %q, want debounce reset", endpoint)
	}
	if endpoint, ok := state.NextEndpointUpdate("b.example.test:51820", "old.example.test:51820", start.Add(6*time.Second), 5*time.Second); ok {
		t.Fatalf("early stable endpoint update = %q, want debounce hold", endpoint)
	}
	endpoint, ok := state.NextEndpointUpdate("b.example.test:51820", "old.example.test:51820", start.Add(8*time.Second), 5*time.Second)
	if !ok || endpoint != "b.example.test:51820" {
		t.Fatalf("stable endpoint update = %q/%v, want b/true", endpoint, ok)
	}
	state.MarkEndpointConfirmed()
	if endpoint, ok := state.NextEndpointUpdate("b.example.test:51820", "b.example.test:51820", start.Add(9*time.Second), 5*time.Second); ok {
		t.Fatalf("duplicate endpoint update = %q, want no update", endpoint)
	}
	endpoint, ok = state.NextEndpointUpdate("b.example.test:51820", "", start.Add(9*time.Second), 0)
	if !ok || endpoint != "b.example.test:51820" {
		t.Fatalf("disappeared authoritative endpoint update = %q/%v, want b/true", endpoint, ok)
	}
	endpoint, ok = state.NextEndpointUpdate("c.example.test:51820", "b.example.test:51820", start.Add(10*time.Second), 0)
	if !ok || endpoint != "c.example.test:51820" {
		t.Fatalf("zero-debounce endpoint update = %q/%v, want c/true", endpoint, ok)
	}
}

func TestEndpointUpdateRequestBuildsGeneratedCandidates(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	req, key, refresh, ok, err := endpointUpdateRequest(client.Config{
		CachedMap: &clientapi.RegisterNodeResponse{
			Node: clientapi.Node{
				Endpoint:           "192.168.55.11:51820",
				EndpointGeneration: 6,
			},
		},
	}, []string{"192.168.55.12:51820", "198.51.100.12:51820"}, true, 2*time.Minute, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || refresh || key != "192.168.55.12:51820\n198.51.100.12:51820" {
		t.Fatalf("generated request state = ok:%v refresh:%v key:%q", ok, refresh, key)
	}
	if req.Endpoint != "192.168.55.12:51820" || req.Generation != 7 || req.TTL != "2m0s" || strings.Join(req.Candidates, ",") != "192.168.55.12:51820,198.51.100.12:51820" {
		t.Fatalf("generated endpoint request = %#v", req)
	}
}

func TestEndpointUpdateRequestAdvancesRetainedExpiredGeneration(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	req, _, refresh, ok, err := endpointUpdateRequest(client.Config{
		CachedMap: &clientapi.RegisterNodeResponse{
			Node: clientapi.Node{
				EndpointGeneration: 7,
			},
		},
	}, []string{"192.168.55.12:51820", "198.51.100.12:51820"}, true, 2*time.Minute, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || refresh || req.Generation != 8 {
		t.Fatalf("expired generated request = %#v ok:%v refresh:%v, want generation 8", req, ok, refresh)
	}
}

func TestEndpointUpdateRequestSkipsFreshGeneratedCandidates(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	expires := now.Add(90 * time.Second)
	_, _, _, ok, err := endpointUpdateRequest(client.Config{
		CachedMap: &clientapi.RegisterNodeResponse{
			Node: clientapi.Node{
				Endpoint:           "192.168.55.12:51820",
				EndpointGeneration: 6,
				EndpointCandidates: []string{"192.168.55.12:51820", "198.51.100.12:51820"},
				EndpointExpiresAt:  &expires,
			},
		},
	}, []string{"192.168.55.12:51820", "198.51.100.12:51820"}, true, 2*time.Minute, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("fresh generated endpoint candidates unexpectedly require update")
	}
}

func TestEndpointUpdateRequestRefreshesExpiringGeneratedCandidates(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	expires := now.Add(30 * time.Second)
	req, _, refresh, ok, err := endpointUpdateRequest(client.Config{
		CachedMap: &clientapi.RegisterNodeResponse{
			Node: clientapi.Node{
				Endpoint:           "192.168.55.12:51820",
				EndpointGeneration: 6,
				EndpointCandidates: []string{"192.168.55.12:51820", "198.51.100.12:51820"},
				EndpointExpiresAt:  &expires,
			},
		},
	}, []string{"192.168.55.12:51820", "198.51.100.12:51820"}, true, 2*time.Minute, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !refresh || req.Generation != 7 {
		t.Fatalf("expiring generated request = %#v ok:%v refresh:%v", req, ok, refresh)
	}
}

func TestEndpointUpdateConfirmedRequiresFullGeneratedState(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	req := clientapi.UpdateNodeEndpointRequest{
		Endpoint:   "192.168.55.12:51820",
		Generation: 8,
		Candidates: []string{"192.168.55.12:51820", "198.51.100.12:51820"},
		TTL:        "2m",
	}
	confirmed := clientapi.Node{
		Endpoint:           req.Endpoint,
		EndpointGeneration: req.Generation,
		EndpointCandidates: append([]string(nil), req.Candidates...),
		EndpointExpiresAt:  &expiresAt,
	}
	if !endpointUpdateConfirmed(confirmed, req, now) {
		t.Fatal("complete generated endpoint state was not confirmed")
	}

	tests := []struct {
		name   string
		mutate func(*clientapi.Node)
	}{
		{
			name: "endpoint",
			mutate: func(node *clientapi.Node) {
				node.Endpoint = ""
			},
		},
		{
			name: "generation",
			mutate: func(node *clientapi.Node) {
				node.EndpointGeneration--
			},
		},
		{
			name: "candidates",
			mutate: func(node *clientapi.Node) {
				node.EndpointCandidates = []string{req.Candidates[0]}
			},
		},
		{
			name: "expiry",
			mutate: func(node *clientapi.Node) {
				expired := now.Add(-time.Nanosecond)
				node.EndpointExpiresAt = &expired
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := confirmed
			node.EndpointCandidates = append([]string(nil), confirmed.EndpointCandidates...)
			tt.mutate(&node)
			if endpointUpdateConfirmed(node, req, now) {
				t.Fatalf("incomplete generated endpoint state confirmed: %#v", node)
			}
		})
	}
}

func TestUpdatePublishedEndpointRetriesUnconfirmedHTTP200(t *testing.T) {
	now := time.Now().UTC()
	candidates := []string{"192.168.55.12:51820", "198.51.100.12:51820"}
	mapKey := testMapSigningKey(t)
	staleMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 7)
	staleMap.Node.EndpointGeneration = 7
	staleSignature, err := clientapi.SignNetworkMap(mapKey, staleMap)
	if err != nil {
		t.Fatal(err)
	}
	staleMap.MapSignature = staleSignature

	expiresAt := now.Add(2 * time.Minute)
	acceptedMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 8)
	acceptedMap.Node.Endpoint = candidates[0]
	acceptedMap.Node.EndpointGeneration = 8
	acceptedMap.Node.EndpointCandidates = append([]string(nil), candidates...)
	acceptedMap.Node.EndpointExpiresAt = &expiresAt
	acceptedSignature, err := clientapi.SignNetworkMap(mapKey, acceptedMap)
	if err != nil {
		t.Fatal(err)
	}
	acceptedMap.MapSignature = acceptedSignature

	var requests []clientapi.UpdateNodeEndpointRequest
	publicKey := testMapSigningPublicKey(t, staleMap.MapSignature)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, publicKey))
		case "/nodes/node-1/endpoint":
			var req clientapi.UpdateNodeEndpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			requests = append(requests, req)
			w.Header().Set("Content-Type", "application/json")
			if len(requests) == 1 {
				_ = json.NewEncoder(w).Encode(staleMap)
				return
			}
			_ = json.NewEncoder(w).Encode(acceptedMap)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "node-credential",
		MapSigningTrust:  testSigningTrustBundle(t, publicKey),
		MapRevision:      staleMap.Network.Revision,
		CachedMap:        &staleMap,
	}); err != nil {
		t.Fatal(err)
	}

	state := &endpointUpdateState{}
	confirmed, err := updatePublishedEndpoint(configPath, time.Second, candidates, true, 2*time.Minute, state, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed {
		t.Fatal("unapplied HTTP 200 endpoint update reported confirmed")
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CachedMap == nil || saved.CachedMap.Node.Endpoint != "" || saved.CachedMap.Node.EndpointGeneration != 7 {
		t.Fatalf("saved authoritative stale map = %#v", saved.CachedMap)
	}
	if !state.HasUnconfirmedEndpoint(strings.Join(candidates, "\n")) {
		t.Fatalf("unconfirmed endpoint state = %#v", state)
	}

	confirmed, err = updatePublishedEndpoint(configPath, time.Second, candidates, true, 2*time.Minute, state, 0, now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("confirmed endpoint response did not report success")
	}
	if len(requests) != 2 || requests[0].Generation != 8 || requests[1].Generation != 8 {
		t.Fatalf("endpoint update requests = %#v, want two generation 8 attempts", requests)
	}
	if state.HasUnconfirmedEndpoint(strings.Join(candidates, "\n")) {
		t.Fatalf("confirmed endpoint remained pending: %#v", state)
	}

	confirmed, err = updatePublishedEndpoint(configPath, time.Second, candidates, true, 2*time.Minute, state, 0, now.Add(45*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if confirmed || len(requests) != 2 {
		t.Fatalf("fresh confirmed endpoint retried: confirmed=%v requests=%#v", confirmed, requests)
	}
}

func TestConnectAgentTunnelConfiguresCachedMapWithoutRendering(t *testing.T) {
	tmp := t.TempDir()
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	cfg := client.Config{
		ControlPlaneURLs: []string{"https://api.example.test"},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "node-credential",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		MapRevision:      7,
		CachedMap:        &networkMap,
	}
	configPath := filepath.Join(tmp, "client.json")
	if err := client.SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}

	payload, err := connectAgentTunnel(context.Background(), agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  wireGuard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.NodeID != "node-1" {
		t.Fatalf("connect payload = %#v", payload)
	}
	if wireGuard.configureCalls != 1 {
		t.Fatalf("wireguard-go configure calls = %d, want 1", wireGuard.configureCalls)
	}
}

func TestAgentIPCDisconnectPersistsConnectionIntent(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:         "node-1",
		NetworkID:      "net-1",
		NodeCredential: "credential-1",
	}); err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}
	opts := agentIPCOptions{
		ConfigPath:     configPath,
		StateOutput:    statePath,
		WireGuard:      wireGuard,
		SyncForConnect: func() error { return nil },
	}
	handlers := agentIPCHandlers(opts)
	before, err := handlers.Status(context.Background(), ipc.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if before.State != ipc.StateConnected {
		t.Fatalf("status before disconnect = %#v, want Connected", before)
	}
	disconnect, err := handlers.Disconnect(context.Background(), ipc.DisconnectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if disconnect.State != ipc.StateDisconnected || !disconnect.UserDisconnected {
		t.Fatalf("disconnect payload = %#v, want disconnected user intent", disconnect)
	}
	if wireGuard.downCalls != 1 {
		t.Fatalf("wireguard-go down calls = %d, want 1", wireGuard.downCalls)
	}
	intent, disconnected, err := agentConnectionIntentStore(opts).Disconnected()
	if err != nil {
		t.Fatal(err)
	}
	if !disconnected || intent.DesiredState != client.ConnectionIntentDesiredDisconnected || intent.Reason != "user_disconnect" {
		t.Fatalf("connection intent = %#v disconnected=%v, want user disconnect", intent, disconnected)
	}
	after, err := handlers.Status(context.Background(), ipc.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if after.State != ipc.StateDisconnected || after.ControlState != ipc.ControlStateDisconnected || !after.UserDisconnected {
		t.Fatalf("status after disconnect = %#v, want persistent Disconnected", after)
	}
}

func TestAgentIPCDisconnectPublishesOfflineNodeStatus(t *testing.T) {
	mapKey := testMapSigningKey(t)
	cached := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 7)
	offlineMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 8)
	offlineMap.Node.Status = clientapi.NodeStatusOffline
	offlineSignature, err := clientapi.SignNetworkMap(mapKey, offlineMap)
	if err != nil {
		t.Fatal(err)
	}
	offlineMap.MapSignature = offlineSignature

	patchCount := 0
	gotCredential := ""
	gotStatus := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, testMapSigningPublicKey(t, cached.MapSignature)))
		case "/nodes/node-1/endpoint":
			patchCount++
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			gotCredential = r.Header.Get("X-EndlessNet-Node-Credential")
			var req clientapi.UpdateNodeEndpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotStatus = req.Status
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(offlineMap)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "node-credential",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, cached.MapSignature)),
		MapRevision:      cached.Network.Revision,
		CachedMap:        &cached,
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCHandlers(agentIPCOptions{ConfigPath: configPath, WireGuard: &testAgentWireGuard{}}).Disconnect(context.Background(), ipc.DisconnectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateDisconnected || !payload.UserDisconnected {
		t.Fatalf("disconnect payload = %#v, want disconnected user intent", payload)
	}
	if patchCount != 1 || gotCredential != "node-credential" || gotStatus != clientapi.NodeStatusOffline {
		t.Fatalf("offline publish patch_count=%d credential=%q status=%q", patchCount, gotCredential, gotStatus)
	}
	saved, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MapRevision != offlineMap.Network.Revision || saved.CachedMap == nil || saved.CachedMap.Node.Status != clientapi.NodeStatusOffline {
		t.Fatalf("saved offline cache = revision %d map %#v, want revision %d offline", saved.MapRevision, saved.CachedMap, offlineMap.Network.Revision)
	}
}

func TestAgentIPCDisconnectPersistsUnenrolledConnectionIntent(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{}); err != nil {
		t.Fatal(err)
	}
	opts := agentIPCOptions{ConfigPath: configPath, StateOutput: statePath, WireGuard: &testAgentWireGuard{}}
	handlers := agentIPCHandlers(opts)
	before, err := handlers.Status(context.Background(), ipc.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if before.State != ipc.StateNeedsEnrollment {
		t.Fatalf("status before disconnect = %#v, want NeedsEnrollment", before)
	}
	disconnect, err := handlers.Disconnect(context.Background(), ipc.DisconnectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if disconnect.State != ipc.StateDisconnected || disconnect.DesiredState != client.ConnectionIntentDesiredDisconnected || !disconnect.UserDisconnected {
		t.Fatalf("disconnect payload = %#v, want disconnected user intent", disconnect)
	}
	after, err := handlers.Status(context.Background(), ipc.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if after.State != ipc.StateDisconnected || after.ControlState != ipc.ControlStateDisconnected || !after.UserDisconnected {
		t.Fatalf("status after disconnect = %#v, want persistent Disconnected", after)
	}
}

func TestAgentIPCConnectClearsDisconnectedConnectionIntent(t *testing.T) {
	tmp := t.TempDir()
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{"https://api.example.test"},
		PrivateKey:       "private-key",
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "node-credential",
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		MapRevision:      7,
		CachedMap:        &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}
	syncWake := make(chan struct{}, 1)
	opts := agentIPCOptions{
		ConfigPath:     configPath,
		StateOutput:    statePath,
		ListenPort:     51820,
		WireGuard:      wireGuard,
		SyncForConnect: func() error { return nil },
		SyncWake:       syncWake,
	}
	if err := agentConnectionIntentStore(opts).SetDisconnected("test"); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentFailureSnapshot(statePath, configPath, errors.New("previous control-plane failure")); err != nil {
		t.Fatal(err)
	}
	payload, err := agentIPCHandlers(opts).Connect(context.Background(), ipc.ConnectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.UserDisconnected {
		t.Fatalf("connect payload = %#v, want Connected and user_disconnected=false", payload)
	}
	if wireGuard.configureCalls != 1 {
		t.Fatalf("wireguard-go configure calls = %d, want 1", wireGuard.configureCalls)
	}
	stored, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionIntent != nil {
		t.Fatalf("connection intent = %#v, want cleared", stored.ConnectionIntent)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("stale agent snapshot stat error = %v, want absent", err)
	}
	select {
	case <-syncWake:
	default:
		t.Fatal("successful connect did not wake the background agent")
	}
}

func TestAgentIPCLocalForgetCompletesWithoutRemoteCleanup(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := client.SaveConfig(configPath, client.Config{
		LocalOwnerID:       "uid:1000",
		ControlPlaneURLs:   []string{"https://unavailable.example.test"},
		Token:              "session-token",
		ActiveAccountID:    "account-1",
		IdentityPrivateKey: "identity-private-key",
		PrivateKey:         "wireguard-private-key",
		NodeID:             "node-1",
		NetworkID:          "network-1",
		NodeCredential:     "node-credential",
		DeviceFingerprint:  "installation-fingerprint",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"node_id":"node-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}
	handler := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath, StateOutput: statePath, WireGuard: wireGuard,
		Now: func() time.Time { return now },
	})
	if _, err := handler.LocalForget(context.Background(), ipc.LocalForgetRequest{}); err == nil {
		t.Fatal("local forget accepted missing explicit confirmation")
	}
	payload, err := handler.LocalForget(context.Background(), ipc.LocalForgetRequest{Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Outcome != ipc.LogoutOutcomeRemoteCleanupUnconfirmed || payload.State != ipc.StateNeedsEnrollment || payload.ControlState != ipc.ControlStateNotRegistered {
		t.Fatalf("local-forget response = %#v", payload)
	}
	if wireGuard.downCalls != 1 {
		t.Fatalf("WireGuard down calls = %d, want 1", wireGuard.downCalls)
	}
	stored, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.NodeID != "" || stored.NetworkID != "" || stored.NodeCredential != "" || stored.Token != "" || stored.ActiveAccountID != "" {
		t.Fatal("local forget retained enrollment or session state")
	}
	if stored.LocalOwnerID != "uid:1000" || stored.IdentityPrivateKey != "identity-private-key" || stored.PrivateKey != "wireguard-private-key" ||
		len(stored.ControlPlaneURLs) != 1 || stored.DeviceFingerprint != "installation-fingerprint" {
		t.Fatal("local forget removed retained owner/key/trust/control material")
	}
	if stored.ConnectionIntent == nil || stored.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected {
		t.Fatalf("local forget intent = %#v", stored.ConnectionIntent)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("agent state still present: %v", err)
	}
}

func TestCmdUpPersistsPendingEnrollmentWithoutRenderingWireGuard(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "endlessnet.conf")
	previousOutput := []byte("existing WireGuard config must remain untouched\n")
	if err := os.WriteFile(outputPath, previousOutput, 0o600); err != nil {
		t.Fatal(err)
	}
	mapKey := testMapSigningKey(t)
	server, snapshot := testPendingEnrollmentServer(t, mapKey, "enr_pending")
	defer server.Close()

	if err := cmdUp([]string{
		"--config", configPath,
		"--server", server.URL,
		"--join-token", "enr_pending",
		"--hostname", "pending-a",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "node-pending" || cfg.NetworkID != "net-pending" || !strings.HasPrefix(cfg.NodeCredential, "enc_") {
		t.Fatalf("pending enrollment identity = %#v", cfg)
	}
	if cfg.NodeApprovalState != clientapi.NodeApprovalPending || cfg.CachedMap != nil || cfg.MapRevision != 0 {
		t.Fatalf("pending enrollment map state = %#v", cfg)
	}
	if strings.TrimSpace(cfg.IdentityPrivateKey) == "" || strings.TrimSpace(cfg.PrivateKey) == "" || strings.TrimSpace(cfg.DeviceFingerprint) == "" {
		t.Fatalf("pending enrollment did not persist local identity: %#v", cfg)
	}
	if !client.HasSigningTrust(cfg) {
		t.Fatal("pending enrollment did not persist signing trust")
	}
	if cfg.Token != "" {
		t.Fatalf("pending enrollment retained join/session token: %#v", cfg)
	}
	rawOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawOutput) != string(previousOutput) {
		t.Fatalf("pending enrollment rendered WireGuard output: %q", rawOutput)
	}
	req, calls := snapshot()
	if calls != 1 || req.JoinToken != "enr_pending" || req.Hostname != "pending-a" {
		t.Fatalf("pending register calls=%d request=%#v", calls, req)
	}
}

func TestCmdUpRejectsPendingEnrollmentBoundToDifferentWireGuardKey(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "endlessnet.conf")
	mapKey := testMapSigningKey(t)
	server, _ := testPendingEnrollmentServer(t, mapKey, "enr_pending", func(response *clientapi.RegisterNodeResponse) {
		response.Node.PublicKey = testWireGuardPublicKey("attacker-key")
	})
	defer server.Close()

	err := cmdUp([]string{
		"--config", configPath,
		"--server", server.URL,
		"--join-token", "enr_pending",
		"--hostname", "pending-a",
	})
	if err == nil || !strings.Contains(err.Error(), "response node binding does not match request") {
		t.Fatalf("mismatched pending enrollment error = %v", err)
	}
	cfg, loadErr := client.LoadConfig(configPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if cfg.NodeID != "" || cfg.NetworkID != "" || cfg.NodeCredential != "" || cfg.NodeApprovalState != "" {
		t.Fatalf("mismatched pending enrollment persisted authority: %#v", cfg)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("mismatched pending enrollment output stat = %v, want absent", statErr)
	}
}

func TestCmdUpRejectsEnrollmentIdentitySubstitutionBeforePersist(t *testing.T) {
	for _, test := range []struct {
		name    string
		pending bool
		mutate  func(*clientapi.RegisterNodeResponse)
		want    string
	}{
		{
			name: "approved identity key",
			mutate: func(response *clientapi.RegisterNodeResponse) {
				response.Node.IdentityPublicKey = "enp_attacker"
			},
			want: "response node binding does not match request",
		},
		{
			name:    "pending missing identity key",
			pending: true,
			mutate: func(response *clientapi.RegisterNodeResponse) {
				response.Node.IdentityPublicKey = ""
			},
			want: "response node binding does not match request",
		},
		{
			name:    "pending missing fingerprint",
			pending: true,
			mutate: func(response *clientapi.RegisterNodeResponse) {
				response.Node.DeviceFingerprint = ""
			},
			want: "response node binding does not match request",
		},
		{
			name: "invalid node credential",
			mutate: func(response *clientapi.RegisterNodeResponse) {
				response.NodeCredential = "enc_invalid"
			},
			want: "node_credential:",
		},
		{
			name: "signed response from another enrollment",
			mutate: func(response *clientapi.RegisterNodeResponse) {
				response.RegistrationBinding = clientapi.RegistrationIdentityProofBinding(clientapi.RegisterNodeRequest{Hostname: "another-node"})
			},
			want: "response registration_binding does not match request proof",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tmp := t.TempDir()
			setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
			configPath := filepath.Join(tmp, "client.json")
			outputPath := filepath.Join(tmp, "endlessnet.conf")
			mapKey := testMapSigningKey(t)
			var server *httptest.Server
			if test.pending {
				server, _ = testPendingEnrollmentServer(t, mapKey, "enr_pending", test.mutate)
			} else {
				server, _ = testEnrollmentServer(t, mapKey, "enr_pending", 1, test.mutate)
			}
			defer server.Close()

			err := cmdUp([]string{
				"--config", configPath,
				"--server", server.URL,
				"--join-token", "enr_pending",
				"--hostname", "node-a",
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("enrollment error = %v, want %q", err, test.want)
			}
			cfg, loadErr := client.LoadConfig(configPath)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if cfg.NodeID != "" || cfg.NetworkID != "" || cfg.NodeCredential != "" || cfg.CachedMap != nil {
				t.Fatalf("rejected enrollment persisted authority: %#v", cfg)
			}
			if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("rejected enrollment output stat = %v, want absent", statErr)
			}
		})
	}
}

func TestCmdUpBindsExplicitDirectEnrollmentNetwork(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	if err := client.SaveConfig(configPath, client.Config{Token: "session-token"}); err != nil {
		t.Fatal(err)
	}
	mapKey := testMapSigningKey(t)
	server, registration := testEnrollmentServer(t, mapKey, "", 1, func(response *clientapi.RegisterNodeResponse) {
		response.Network.Name = "substituted-network"
	})
	defer server.Close()

	err := cmdUp([]string{
		"--config", configPath,
		"--server", server.URL,
		"--network", "default",
		"--hostname", "node-a",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match requested network name") {
		t.Fatalf("network substitution error = %v", err)
	}
	cfg, loadErr := client.LoadConfig(configPath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if cfg.NodeID != "" || cfg.NetworkID != "" || cfg.NodeCredential != "" || cfg.CachedMap != nil {
		t.Fatalf("network substitution persisted authority: %#v", cfg)
	}
	request, calls := registration()
	if calls != 1 {
		t.Fatalf("registration calls = %d, want 1", calls)
	}
	if got, want := request.SessionTokenBinding, clientapi.RegistrationSessionTokenBinding("session-token"); got != want {
		t.Fatalf("session token binding = %q, want %q", got, want)
	}
	if got, want := request.ClientVersion, strings.TrimSpace(version); got != want {
		t.Fatalf("client version = %q, want %q", got, want)
	}
	if strings.Contains(request.SessionTokenBinding, "session-token") {
		t.Fatal("registration request exposed the raw session token in its binding")
	}
	if err := clientapi.VerifyRegisterNodeIdentityProof(request); err != nil {
		t.Fatalf("session-bound registration proof: %v", err)
	}
}

func TestAgentIPCEnrollPendingReturnsNeedsApprovalWithoutApply(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "endlessnet.conf")
	mapKey := testMapSigningKey(t)
	server, _ := testPendingEnrollmentServer(t, mapKey, "enr_pending")
	defer server.Close()

	wireGuard := &testAgentWireGuard{configure: func(client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
		return client.WireGuardApplyResult{}, errors.New("wireguard-go configure must not run")
	}}
	payloadRaw, err := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath,
		WireGuard:  wireGuard,
	}).Enroll(context.Background(), ipc.EnrollRequest{
		EnrollToken: "enr_pending",
		Server:      server.URL,
		Hostname:    "pending-ipc",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadRaw
	if payload.State != ipc.StateNeedsApproval || payload.ControlState != ipc.ControlStatePendingApproval {
		t.Fatalf("pending IPC enrollment payload = %#v", payload)
	}
	if payload.WireGuardApply != nil {
		t.Fatalf("pending IPC enrollment unexpectedly returned apply result: %#v", payload.WireGuardApply)
	}
	if wireGuard.configureCalls != 0 {
		t.Fatalf("pending IPC enrollment configure calls = %d, want 0", wireGuard.configureCalls)
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending IPC output stat = %v, want absent", err)
	}
}

func TestAgentIPCEnrollmentServerPriority(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		got, err := agentIPCEnrollmentServer(agentIPCOptions{}, "  https://request.example.test/  ")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://request.example.test/" {
			t.Fatalf("enrollment server = %q, want request server", got)
		}
	})

	t.Run("saved config", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "client.json")
		if err := client.SaveConfig(configPath, client.Config{
			ControlPlaneURLs: []string{"https://saved.example.test/", "https://secondary.example.test/"},
		}); err != nil {
			t.Fatal(err)
		}
		got, err := agentIPCEnrollmentServer(agentIPCOptions{ConfigPath: configPath}, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != "https://saved.example.test" {
			t.Fatalf("enrollment server = %q, want saved primary server", got)
		}
	})

	t.Run("public default", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "client.json")
		got, err := agentIPCEnrollmentServer(agentIPCOptions{ConfigPath: configPath}, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != defaultPublicServerURL {
			t.Fatalf("enrollment server = %q, want public default %q", got, defaultPublicServerURL)
		}
	})
}

func TestAgentIPCEnrollNoTokenReturnsApprovalURL(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	outputPath := filepath.Join(tmp, "endlessnet.conf")
	mapKey := testMapSigningKey(t)
	mapSigningPublicKey := testMapSigningPublicKey(t, testNetworkMapWithRevision(t, mapKey, "net-pending", "node-pending", 1).MapSignature)
	approvalURL := "https://admin.example.test/admin/?enrollment_request=ner_test"
	pollToken := "nep_secret_poll"
	var gotReq clientapi.RegisterNodeRequest
	requestCalls := 0
	approved := false
	completeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			w.WriteHeader(http.StatusOK)
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, mapSigningPublicKey))
		case "/nodes/enrollment-requests":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			requestCalls++
			if auth := r.Header.Get("Authorization"); auth != "" {
				http.Error(w, "authorization header must be empty", http.StatusBadRequest)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(gotReq.JoinToken) != "" {
				http.Error(w, "join token must not be set", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(gotReq.IdempotencyID) == "" {
				http.Error(w, "idempotency key is required", http.StatusBadRequest)
				return
			}
			if err := clientapi.VerifyRegisterNodeIdentityProof(gotReq); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(clientapi.CreateNodeEnrollmentRequestResponse{
				Request: clientapi.NodeEnrollmentRequest{
					ID:          "ner_test",
					Status:      clientapi.NodeEnrollmentRequestPending,
					ApprovalURL: approvalURL,
				},
				PollToken:        pollToken,
				PollAfterSeconds: 1,
			})
		case "/nodes/enrollment-requests/ner_test":
			status := clientapi.NodeEnrollmentRequestPending
			if approved {
				status = clientapi.NodeEnrollmentRequestApproved
			}
			_ = json.NewEncoder(w).Encode(clientapi.NodeEnrollmentRequestStatusResponse{
				Request: clientapi.NodeEnrollmentRequest{ID: "ner_test", Status: status, ApprovalURL: approvalURL},
			})
		case "/nodes/enrollment-requests/ner_test/complete":
			if !approved {
				http.Error(w, "enrollment is not approved", http.StatusConflict)
				return
			}
			completeCalls++
			registration := clientapi.RegisterNodeResponse{
				Network: clientapi.Network{ID: "net-browser", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
				Node: clientapi.Node{
					ID:                "node-browser",
					NetworkID:         "net-browser",
					Hostname:          gotReq.Hostname,
					IdentityPublicKey: gotReq.IdentityPublicKey,
					PublicKey:         gotReq.PublicKey,
					DeviceFingerprint: gotReq.DeviceFingerprint,
					AssignedIP:        "100.64.0.2",
					ApprovalState:     clientapi.NodeApprovalApproved,
				},
				SchemaVersion:       clientapi.SchemaVersion,
				IdempotencyID:       gotReq.IdempotencyID,
				RegistrationBinding: clientapi.RegistrationIdentityProofBinding(gotReq),
			}
			credential, err := clientapi.SignNodeCredential(mapKey, registration.Network.ID, registration.Node.ID, []string{"node:register", "node:map", "node:endpoint", "node:delete"}, time.Now().UTC().Add(time.Hour))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			registration.NodeCredential = credential
			signature, err := clientapi.SignNetworkMap(mapKey, registration)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			registration.MapSignature = signature
			_ = json.NewEncoder(w).Encode(clientapi.CompleteNodeEnrollmentRequestResponse{
				Request:      clientapi.NodeEnrollmentRequest{ID: "ner_test", Status: clientapi.NodeEnrollmentRequestEnrolled, NodeID: registration.Node.ID},
				Registration: &registration,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	allowConfigure := false
	wireGuard := &testAgentWireGuard{configure: func(client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
		if !allowConfigure {
			return client.WireGuardApplyResult{}, errors.New("wireguard-go configure must not run")
		}
		return client.WireGuardApplyResult{OK: true, Method: "wireguard-go", Changed: true}, nil
	}}
	opts := agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  wireGuard,
	}
	handlers := agentIPCHandlers(opts)
	payloadRaw, err := handlers.Enroll(context.Background(), ipc.EnrollRequest{
		Server:   server.URL,
		Hostname: "interactive-ipc",
		Mode:     "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadRaw
	if payload.State != ipc.StateNeedsApproval || payload.ControlState != ipc.ControlStatePendingApproval {
		t.Fatalf("no-token IPC enrollment payload = %#v", payload)
	}
	if payload.ApprovalURL != approvalURL || payload.EnrollmentRequestID != "ner_test" || payload.State != ipc.StateNeedsApproval {
		t.Fatalf("no-token IPC enrollment approval payload = %#v", payload)
	}
	if payload.WireGuardApply != nil {
		t.Fatalf("pending browser enrollment unexpectedly returned apply result: %#v", payload.WireGuardApply)
	}
	if strings.Contains(fmt.Sprint(payload), pollToken) {
		t.Fatalf("no-token IPC payload leaked poll token: %#v", payload)
	}
	if requestCalls != 1 || gotReq.Hostname != "interactive-ipc" || !containsString(gotReq.Tags, "mode:server") {
		t.Fatalf("enrollment request calls=%d request=%#v", requestCalls, gotReq)
	}
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnrollmentRequestID != "ner_test" || cfg.EnrollmentPollToken != pollToken || cfg.ApprovalURL != approvalURL {
		t.Fatalf("saved enrollment request state = %#v", cfg)
	}
	if cfg.EnrollmentRequest == nil || clientapi.RegistrationIdentityProofBinding(*cfg.EnrollmentRequest) != clientapi.RegistrationIdentityProofBinding(gotReq) {
		t.Fatalf("saved enrollment request proof = %#v, want original request binding", cfg.EnrollmentRequest)
	}
	if cfg.NodeID != "" || cfg.NodeCredential != "" || cfg.CachedMap != nil {
		t.Fatalf("no-token approval request persisted node authority before approval: %#v", cfg)
	}
	if strings.Contains(cfg.ApprovalURL, pollToken) {
		t.Fatal("approval URL leaked poll token")
	}
	if wireGuard.configureCalls != 0 {
		t.Fatalf("no-token IPC enrollment configure calls = %d, want 0", wireGuard.configureCalls)
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-token IPC output stat = %v, want absent", err)
	}

	approved = true
	allowConfigure = true
	payloadRaw, err = handlers.Enroll(context.Background(), ipc.EnrollRequest{
		Server:   server.URL,
		Hostname: "interactive-ipc",
		Mode:     "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload = payloadRaw
	if payload.State != ipc.StateConnected || payload.NodeID != "node-browser" {
		t.Fatalf("approved no-token IPC enrollment payload = %#v", payload)
	}
	if payload.WireGuardApply == nil || !payload.WireGuardApply.OK {
		t.Fatalf("approved no-token IPC enrollment apply = %#v, want success", payload.WireGuardApply)
	}
	if requestCalls != 1 || completeCalls != 1 || wireGuard.configureCalls != 1 {
		t.Fatalf("approved enrollment calls: create=%d complete=%d configure=%d", requestCalls, completeCalls, wireGuard.configureCalls)
	}
	cfg, err = client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnrollmentRequest != nil || cfg.EnrollmentRequestID != "" || cfg.EnrollmentPollToken != "" || cfg.NodeID != "node-browser" {
		t.Fatalf("completed browser enrollment state = %#v", cfg)
	}
}

func TestAgentOnlineNetworkMapActivatesRestrictedEnrollmentAfterApproval(t *testing.T) {
	for _, initial := range []string{clientapi.NodeApprovalPending, clientapi.NodeApprovalRejected} {
		t.Run(initial, func(t *testing.T) {

			mapKey := testMapSigningKey(t)
			mapPublicKey := base64.RawURLEncoding.EncodeToString(mapKey.Public().(ed25519.PublicKey))
			approvedMap := clientapi.RegisterNodeResponse{
				Network: clientapi.Network{ID: "net-pending", Name: "default", CIDR: "100.64.0.0/24", Revision: 2},
				Node: clientapi.Node{
					ID:            "node-pending",
					NetworkID:     "net-pending",
					Hostname:      "pending-a",
					PublicKey:     testWireGuardPublicKey("pending-a"),
					AssignedIP:    "100.64.0.2",
					ApprovalState: clientapi.NodeApprovalApproved,
				},
			}
			signature, err := clientapi.SignNetworkMap(mapKey, approvedMap)
			if err != nil {
				t.Fatal(err)
			}
			approvedMap.MapSignature = signature
			var approved atomic.Bool
			var endpointCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/server-key":
					_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, mapPublicKey))
				case "/nodes/node-pending/endpoint":
					endpointCalls.Add(1)
					http.Error(w, "pending client must not publish endpoint", http.StatusInternalServerError)
				case "/maps/node-pending/stream":
					setTestMapStreamResponseHeaders(w)
					if !approved.Load() {
						http.Error(w, "node is pending and cannot receive a network map", http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/x-ndjson")
					_ = json.NewEncoder(w).Encode(testMapStreamSnapshotEvent(t, approvedMap))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tmp := t.TempDir()
			configPath := filepath.Join(tmp, "client.json")
			bundle := testSigningTrustBundle(t, mapPublicKey)
			cfg := client.Config{
				ControlPlaneURLs:  []string{server.URL},
				PrivateKey:        "private-key",
				NodeID:            "node-pending",
				NetworkID:         "net-pending",
				NodeCredential:    "credential-pending",
				NodeApprovalState: initial,
			}
			if err := client.SetSigningTrustBundle(&cfg, *bundle); err != nil {
				t.Fatal(err)
			}
			if err := client.SaveConfig(configPath, cfg); err != nil {
				t.Fatal(err)
			}

			if _, _, _, err := agentOnlineNetworkMap(configPath, time.Second, 0); err == nil || !strings.Contains(err.Error(), "pending") {
				t.Fatalf("pending map poll error = %v, want pending denial", err)
			}
			if endpointCalls.Load() != 0 {
				t.Fatalf("pending map poll endpoint calls = %d, want 0", endpointCalls.Load())
			}
			approved.Store(true)
			updatedCfg, networkMap, unchanged, err := agentOnlineNetworkMap(configPath, time.Second, 0)
			if err != nil {
				t.Fatal(err)
			}
			if unchanged || networkMap.Network.Revision != 2 || updatedCfg.NodeApprovalState != clientapi.NodeApprovalApproved {
				t.Fatalf("approved poll cfg=%#v map=%#v unchanged=%t", updatedCfg, networkMap, unchanged)
			}
			saved, err := client.LoadConfig(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if saved.NodeApprovalState != clientapi.NodeApprovalApproved || saved.CachedMap == nil || saved.MapRevision != 2 {
				t.Fatalf("activated pending config = %#v", saved)
			}

		})
	}
}

func TestConnectAgentTunnelRejectsPendingEnrollmentBeforeApply(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:            "node-pending",
		NetworkID:         "net-pending",
		NodeCredential:    "credential-pending",
		PrivateKey:        "private-key",
		NodeApprovalState: clientapi.NodeApprovalPending,
	}); err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}
	_, err := connectAgentTunnel(context.Background(), agentIPCOptions{
		ConfigPath: configPath,
		WireGuard:  wireGuard,
	})
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != "approval_required" {
		t.Fatalf("pending connect error = %#v, want approval_required", err)
	}
	if wireGuard.configureCalls != 0 {
		t.Fatalf("pending connect configure calls = %d, want 0", wireGuard.configureCalls)
	}
}

func TestPendingApprovalStatusIsNotOverriddenByExpectedPollingError(t *testing.T) {
	payload := serviceIPCStatusForConfig(client.Config{
		NodeID:            "node-pending",
		NetworkID:         "net-pending",
		NodeCredential:    "credential-pending",
		NodeApprovalState: clientapi.NodeApprovalPending,
	})
	attachServiceIPCAgentStatus(&payload, client.AgentSnapshot{
		NodeID:    "node-pending",
		NetworkID: "net-pending",
		LastError: "GET /maps/node-pending/stream failed: node is pending",
	})
	if payload.ControlState != ipc.ControlStatePendingApproval {
		t.Fatalf("pending control state after poll error = %#v", payload)
	}
	if got := payload.State; got != ipc.StateNeedsApproval {
		t.Fatalf("pending service state = %q, want %q", got, ipc.StateNeedsApproval)
	}
}

func TestAgentIPCEnrollConnectsTunnel(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	mapKey := testMapSigningKey(t)
	server, enrollmentSnapshot := testEnrollmentServer(t, mapKey, "enr_test", 7)
	defer server.Close()

	wireGuard := &testAgentWireGuard{}
	syncWake := make(chan struct{}, 1)

	payload, err := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  wireGuard,
		SyncWake:   syncWake,
	}).Enroll(context.Background(), ipc.EnrollRequest{
		EnrollToken:    "enr_test",
		Server:         server.URL,
		Mode:           "interactive",
		Hostname:       "node-ipc",
		IdempotencyKey: "idem-ipc-registration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.NodeID != "node-1" {
		t.Fatalf("enroll payload = %#v", payload)
	}
	if payload.WireGuardApply == nil || !payload.WireGuardApply.OK {
		t.Fatalf("enroll apply = %#v, want success", payload.WireGuardApply)
	}
	req, registerCalls := enrollmentSnapshot()
	if registerCalls != 1 || req.JoinToken != "enr_test" || req.Hostname != "node-ipc" || req.IdempotencyID != "idem-ipc-registration" || !containsString(req.Tags, "mode:interactive") {
		t.Fatalf("register calls=%d request=%#v", registerCalls, req)
	}
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "node-1" || !strings.HasPrefix(cfg.NodeCredential, "enc_") || cfg.CachedMap == nil || cfg.CachedMap.Network.Revision != 7 {
		t.Fatalf("saved enrolled config = %#v", cfg)
	}
	if wireGuard.configureCalls != 1 {
		t.Fatalf("wireguard-go configure calls = %d, want 1", wireGuard.configureCalls)
	}
	select {
	case <-syncWake:
	default:
		t.Fatal("successful enrollment did not wake the background agent")
	}
}

func TestAgentIPCEnrollReturnsSuccessfulApplyWhenControlPlaneIsDegraded(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	mapKey := testMapSigningKey(t)
	server, _ := testEnrollmentServer(t, mapKey, "enr_test", 7)
	healthyHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/readyz" {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		healthyHandler.ServeHTTP(w, r)
	})
	defer server.Close()

	payload, err := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  &testAgentWireGuard{},
	}).Enroll(context.Background(), ipc.EnrollRequest{
		EnrollToken: "enr_test",
		Server:      server.URL,
		Hostname:    "node-ipc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateDegraded || payload.ControlState != ipc.ControlStateDegraded {
		t.Fatalf("degraded enrollment status = %#v", payload.StatusResponse)
	}
	if payload.WireGuardApply == nil || !payload.WireGuardApply.OK {
		t.Fatalf("degraded enrollment apply = %#v, want success", payload.WireGuardApply)
	}
}

func TestAgentIPCEnrollReturnsConfigureFailureWithoutWritingConfig(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	mapKey := testMapSigningKey(t)
	server, _ := testEnrollmentServer(t, mapKey, "enr_test", 7)
	defer server.Close()

	wireGuard := &testAgentWireGuard{configure: func(client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
		return client.WireGuardApplyResult{Method: "wireguard-go"}, errors.New("configure failed")
	}}

	_, err := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  wireGuard,
	}).Enroll(context.Background(), ipc.EnrollRequest{
		EnrollToken: "enr_test",
		Server:      server.URL,
	})
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != "connect_failed" {
		t.Fatalf("Enroll error = %#v, want connect_failed IPC error", err)
	}
}

func TestAgentIPCEnrollRejectsUnsuccessfulConfigureResult(t *testing.T) {
	tmp := t.TempDir()
	setInstallationStateDirForTest(t, filepath.Join(tmp, "installation-state"))
	configPath := filepath.Join(tmp, "client.json")
	mapKey := testMapSigningKey(t)
	server, _ := testEnrollmentServer(t, mapKey, "enr_test", 7)
	defer server.Close()

	wireGuard := &testAgentWireGuard{configure: func(client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
		return client.WireGuardApplyResult{Method: "wireguard-go"}, nil
	}}
	_, err := agentIPCHandlers(agentIPCOptions{
		ConfigPath: configPath,
		ListenPort: 51820,
		WireGuard:  wireGuard,
	}).Enroll(context.Background(), ipc.EnrollRequest{
		EnrollToken: "enr_test",
		Server:      server.URL,
	})
	var ipcErr ipc.Error
	if !errors.As(err, &ipcErr) || ipcErr.Code != "connect_failed" {
		t.Fatalf("Enroll error = %#v, want connect_failed IPC error", err)
	}
}

func TestEnrollmentStatusAfterConnectKeepsConnectionResultWhenHealthUnavailable(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(configPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	connected := ipc.ConnectResponse{
		State:        ipc.StateConnected,
		DesiredState: ipc.DesiredConnected,
		NodeID:       "node-1",
		NetworkID:    "net-1",
		MapRevision:  7,
	}
	status := enrollmentStatusAfterConnect(context.Background(), agentIPCOptions{ConfigPath: configPath}, connected)
	if status.State != ipc.StateError || status.ControlState != ipc.ControlStateError || status.LocalStateError == "" {
		t.Fatalf("fallback enrollment status = %#v", status)
	}
	if status.NodeID != connected.NodeID || status.NetworkID != connected.NetworkID || status.MapRevision != connected.MapRevision {
		t.Fatalf("fallback enrollment status lost connection identity: %#v", status)
	}
}

func TestServiceEnrollViaIPCSendsEnrollmentRequest(t *testing.T) {
	var got ipc.EnrollRequest
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			got = req
			return ipc.EnrollResponse{
				StatusResponse: ipc.StatusResponse{State: ipc.StateConnected, NodeID: "node-1"},
				WireGuardApply: testSuccessfulIPCWireGuardApply(),
			}, nil
		},
	}))
	defer server.Close()
	ipcClient := ipc.NewClient(server.Client())
	ipcClient.BaseURL = server.URL

	payload, err := serviceEnrollViaIPC(context.Background(), ipcClient, "  enr_test  ", "https://api.example.test/", "server", "node-service", "idem-service")
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.NodeID != "node-1" || payload.WireGuardApply == nil || !payload.WireGuardApply.OK {
		t.Fatalf("serviceEnrollViaIPC payload = %#v", payload)
	}
	if got.EnrollToken != "enr_test" || got.Server != "https://api.example.test/" || got.Mode != "server" || got.Hostname != "node-service" || got.IdempotencyKey != "idem-service" {
		t.Fatalf("serviceEnrollViaIPC request = %#v", got)
	}
	got = ipc.EnrollRequest{}
	payload, err = serviceEnrollViaIPC(context.Background(), ipcClient, "", "https://api.example.test/", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.NodeID != "node-1" {
		t.Fatalf("serviceEnrollViaIPC no-token payload = %#v", payload)
	}
	if got.EnrollToken != "" {
		t.Fatalf("serviceEnrollViaIPC no-token request sent enroll_token: %#v", got)
	}
	if got.Server != "https://api.example.test/" {
		t.Fatalf("serviceEnrollViaIPC no-token request server = %#v; request=%#v", got.Server, got)
	}
}

func TestCmdServiceEnrollAllowsNoTokenBeforeIPC(t *testing.T) {
	var got ipc.EnrollRequest
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			got = req
			return ipc.EnrollResponse{StatusResponse: ipc.StatusResponse{State: ipc.StateNeedsEnrollment}}, nil
		},
	}))
	defer server.Close()
	original := newServiceIPCClient
	newServiceIPCClient = func(pipe string) *ipc.Client {
		if pipe != "test-pipe" {
			t.Fatalf("service IPC pipe = %q, want test-pipe", pipe)
		}
		ipc := ipc.NewClient(server.Client())
		ipc.BaseURL = server.URL
		return ipc
	}
	defer func() { newServiceIPCClient = original }()

	out, err := captureStdout(t, func() error {
		return cmdService([]string{"enroll", "--ipc-pipe", "test-pipe", "--server", "https://api.example.test/", "--timeout", "5s"})
	})
	if err != nil {
		t.Fatalf("cmdService enroll failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "service enrollment state: NeedsEnrollment") {
		t.Fatalf("cmdService enroll output = %q", out)
	}
	if got.EnrollToken != "" {
		t.Fatalf("cmdService enroll no-token request sent enroll_token: %#v", got)
	}
	if got.Server != "https://api.example.test/" {
		t.Fatalf("cmdService enroll request server = %#v; request=%#v", got.Server, got)
	}
}

func TestCmdServiceIPCCommandsUseServicePipeFacade(t *testing.T) {
	var selectedNetwork string
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return ipc.StatusResponse{State: ipc.StateConnected}, nil
		},
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			return ipc.ConnectResponse{State: ipc.StateConnected}, nil
		},
		Disconnect: func(ctx context.Context, req ipc.DisconnectRequest) (ipc.DisconnectResponse, error) {
			return ipc.DisconnectResponse{State: ipc.StateDisconnected}, nil
		},
		Logout: func(ctx context.Context, req ipc.LogoutRequest) (ipc.LogoutResponse, error) {
			return ipc.LogoutResponse{State: ipc.StateNeedsEnrollment}, nil
		},
		Networks: func(ctx context.Context, req ipc.NetworksRequest) (ipc.NetworksResponse, error) {
			return ipc.NetworksResponse{Networks: []clientapi.Network{{ID: "net-1", Name: "default"}}}, nil
		},
		SelectNetwork: func(ctx context.Context, req ipc.SelectNetworkRequest) (ipc.SelectNetworkResponse, error) {
			selectedNetwork = req.NetworkID
			return ipc.SelectNetworkResponse{SelectedNetworkID: selectedNetwork}, nil
		},
		Diagnostics: func(ctx context.Context, req ipc.DiagnosticsRequest) (ipc.DiagnosticsResponse, error) {
			return ipc.DiagnosticsResponse{Diagnostics: ipc.Diagnostics{
				GeneratedAt:    "2026-07-18T00:00:00Z",
				LastErrors:     []string{},
				RecentLogs:     []ipc.LogEntry{},
				Interfaces:     []ipc.NetworkInterfaceStatus{},
				RouteConflicts: []ipc.OverlayCIDRConflict{},
			}}, nil
		},
		DiagnosticsBundle: func(ctx context.Context, req ipc.DiagnosticsBundleRequest) (ipc.DiagnosticsBundleResponse, error) {
			return ipc.DiagnosticsBundleResponse{Path: "diagnostics.json", CreatedAt: "2026-07-18T00:00:00Z", ExpiresAt: "2026-07-25T00:00:00Z", SizeBytes: 128}, nil
		},
		RecentLogs: func(ctx context.Context, req ipc.RecentLogsRequest) (ipc.RecentLogsResponse, error) {
			return ipc.RecentLogsResponse{Logs: []ipc.LogEntry{}}, nil
		},
	}))
	defer server.Close()
	original := newServiceIPCClient
	newServiceIPCClient = func(pipe string) *ipc.Client {
		if pipe != "test-pipe" {
			t.Fatalf("service IPC pipe = %q, want test-pipe", pipe)
		}
		ipc := ipc.NewClient(server.Client())
		ipc.BaseURL = server.URL
		return ipc
	}
	defer func() { newServiceIPCClient = original }()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status"}},
		{name: "connect", args: []string{"connect"}},
		{name: "disconnect", args: []string{"disconnect"}},
		{name: "logout", args: []string{"logout"}},
		{name: "networks", args: []string{"networks"}},
		{name: "diagnostics", args: []string{"diagnostics"}},
		{name: "diagnostics bundle", args: []string{"diagnostics-bundle"}},
		{name: "logs", args: []string{"logs-recent"}},
		{name: "select network", args: []string{"select-network", "--network-id", "office"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{}, tc.args...)
			args = append(args, "--ipc-pipe", "test-pipe", "--timeout", "5s")
			out, err := captureStdout(t, func() error { return cmdService(args) })
			if err != nil {
				t.Fatalf("cmdService %v failed: %v\n%s", args, err, out)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("decode service IPC command JSON: %v\n%s", err, out)
			}
			if payload["ipc_protocol"] != ipc.Protocol {
				t.Fatalf("service IPC metadata = %#v", payload)
			}
		})
	}
	if selectedNetwork != "office" {
		t.Fatalf("selected network = %#v, want office", selectedNetwork)
	}
}

func TestCmdServiceEventsStreamsNDJSON(t *testing.T) {
	server := httptest.NewServer(client.NewServiceIPCHandler(client.ServiceIPCHandlers{
		Events: func(ctx context.Context, req ipc.EventsRequest, writer client.ServiceIPCEventWriter) error {
			if err := writer.Send(ipc.Event{EventType: ipc.EventTypeHello, Sequence: 1}); err != nil {
				return err
			}
			return writer.Send(ipc.Event{EventType: ipc.EventTypeStatusChanged, Sequence: 2, Status: &ipc.StatusResponse{State: ipc.StateConnected}})
		},
	}))
	defer server.Close()
	original := newServiceIPCClient
	newServiceIPCClient = func(pipe string) *ipc.Client {
		if pipe != "test-pipe" {
			t.Fatalf("service IPC pipe = %q, want test-pipe", pipe)
		}
		ipc := ipc.NewClient(server.Client())
		ipc.BaseURL = server.URL
		return ipc
	}
	defer func() { newServiceIPCClient = original }()

	out, err := captureStdout(t, func() error {
		return cmdService([]string{"events", "--ipc-pipe", "test-pipe", "--timeout", "5s"})
	})
	if err != nil {
		t.Fatalf("cmdService events failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("events output lines = %d\n%s", len(lines), out)
	}
	var hello map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &hello); err != nil {
		t.Fatalf("decode hello event: %v\n%s", err, lines[0])
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &status); err != nil {
		t.Fatalf("decode status event: %v\n%s", err, lines[1])
	}
	if hello["event_type"] != string(ipc.EventTypeHello) || status["event_type"] != string(ipc.EventTypeStatusChanged) {
		t.Fatalf("events output = %#v %#v", hello, status)
	}
}

func TestCmdServiceRenderSystemdWritesArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	if err := cmdService([]string{
		"render-systemd",
		"--output-dir", outputDir,
		"--binary", "/opt/endlessnet/bin/endlessnet-client",
		"--interval", "9s",
		"--listen-port", "51820",
	}); err != nil {
		t.Fatal(err)
	}

	servicePath := filepath.Join(outputDir, "endlessnet-client.service")
	tmpfilesPath := filepath.Join(outputDir, "endlessnet-client.tmpfiles.conf")
	unit, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	tmpfiles, err := os.ReadFile(tmpfilesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), `"agent" "--config" "/var/lib/endlessnet/client.json"`) {
		t.Fatalf("service unit missing agent command:\n%s", unit)
	}
	if !strings.Contains(string(unit), `"--interval" "9s"`) {
		t.Fatalf("service unit missing interval:\n%s", unit)
	}
	if !strings.Contains(string(unit), `"--listen-port" "51820"`) {
		t.Fatalf("service unit missing listen-port:\n%s", unit)
	}
	if !strings.Contains(string(unit), `"--ipc-socket" "/run/endlessnet/client.sock"`) {
		t.Fatalf("service unit missing ipc-socket:\n%s", unit)
	}
	if !strings.Contains(string(unit), `"--wg-interface" "endlessnet"`) {
		t.Fatalf("service unit missing wg-interface:\n%s", unit)
	}
	if !strings.Contains(string(unit), "StateDirectory=endlessnet") ||
		!strings.Contains(string(unit), "Environment=ENDLESSNET_INSTALLATION_STATE_DIR=/var/lib/endlessnet") {
		t.Fatalf("service unit missing persistent installation state directory:\n%s", unit)
	}
	if !strings.Contains(string(tmpfiles), "d /var/lib/endlessnet 0700 root root -") {
		t.Fatalf("tmpfiles missing config directory:\n%s", tmpfiles)
	}
}

func TestCmdServiceRenderMacOSWritesArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	if err := cmdService([]string{
		"render-macos",
		"--output-dir", outputDir,
		"--binary", "/Library/EndlessNet/endlessnet-client",
		"--interval", "9s",
		"--listen-port", "51820",
	}); err != nil {
		t.Fatal(err)
	}

	plistPath := filepath.Join(outputDir, "ru.endlessnet.client.plist")
	installPath := filepath.Join(outputDir, "ru.endlessnet.client-install.sh")
	uninstallPath := filepath.Join(outputDir, "ru.endlessnet.client-uninstall.sh")
	plist, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	installScript, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatal(err)
	}
	uninstallScript, err := os.ReadFile(uninstallPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>/Library/EndlessNet/endlessnet-client</string>",
		"<string>agent</string>",
		"<string>--config</string>",
		"<string>/Library/Application Support/EndlessNet/client.json</string>",
		"<string>--ipc-socket</string>",
		"<string>/var/run/endlessnet/client.sock</string>",
		"<string>--interval</string>",
		"<string>9s</string>",
		"<string>--listen-port</string>",
		"<string>51820</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Fatalf("launchd plist missing %q:\n%s", want, plist)
		}
	}
	if strings.Contains(string(plist), "--userspace-wireguard") || strings.Contains(string(plist), "--apply-wg-quick") {
		t.Fatalf("launchd plist contains an obsolete WireGuard backend selector:\n%s", plist)
	}
	if !strings.Contains(string(installScript), "launchctl bootstrap system \"$PlistTarget\"") || !strings.Contains(string(installScript), "chown root:wheel \"$PlistTarget\"") {
		t.Fatalf("launchd install script missing install commands:\n%s", installScript)
	}
	if !strings.Contains(string(uninstallScript), "launchctl bootout system \"$PlistTarget\"") || !strings.Contains(string(uninstallScript), "--remove-state") {
		t.Fatalf("launchd uninstall script missing uninstall commands:\n%s", uninstallScript)
	}
}

func TestCmdServiceRenderWindowsWritesArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	if err := cmdService([]string{
		"render-windows",
		"--output-dir", outputDir,
		"--binary", `C:\Program Files\EndlessNet\endlessnet-client.exe`,
		"--interval", "9s",
		"--listen-port", "51820",
	}); err != nil {
		t.Fatal(err)
	}

	installPath := filepath.Join(outputDir, "endlessnet-client-install.ps1")
	uninstallPath := filepath.Join(outputDir, "endlessnet-client-uninstall.ps1")
	installScript, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatal(err)
	}
	uninstallScript, err := os.ReadFile(uninstallPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"New-Service -Name $ServiceName",
		"--windows-service",
		`C:\ProgramData\EndlessNet\client.json`,
		"'--interval'",
		"'9s'",
		"'--ipc-pipe'",
		`\\.\pipe\endlessnet-service`,
		"'--debug'",
		"'--debug-log-dir'",
		`~\.endlessnet\logs`,
		"icacls.exe $StateRoot /inheritance:r",
		"'--listen-port'",
		"'51820'",
	} {
		if !strings.Contains(string(installScript), want) {
			t.Fatalf("install script missing %q:\n%s", want, installScript)
		}
	}
	if strings.Contains(string(installScript), "--output") || strings.Contains(string(installScript), "endlessnet.conf") {
		t.Fatalf("Windows install script contains removed rendered-config plumbing:\n%s", installScript)
	}
	if strings.Contains(string(installScript), "--userspace-wireguard") || strings.Contains(string(installScript), "--apply-wireguard") || strings.Contains(string(installScript), "--wireguard-windows") {
		t.Fatalf("Windows install script contains an obsolete WireGuard backend selector:\n%s", installScript)
	}
	if !strings.Contains(string(uninstallScript), "sc.exe delete $ServiceName") {
		t.Fatalf("uninstall script missing service delete:\n%s", uninstallScript)
	}
}

func TestCmdLogoutLeavesManualExportAndRemovesAgentState(t *testing.T) {
	tmp := t.TempDir()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/nodes/node-1" {
			if got := r.Header.Get("X-EndlessNet-Node-Credential"); got != "credential-1" {
				t.Errorf("node credential header = %q, want credential-1", got)
				http.Error(w, "missing credential", http.StatusUnauthorized)
				return
			}
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	configPath := filepath.Join(tmp, "client.json")
	wgConfigPath := filepath.Join(tmp, "endlessnet.conf")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		LocalOwnerID:     "uid:1000",
		ControlPlaneURLs: []string{server.URL},
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wgConfigPath, []byte("[Interface]\nPrivateKey = secret-private-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"node_id":"node-1","network_id":"net-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := cmdLogout([]string{
		"--config", configPath,
		"--state-output", statePath,
	}); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("logout did not delete the server node")
	}
	if _, err := os.Stat(wgConfigPath); err != nil {
		t.Fatalf("manual export stat err = %v, want preserved", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("logout state path stat err = %v, want not exist", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "credential-1") || strings.Contains(string(raw), "node-1") {
		t.Fatalf("logout did not clear local config: %s", raw)
	}
	stored, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LocalOwnerID != "uid:1000" {
		t.Fatalf("logout local owner = %q, want preserved", stored.LocalOwnerID)
	}
}

func TestCmdLogoutRevokesSessionThroughClientAPI(t *testing.T) {
	tmp := t.TempDir()
	loggedOut := false
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/logout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			t.Errorf("authorization = %q", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		loggedOut = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(control.Close)
	configPath := filepath.Join(tmp, "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{control.URL},
		ManagementURL:    control.URL + "/",
		Token:            "session-token",
	}); err != nil {
		t.Fatal(err)
	}

	if err := cmdLogout([]string{"--config", configPath}); err != nil {
		t.Fatal(err)
	}
	if !loggedOut {
		t.Fatal("logout did not call Client API")
	}
	stored, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "" || stored.ActiveAccountID != "" || stored.ManagementURL != control.URL+"/" {
		t.Fatalf("logout retained session state: %#v", stored)
	}
	if stored.ConnectionIntent == nil || stored.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected || stored.ConnectionIntent.Reason != "local_logout" {
		t.Fatalf("logout connection intent = %#v, want explicit disconnected local_logout", stored.ConnectionIntent)
	}
}

func TestAgentIPCLogoutRemovesAgentState(t *testing.T) {
	tmp := t.TempDir()
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/nodes/node-1" {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		LocalOwnerID:     "uid:1000",
		ControlPlaneURLs: []string{server.URL},
		NodeID:           "node-1",
		NodeCredential:   "credential-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"node_id":"node-1","network_id":"net-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := agentIPCOptions{
		ConfigPath:  configPath,
		StateOutput: statePath,
		WireGuard:   &testAgentWireGuard{},
	}
	if err := agentConnectionIntentStore(opts).SetDisconnected("test"); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCHandlers(opts).Logout(context.Background(), ipc.LogoutRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("IPC logout did not delete the server node")
	}
	if state := payload.State; state != ipc.StateNeedsEnrollment {
		t.Fatalf("IPC logout state = %#v, want NeedsEnrollment", state)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("agent state stat err = %v, want not exist", err)
	}
	stored, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionIntent == nil || stored.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected || stored.ConnectionIntent.Reason != "local_logout" {
		t.Fatalf("connection intent = %#v, want disconnected local_logout", stored.ConnectionIntent)
	}
	if stored.LocalOwnerID != "uid:1000" {
		t.Fatalf("IPC logout local owner = %q, want preserved", stored.LocalOwnerID)
	}
}

func TestSecretFlagValueReadsFileAndRejectsMixedSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "join-token.txt")
	if err := os.WriteFile(path, []byte("  enr_test_secret  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := secretFlagValue("join-token", "", path)
	if err != nil {
		t.Fatal(err)
	}
	if value != "enr_test_secret" {
		t.Fatalf("secretFlagValue = %q, want trimmed token", value)
	}
	if _, err := secretFlagValue("join-token", "inline", path); err == nil || !strings.Contains(err.Error(), "use either") {
		t.Fatalf("mixed secret sources error = %v, want use either", err)
	}
}

func TestSecretFlagValueRejectsUnsafeOrOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := secretFlagValue("token", "", dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory secret source error = %v, want regular-file rejection", err)
	}

	oversized := filepath.Join(dir, "oversized-token.txt")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", maxSecretInputBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := secretFlagValue("token", "", oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized secret source error = %v, want size rejection", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	unsafe := filepath.Join(dir, "unsafe-token.txt")
	if err := os.WriteFile(unsafe, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := secretFlagValue("token", "", unsafe); err == nil || !strings.Contains(err.Error(), "0600 or stricter") {
		t.Fatalf("unsafe secret permissions error = %v, want owner-only rejection", err)
	}
}

func TestRequireLoopbackInlineSessionToken(t *testing.T) {
	for _, rawURL := range []string{
		"http://localhost:8080",
		"https://localhost.",
		"http://127.0.0.1:8080",
		"https://[::1]:8443",
	} {
		if err := requireLoopbackInlineSessionToken([]string{rawURL}); err != nil {
			t.Errorf("requireLoopbackInlineSessionToken(%q): %v", rawURL, err)
		}
	}

	for _, rawURL := range []string{
		"https://api.endlessnet.ru",
		"http://192.0.2.10:8080",
		"https://localhost.example.test",
		"ftp://localhost/token",
		"https://user@localhost",
	} {
		if err := requireLoopbackInlineSessionToken([]string{rawURL}); err == nil {
			t.Errorf("requireLoopbackInlineSessionToken(%q) accepted an unsafe URL", rawURL)
		}
	}
	if err := requireLoopbackInlineSessionToken([]string{"http://localhost", "https://api.endlessnet.ru"}); err == nil {
		t.Fatal("mixed loopback/non-loopback coordinator list accepted an inline session token")
	}
}

func TestLoginWithoutTokenDescribesCookieJoinTokenFlow(t *testing.T) {
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/endlessnet" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"management_url":"https://admin.endlessnet.ru/"}`)
	}))
	t.Cleanup(discovery.Close)
	out, err := captureStdout(t, func() error {
		return cmdLogin([]string{"--server", discovery.URL})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HttpOnly cookie", "one-time node join token", "--join-token-file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("login guidance = %q, want %q", out, want)
		}
	}
	if strings.Contains(out, "paste the returned token") || strings.Contains(out, " --token ") {
		t.Fatalf("login guidance still claims browser session-token export: %q", out)
	}
	if strings.Contains(out, discovery.URL+"/auth/login") || !strings.Contains(out, "https://admin.endlessnet.ru/") {
		t.Fatalf("login guidance did not use discovered management URL: %q", out)
	}
}

func TestManagementDiscoveryRejectsUnsafeOrAmbiguousURLs(t *testing.T) {
	for _, document := range []string{
		`{"management_url":"http://admin.example.test/"}`,
		`{"management_url":"https://user@admin.example.test/"}`,
		`{"management_url":"https://admin.example.test/path"}`,
		`{"management_url":"https://admin.example.test/?token=secret"}`,
		`{"management_url":"https://admin.example.test/"} {}`,
	} {
		t.Run(document, func(t *testing.T) {
			discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, document)
			}))
			t.Cleanup(discovery.Close)
			if _, err := discoverManagementURL(context.Background(), discovery.URL); err == nil {
				t.Fatalf("discovery accepted %s", document)
			}
		})
	}
}

func TestManagementDiscoveryFallsBackAcrossControlPlanes(t *testing.T) {
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(unavailable.Close)
	available := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/endlessnet" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"management_url":"https://admin.endlessnet.ru/"}`)
	}))
	t.Cleanup(available.Close)

	got, err := discoverManagementURLFromControlPlanes(context.Background(), []string{unavailable.URL, available.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://admin.endlessnet.ru/" {
		t.Fatalf("management URL = %q", got)
	}
}

func TestLoginRejectsInlineSessionTokenForRemoteServer(t *testing.T) {
	err := cmdLogin([]string{
		"--server", "https://api.endlessnet.ru",
		"--token", "secret-session-token",
		"--config", filepath.Join(t.TempDir(), "client.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "--token-file") {
		t.Fatalf("remote inline session token error = %v, want token-file guidance", err)
	}
}

func TestServiceStateFromControlState(t *testing.T) {
	for _, tc := range []struct {
		control          ipc.ControlState
		cachedMapInvalid bool
		want             ipc.ServiceState
	}{
		{control: ipc.ControlStateNotRegistered, want: ipc.StateNeedsEnrollment},
		{control: ipc.ControlStateReady, want: ipc.StateConnected},
		{control: ipc.ControlStateRegistered, want: ipc.StateConnected},
		{control: ipc.ControlStatePendingApproval, want: ipc.StateNeedsApproval},
		{control: ipc.ControlStateDegraded, want: ipc.StateDegraded},
		{control: ipc.ControlStateCacheInvalid, want: ipc.StateError},
		{control: ipc.ControlStateError, want: ipc.StateError},
		{control: ipc.ControlStateOfflineCache, want: ipc.StateDegraded},
		{control: ipc.ControlStateDisconnected, want: ipc.StateDisconnected},
		{control: "", want: ipc.StateDisconnected},
		{control: ipc.ControlStateReady, cachedMapInvalid: true, want: ipc.StateError},
	} {
		if got := serviceStateFromControlState(tc.control, tc.cachedMapInvalid); got != tc.want {
			t.Fatalf("serviceStateFromControlState(%q, %t) = %q, want %q", tc.control, tc.cachedMapInvalid, got, tc.want)
		}
	}
}

func TestCacheNetworkMapRedactsNodeCredential(t *testing.T) {
	cfg := client.Config{}
	response := clientapi.RegisterNodeResponse{
		Network:        clientapi.Network{ID: "net-1", Revision: 12},
		Node:           clientapi.Node{ID: "node-1", NetworkID: "net-1"},
		NodeCredential: "secret-node-credential",
		RelayCredential: &relayauth.Credential{
			NetworkID: "net-1",
			NodeID:    "node-1",
			Signature: "secret-relay-credential",
		},
	}

	cacheNetworkMap(&cfg, response)

	if cfg.NodeID != "node-1" || cfg.NetworkID != "net-1" || cfg.MapRevision != 12 {
		t.Fatalf("cached config identity = %#v", cfg)
	}
	if cfg.CachedMap == nil {
		t.Fatal("cached map is nil")
	}
	if cfg.CachedMap.NodeCredential != "" {
		t.Fatalf("cached map leaked node credential: %#v", cfg.CachedMap)
	}
	if cfg.CachedMap.RelayCredential != nil {
		t.Fatalf("cached map leaked relay credential: %#v", cfg.CachedMap)
	}
	if cfg.CachedMapSavedAt == nil || cfg.CachedMapSavedAt.IsZero() {
		t.Fatalf("cached map saved timestamp was not recorded: %#v", cfg.CachedMapSavedAt)
	}
}

func TestCacheNetworkMapCheckedRejectsStaleRevision(t *testing.T) {
	newer := signedTestNetworkMap(t, "net-1", "node-1", 9)
	older := signedTestNetworkMap(t, "net-1", "node-1", 8)
	cfg := client.Config{
		NodeID:      "node-1",
		NetworkID:   "net-1",
		MapRevision: 9,
		CachedMap:   &newer,
	}

	err := cacheNetworkMapChecked(&cfg, older)
	if err == nil || !strings.Contains(err.Error(), "stale network map revision") {
		t.Fatalf("stale map error = %v, want stale revision rejection", err)
	}
	if cfg.MapRevision != 9 || cfg.CachedMap == nil || cfg.CachedMap.Network.Revision != 9 {
		t.Fatalf("stale map mutated cached revision: %#v", cfg)
	}
}

func TestCacheNetworkMapFromEventAppliesSignedDelta(t *testing.T) {
	mapKey := testMapSigningKey(t)
	base := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: 7},
		Node:    clientapi.Node{ID: "node-1", NetworkID: "net-1", Hostname: "node-a", PublicKey: testWireGuardPublicKey("pub-a"), AssignedIP: "100.64.0.2"},
		Peers: []clientapi.Peer{
			{ID: "peer-a", Hostname: "peer-a", PublicKey: testWireGuardPublicKey("pub-peer-a"), AllowedIPs: []string{"100.64.0.3/32"}},
			{ID: "peer-b", Hostname: "peer-b", PublicKey: testWireGuardPublicKey("pub-peer-b"), AllowedIPs: []string{"100.64.0.4/32"}},
		},
		Relays: []relayauth.Endpoint{
			{ID: "relay-a", Addr: "relay-a.example.test:443", Protocol: relayauth.EndpointProtocolTLS},
			{ID: "relay-b", Addr: "relay-b.example.test:443", Protocol: relayauth.EndpointProtocolTLS},
		},
	}
	baseSnapshot := networkMapSnapshotFromResponse(base)
	baseSnapshot.Revision = clientapi.MapRevision{Network: 7}
	baseSignature, err := clientapi.SignNetworkMapSnapshot(mapKey, baseSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	base.MapSignature = baseSignature

	updatedPeer := clientapi.Peer{ID: "peer-a", Hostname: "peer-a", PublicKey: testWireGuardPublicKey("pub-peer-a"), Endpoint: "peer-a.example.test:51820", AllowedIPs: []string{"100.64.0.3/32"}}
	addedPeer := clientapi.Peer{ID: "peer-c", Hostname: "peer-c", PublicKey: testWireGuardPublicKey("pub-peer-c"), Endpoint: "peer-c.example.test:51820", AllowedIPs: []string{"100.64.0.5/32"}}
	updatedRelay := relayauth.Endpoint{ID: "relay-a", Addr: "relay-a-new.example.test:443", Protocol: relayauth.EndpointProtocolTLS}
	addedRelay := relayauth.Endpoint{ID: "relay-c", Addr: "relay-c.example.test:443", Protocol: relayauth.EndpointProtocolTLS}
	resultSnapshot := baseSnapshot
	resultSnapshot.Revision = clientapi.MapRevision{Network: 8}
	resultSnapshot.Network.Revision = 8
	resultSnapshot.Peers = []clientapi.Peer{updatedPeer, addedPeer}
	resultSnapshot.Relays = []relayauth.Endpoint{updatedRelay, addedRelay}
	resultSignature, err := clientapi.SignNetworkMapSnapshot(mapKey, resultSnapshot)
	if err != nil {
		t.Fatal(err)
	}

	cfg := client.Config{NodeID: "node-1", NetworkID: "net-1", MapRevision: 7, MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, baseSignature)), CachedMap: &base}
	got, action, err := cacheNetworkMapFromEvent(&cfg, clientapi.MapStreamEvent{
		Type: "delta", ProtocolVersion: clientapi.MapStreamProtocolVersion, Capabilities: clientapi.MapStreamSupportedCapabilities(), EventID: "event-8",
		From: clientapi.MapRevision{Network: 7}, To: clientapi.MapRevision{Network: 8}, BaseHash: baseSignature.PayloadHash,
		Delta:           &clientapi.MapDelta{Network: &resultSnapshot.Network, PeerUpserts: []clientapi.Peer{updatedPeer, addedPeer}, PeerRemoveIDs: []string{"peer-b"}, RelayUpserts: []relayauth.Endpoint{updatedRelay, addedRelay}, RelayRemoveIDs: []string{"relay-b"}},
		ResultSignature: resultSignature,
	})
	if err != nil {
		t.Fatal(err)
	}
	if action != "delta" || got.Network.Revision != 8 || len(got.Peers) != 2 || got.Peers[0].ID != "peer-a" || got.Peers[1].ID != "peer-c" {
		t.Fatalf("delta cache result action=%s map=%#v", action, got)
	}
	if len(got.Relays) != 2 || got.Relays[0].Addr != "relay-a-new.example.test:443" || got.Relays[1].ID != "relay-c" {
		t.Fatalf("delta cache relays = %#v", got.Relays)
	}
	if cfg.MapRevision != 8 || cfg.MapGlobalRevision != 0 || cfg.MapHash != resultSignature.PayloadHash || cfg.CachedMap == nil || cfg.CachedMap.RelayCredential != nil {
		t.Fatalf("cached config after delta = %#v", cfg)
	}
}
func TestVerifiedCachedNetworkMapRejectsUnsafeCache(t *testing.T) {
	valid := signedTestNetworkMap(t, "net-1", "node-1", 3)
	cfg := client.Config{
		NodeID:          "node-1",
		NetworkID:       "net-1",
		MapRevision:     3,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, valid.MapSignature)),
		CachedMap:       &valid,
	}
	if _, err := verifiedCachedNetworkMap(&cfg); err != nil {
		t.Fatalf("valid cached map rejected: %v", err)
	}

	tampered := cfg
	tamperedMap := valid
	tamperedMap.Node.AssignedIP = "100.64.0.99"
	tampered.CachedMap = &tamperedMap
	if _, err := verifiedCachedNetworkMap(&tampered); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("tampered cache error = %v, want payload hash mismatch", err)
	}

	rollback := cfg
	rollback.MapRevision = 4
	if _, err := verifiedCachedNetworkMap(&rollback); err == nil || !strings.Contains(err.Error(), "does not match local map_revision") {
		t.Fatalf("rollback cache error = %v, want revision mismatch", err)
	}

	foreign := cfg
	foreign.NetworkID = "net-foreign"
	if _, err := verifiedCachedNetworkMap(&foreign); err == nil || !strings.Contains(err.Error(), "does not match local network_id") {
		t.Fatalf("foreign cache error = %v, want network_id mismatch", err)
	}
}

func TestVerifyNetworkMapFailsClosedWithoutConfiguredTrust(t *testing.T) {
	response := signedTestNetworkMap(t, "net-1", "node-1", 3)
	cfg := client.Config{}
	if err := verifyNetworkMap(&cfg, response); err == nil || !strings.Contains(err.Error(), "trust anchor") {
		t.Fatalf("untrusted signed map error = %v, want trust-anchor rejection", err)
	}
	if cfg.MapSigningTrust != nil {
		t.Fatalf("network map self-pinned trust: %#v", cfg)
	}

	trusted := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature))}
	unsigned := response
	unsigned.MapSignature = nil
	if err := verifyNetworkMap(&trusted, unsigned); err == nil || !strings.Contains(err.Error(), "signature is missing") {
		t.Fatalf("unsigned map error = %v, want missing-signature rejection", err)
	}
}

func TestVerifyNetworkMapRejectsSignedCrossIdentitySubstitution(t *testing.T) {
	response := signedTestNetworkMap(t, "net-remote", "node-remote", 3)
	crossNode := client.Config{
		NodeID:          "node-local",
		NetworkID:       "net-remote",
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature)),
	}
	if err := verifyNetworkMap(&crossNode, response); err == nil || !strings.Contains(err.Error(), "does not match local node_id") {
		t.Fatalf("cross-node signed map error = %v", err)
	}
	crossNetwork := client.Config{
		NodeID:          "node-remote",
		NetworkID:       "net-local",
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature)),
	}
	if err := verifyNetworkMap(&crossNetwork, response); err == nil || !strings.Contains(err.Error(), "does not match local network_id") {
		t.Fatalf("cross-network signed map error = %v", err)
	}
	identityPrivateKey, err := client.GenerateIdentityPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	wireGuardPrivateKey, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	initialEnrollment := client.Config{
		MapSigningTrust:    testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature)),
		PrivateKey:         wireGuardPrivateKey,
		IdentityPrivateKey: identityPrivateKey,
		DeviceFingerprint:  "local-device-fingerprint",
	}
	if err := verifyNetworkMap(&initialEnrollment, response); err == nil || !strings.Contains(err.Error(), "does not match local WireGuard public key") {
		t.Fatalf("initial cross-identity signed map error = %v", err)
	}
}

func TestRefreshMapSigningTrustPropagatesFetchFailure(t *testing.T) {
	response := signedTestNetworkMap(t, "net-1", "node-1", 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "signer unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := client.Config{MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, response.MapSignature))}
	if err := refreshMapSigningTrust(&cfg, clientapi.NewAPI(server.URL, "")); err == nil || !strings.Contains(err.Error(), "fetch server signing trust bundle") {
		t.Fatalf("refreshMapSigningTrust error = %v, want fetch failure", err)
	}
}

func TestRefreshMapSigningTrustPersistsPretrustedActiveSwitch(t *testing.T) {
	oldPublic := testMapSigningKey(t).Public().(ed25519.PublicKey)
	newPublic := testMapSigningKey(t).Public().(ed25519.PublicKey)
	oldBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(oldPublic))
	if err != nil {
		t.Fatal(err)
	}
	newBundle, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(newPublic))
	if err != nil {
		t.Fatal(err)
	}
	overlap := clientapi.SigningTrustBundle{
		Version:     clientapi.SigningTrustBundleVersion,
		ActiveKeyID: oldBundle.ActiveKeyID,
		Keys:        []clientapi.SigningTrustKey{oldBundle.Keys[0], newBundle.Keys[0]},
	}
	cfg := client.Config{}
	if err := client.ReplaceSigningTrustBundle(&cfg, overlap); err != nil {
		t.Fatal(err)
	}
	announced := overlap
	announced.ActiveKeyID = newBundle.ActiveKeyID
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(testServerKeyResponseFromBundle(announced))
	}))
	defer server.Close()
	if err := refreshMapSigningTrust(&cfg, clientapi.NewAPI(server.URL, "")); err != nil {
		t.Fatal(err)
	}
	if cfg.MapSigningTrust == nil || cfg.MapSigningTrust.ActiveKeyID != newBundle.ActiveKeyID {
		t.Fatalf("announced pretrusted rotation was not persisted: %#v", cfg)
	}
}

func TestEnrollmentRejectsUntrustedPlaintextControlPlane(t *testing.T) {
	cfg := client.Config{ControlPlaneURLs: []string{"http://clientapi.example.test"}, Token: "session"}
	if err := enrollMapSigningTrust(&cfg, clientapi.NewAPI(cfg.ControlURLs()[0], cfg.Token)); err == nil || !strings.Contains(err.Error(), "requires HTTPS") {
		t.Fatalf("plaintext enrollment error = %v, want HTTPS rejection", err)
	}
}

func TestVerifiedCachedNetworkMapMaxAge(t *testing.T) {
	valid := signedTestNetworkMap(t, "net-1", "node-1", 3)
	savedAt := time.Now().UTC().Add(-2 * time.Second)
	cfg := client.Config{
		NodeID:           "node-1",
		NetworkID:        "net-1",
		MapRevision:      3,
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, valid.MapSignature)),
		CachedMap:        &valid,
		CachedMapSavedAt: &savedAt,
	}
	if _, err := verifiedCachedNetworkMapWithMaxAge(&cfg, time.Minute); err != nil {
		t.Fatalf("fresh cached map rejected: %v", err)
	}
	if _, err := verifiedCachedNetworkMapWithMaxAge(&cfg, time.Second); err == nil || !strings.Contains(err.Error(), "cached network map expired") {
		t.Fatalf("expired cache error = %v, want expiration", err)
	}
	future := time.Now().UTC().Add(10 * time.Minute)
	cfg.CachedMapSavedAt = &future
	if _, err := verifiedCachedNetworkMapWithMaxAge(&cfg, time.Hour); err == nil || !strings.Contains(err.Error(), "timestamp is in the future") {
		t.Fatalf("future cache timestamp error = %v, want future timestamp rejection", err)
	}
	cfg.CachedMapSavedAt = nil
	if _, err := verifiedCachedNetworkMapWithMaxAge(&cfg, time.Minute); err == nil || !strings.Contains(err.Error(), "timestamp is missing") {
		t.Fatalf("missing timestamp error = %v, want missing timestamp", err)
	}
	if _, err := verifiedCachedNetworkMap(&cfg); err != nil {
		t.Fatalf("default cache verification rejected missing timestamp: %v", err)
	}
}

func TestRecentLogBufferRedactsAndLimitsServiceLogs(t *testing.T) {
	logs := newRecentLogBuffer(3)
	_, _ = fmt.Fprintln(logs, "agent started")
	_, _ = fmt.Fprintln(logs, "PrivateKey = secret-private-key")
	_, _ = fmt.Fprintln(logs, `{"node_credential":"secret-node-credential"}`)
	_, _ = fmt.Fprintln(logs, `powershell -Command install -EnrollToken enr_secret_recent_log`)

	payload, err := agentIPCHandlers(agentIPCOptions{RecentLogs: logs}).RecentLogs(context.Background(), ipc.RecentLogsRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	entries := payload.Logs
	if len(entries) != 3 {
		t.Fatalf("recent log count = %d, want bounded 3: %#v", len(entries), entries)
	}
	out, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, secret := range []string{"secret-private-key", "secret-node-credential", "enr_secret_recent_log"} {
		if strings.Contains(text, secret) {
			t.Fatalf("recent logs leaked %q: %s", secret, text)
		}
	}
	for _, want := range []string{"[redacted private key line]", "[redacted node credential line]", "[redacted token line]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("recent logs missing %q: %s", want, text)
		}
	}
}

func TestAgentIPCDiagnosticsIncludesRecentRedactedLogs(t *testing.T) {
	dir := t.TempDir()
	prepareDiagnosticsTestDirectory(t, dir)
	configPath := filepath.Join(dir, "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{"https://api.example.test"},
		Token:            "secret-session-token",
		PrivateKey:       "secret-private-key",
		NodeID:           "node-1",
		NodeCredential:   "secret-node-credential",
	}); err != nil {
		t.Fatal(err)
	}
	logs := newRecentLogBuffer(5)
	_, _ = fmt.Fprintln(logs, "agent ready")
	_, _ = fmt.Fprintln(logs, "PrivateKey = secret-private-key")
	_, _ = fmt.Fprintln(logs, `{"node_credential":"secret-node-credential"}`)
	_, _ = fmt.Fprintln(logs, `powershell -Command install -EnrollToken enr_secret_diagnostics_log`)
	statePath := filepath.Join(dir, "agent-state.json")
	agentState := client.AgentSnapshot{
		GeneratedAt: "2026-07-07T00:00:00Z",
		NodeID:      "node-1",
		STUN:        client.AgentSTUNSnapshot{Error: "join_token=enr_secret_diagnostics_state"},
		Relay:       client.AgentRelaySnapshot{Error: `{"node_credential":"secret-node-credential"}`},
		Apply:       &client.WireGuardApplyResult{UpError: "PrivateKey = secret-private-key"},
	}
	rawState, err := json.Marshal(agentState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, rawState, 0o600); err != nil {
		t.Fatal(err)
	}

	opts := agentIPCOptions{
		ConfigPath:     configPath,
		StateOutput:    statePath,
		DiagnosticsDir: dir,
		RecentLogs:     logs,
	}
	handlers := agentIPCHandlers(opts)
	payload, err := handlers.Diagnostics(context.Background(), ipc.DiagnosticsRequest{LogLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := payload.Diagnostics
	if diagnostics.Runtime.GOOS != runtime.GOOS || diagnostics.Runtime.GOARCH != runtime.GOARCH || diagnostics.Runtime.OS.Name != runtime.GOOS {
		t.Fatalf("diagnostics runtime = %#v", diagnostics.Runtime)
	}
	if !diagnostics.Config.TokenPresent || !diagnostics.Config.PrivateKeyPresent || !diagnostics.Config.NodeCredentialPresent {
		t.Fatalf("diagnostics config presence flags = %#v", diagnostics.Config)
	}
	if diagnostics.Status.Agent == nil || !diagnostics.Status.Agent.StatePresent {
		t.Fatalf("diagnostics agent status = %#v", diagnostics.Status.Agent)
	}
	if len(diagnostics.RecentLogs) != 4 {
		t.Fatalf("diagnostics recent_logs = %#v, want 4 redacted entries", diagnostics.RecentLogs)
	}
	raw, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "diagnostics-*.json")); err != nil || len(matches) != 0 {
		t.Fatalf("GET diagnostics created bundle files: matches=%#v err=%v", matches, err)
	}
	bundle, err := handlers.DiagnosticsBundle(context.Background(), ipc.DiagnosticsBundleRequest{LogLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(bundle.Path) == "" || bundle.SizeBytes <= 0 || bundle.CreatedAt == "" || bundle.ExpiresAt == "" {
		t.Fatalf("diagnostics bundle metadata = %#v", bundle)
	}
	bundleRaw, err := os.ReadFile(bundle.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + "\n" + string(bundleRaw)
	for _, secret := range []string{"secret-session-token", "secret-private-key", "secret-node-credential", "enr_secret_diagnostics_log", "enr_secret_diagnostics_state"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostics recent logs leaked %q: %s", secret, text)
		}
	}
	for _, want := range []string{"agent ready", "[redacted private key line]", "[redacted node credential line]", "[redacted token line]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics recent logs missing %q: %s", want, text)
		}
	}
}

func TestAgentIPCSelectNetworkCurrentNetwork(t *testing.T) {
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:          "node-1",
		NetworkID:       "net-1",
		MapRevision:     7,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:       &networkMap,
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCHandlers(agentIPCOptions{ConfigPath: configPath}).SelectNetwork(context.Background(), ipc.SelectNetworkRequest{NetworkID: "net-1"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.SelectedNetworkID != "net-1" || payload.NodeID != "node-1" {
		t.Fatalf("SelectNetwork payload = %#v", payload)
	}

	payload, err = agentIPCHandlers(agentIPCOptions{ConfigPath: configPath}).SelectNetwork(context.Background(), ipc.SelectNetworkRequest{NetworkName: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.SelectedNetworkID != "net-1" {
		t.Fatalf("SelectNetwork by name payload = %#v", payload)
	}
}

func TestAgentIPCSelectNetworkStableErrors(t *testing.T) {
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:          "node-1",
		NetworkID:       "net-1",
		MapRevision:     7,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:       &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	handler := agentIPCHandlers(agentIPCOptions{ConfigPath: configPath})
	for _, tc := range []struct {
		name   string
		req    ipc.SelectNetworkRequest
		status int
		code   string
	}{
		{name: "missing", req: ipc.SelectNetworkRequest{}, status: http.StatusBadRequest, code: "network_ref_required"},
		{name: "different", req: ipc.SelectNetworkRequest{NetworkID: "net-2"}, status: http.StatusConflict, code: "network_selection_requires_enrollment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handler.SelectNetwork(context.Background(), tc.req)
			var ipcErr ipc.Error
			if !errors.As(err, &ipcErr) || ipcErr.Status != tc.status || ipcErr.Code != tc.code {
				t.Fatalf("SelectNetwork error = %#v, want status=%d code=%s", err, tc.status, tc.code)
			}
		})
	}
}

func TestDiagnosticsPayloadRedactsSecrets(t *testing.T) {
	cfg := client.Config{
		ControlPlaneURLs:   []string{"https://api.example.test"},
		ActiveAccountID:    "acct-1",
		Token:              "secret-token",
		IdentityPrivateKey: "secret-identity-private-key",
		PrivateKey:         "secret-private-key",
		NodeID:             "node-1",
		NodeCredential:     "secret-node-credential",
		MapRevision:        3,
		CachedMap: &clientapi.RegisterNodeResponse{
			Network:        clientapi.Network{Revision: 3},
			Node:           clientapi.Node{ID: "node-1"},
			NodeCredential: "cached-secret-node-credential",
			RelayCredential: &relayauth.Credential{
				Signature: "cached-secret-relay-credential",
			},
		},
	}

	payload := diagnosticsPayload(cfg)
	clientInfo, ok := payload["client"].(map[string]any)
	if !ok || clientInfo["product"] != "EndlessNet Client" || clientInfo["target_os"] != runtime.GOOS || clientInfo["target_arch"] != runtime.GOARCH {
		t.Fatalf("diagnostics client metadata = %#v", payload["client"])
	}
	runtimeInfo, ok := payload["runtime"].(map[string]any)
	if !ok || runtimeInfo["goos"] != runtime.GOOS || runtimeInfo["goarch"] != runtime.GOARCH {
		t.Fatalf("diagnostics runtime metadata = %#v", payload["runtime"])
	}
	osInfo, ok := runtimeInfo["os"].(map[string]any)
	if !ok || strings.TrimSpace(fmt.Sprint(osInfo["name"])) == "" {
		t.Fatalf("diagnostics OS metadata = %#v", runtimeInfo["os"])
	}
	status, ok := payload["status"].(map[string]any)
	if !ok || status["node_id"] != "node-1" || status["map_revision"] != float64(3) {
		t.Fatalf("diagnostics status summary = %#v", payload["status"])
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, `"identity_private_key_present":true`) {
		t.Fatalf("diagnostics payload missing identity private key presence flag: %s", out)
	}
	for _, secret := range []string{"secret-token", "secret-identity-private-key", "secret-private-key", "secret-node-credential", "cached-secret-node-credential", "cached-secret-relay-credential"} {
		if strings.Contains(out, secret) {
			t.Fatalf("diagnostics payload leaked %q: %s", secret, out)
		}
	}
}

func TestDiagnosticsPayloadIncludesSupportSummaries(t *testing.T) {
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 9)
	networkMap.Network.Name = "prod"
	networkMap.Network.IPv6CIDR = "fd7a:115c:a1e0::/64"
	networkMap.Network.DNS = []string{"100.64.0.1", "fd7a:115c:a1e0::1"}
	networkMap.Network.DNSConfig = &clientapi.DNSConfig{
		MagicDNSEnabled: true,
		Suffix:          "nodes.prod.example",
		SearchDomains:   []string{"nodes.prod.example", "corp.example"},
		Nameservers: []clientapi.DNSNameserver{
			{ID: "global", Address: "100.64.0.1", Scope: "global", Priority: 10},
			{ID: "split", Address: "100.64.0.53", Scope: "split", Priority: 20, SplitDomains: []string{"corp.example"}},
		},
	}
	networkMap.Node.AssignedIPv6 = "fd7a:115c:a1e0::2"
	networkMap.Peers = []clientapi.Peer{
		{
			ID:         "peer-a",
			Hostname:   "app",
			PublicKey:  testWireGuardPublicKey("pub-app"),
			AllowedIPs: []string{"100.64.0.3/32", "fd7a:115c:a1e0::3/128", "10.8.0.0/24"},
		},
		{
			ID:         "peer-b",
			Hostname:   "exit",
			PublicKey:  testWireGuardPublicKey("pub-exit"),
			AllowedIPs: []string{"0.0.0.0/0"},
		},
	}
	signature, err := clientapi.SignNetworkMap(testMapSigningKey(t), networkMap)
	if err != nil {
		t.Fatal(err)
	}
	networkMap.MapSignature = signature
	cfg := client.Config{
		ControlPlaneURLs:    []string{"https://api.example.test"},
		NodeID:              "node-1",
		NetworkID:           "net-1",
		MapRevision:         9,
		MapSigningTrust:     testSigningTrustBundle(t, testMapSigningPublicKey(t, signature)),
		WireGuardRouteTable: "51820",
		CachedMap:           &networkMap,
	}

	payload := diagnosticsPayload(cfg)
	dnsSummary, ok := payload["dns_summary"].(map[string]any)
	if !ok || dnsSummary["search_domain"] != "nodes.prod.example" || dnsSummary["record_count"] != 2 || dnsSummary["config_present"] != true {
		t.Fatalf("diagnostics DNS summary = %#v", payload["dns_summary"])
	}
	routeSummary, ok := payload["route_summary"].(map[string]any)
	if !ok || routeSummary["allowed_ip_count"] != 4 || routeSummary["subnet_route_count"] != 2 || routeSummary["default_route_present"] != true {
		t.Fatalf("diagnostics route summary = %#v", payload["route_summary"])
	}
	if routeSummary["wireguard_route_table"] != "51820" || routeSummary["overlay_ipv6_cidr"] != "fd7a:115c:a1e0::/64" {
		t.Fatalf("diagnostics route metadata = %#v", routeSummary)
	}
	status, ok := payload["status"].(map[string]any)
	if !ok || status["control_state"] != "ready" || status["map_revision"] != float64(9) {
		t.Fatalf("diagnostics status summary = %#v", payload["status"])
	}
	if lastErrors, ok := payload["last_errors"].([]string); !ok || len(lastErrors) != 0 {
		t.Fatalf("diagnostics last_errors = %#v, want empty []string", payload["last_errors"])
	}
	typed := serviceIPCDiagnosticsPayload(cfg, serviceIPCStatusForConfig(cfg), nil)
	if typed.DNSSummary == nil || typed.DNSSummary.SearchDomain != "nodes.prod.example" || typed.DNSSummary.RecordCount != 2 || !typed.DNSSummary.ConfigPresent || !typed.DNSSummary.MagicDNSEnabled || len(typed.DNSSummary.SplitDomains) != 1 {
		t.Fatalf("typed diagnostics DNS summary = %#v", typed.DNSSummary)
	}
	if typed.RouteSummary == nil || typed.RouteSummary.AllowedIPCount != 4 || typed.RouteSummary.SubnetRouteCount != 2 || !typed.RouteSummary.DefaultRoutePresent {
		t.Fatalf("typed diagnostics route summary = %#v", typed.RouteSummary)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{"nodes.prod.example", "corp.example", "100.64.0.1", "100.64.0.53", "10.8.0.0/24", "0.0.0.0/0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diagnostics payload missing %q: %s", want, out)
		}
	}
}

func TestDiagnosticsStoreWritesRedactedPayload(t *testing.T) {
	dir := t.TempDir()
	prepareDiagnosticsTestDirectory(t, dir)
	payload := diagnosticsPayload(client.Config{
		ControlPlaneURLs:   []string{"https://api.example.test"},
		Token:              "secret-token",
		IdentityPrivateKey: "secret-identity-private-key",
		PrivateKey:         "secret-private-key",
		NodeID:             "node-1",
		NodeCredential:     "secret-node-credential",
	})
	payload["unexpected"] = map[string]any{
		"private_key": "unexpected-secret-private-key",
		"message":     "join_token=enr_secret_diagnostics_bundle",
	}
	bundle, err := newDiagnosticsStore(dir).Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(bundle.Path) != dir {
		t.Fatalf("bundle path = %s, want under %s", bundle.Path, dir)
	}
	raw, err := os.ReadFile(bundle.Path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, `"node_credential_present": true`) {
		t.Fatalf("diagnostics bundle missing redacted presence flags: %s", out)
	}
	for _, secret := range []string{"secret-token", "secret-identity-private-key", "secret-private-key", "secret-node-credential", "unexpected-secret-private-key", "enr_secret_diagnostics_bundle"} {
		if strings.Contains(out, secret) {
			t.Fatalf("diagnostics bundle leaked %q: %s", secret, out)
		}
	}
}

func TestCmdDiagnosticsSanitizesAgentStateSecrets(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "client.json")
	statePath := filepath.Join(dir, "agent-state.json")
	outputPath := filepath.Join(dir, "diagnostics.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{"https://api.example.test"},
		Token:            "secret-session-token",
		PrivateKey:       "secret-private-key",
		NodeCredential:   "secret-node-credential",
	}); err != nil {
		t.Fatal(err)
	}
	agentState := client.AgentSnapshot{
		GeneratedAt: "2026-07-07T00:00:00Z",
		NodeID:      "node-1",
		STUN:        client.AgentSTUNSnapshot{Error: "join_token=enr_secret_cli_diagnostics"},
		Relay:       client.AgentRelaySnapshot{Error: `{"node_credential":"secret-node-credential"}`},
		Apply:       &client.WireGuardApplyResult{UpError: "PrivateKey = secret-private-key"},
	}
	rawState, err := json.Marshal(agentState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, rawState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdDiagnostics([]string{"--config", configPath, "--agent-state", statePath, "--output", outputPath}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"secret-session-token", "secret-private-key", "secret-node-credential", "enr_secret_cli_diagnostics"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostics CLI output leaked %q: %s", secret, text)
		}
	}
	for _, want := range []string{"[redacted private key line]", "[redacted node credential line]", "[redacted token line]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics CLI output missing %q: %s", want, text)
		}
	}
}

func TestDiagnosticsPayloadWithAgentStateRedactsLocalSecrets(t *testing.T) {
	cfg := client.Config{
		ControlPlaneURLs:   []string{"https://api.example.test"},
		Token:              "secret-token",
		IdentityPrivateKey: "secret-identity-private-key",
		PrivateKey:         "secret-private-key",
		NodeID:             "node-1",
		NodeCredential:     "secret-node-credential",
		MapRevision:        3,
	}
	agentState := client.AgentSnapshot{
		GeneratedAt: "2026-06-24T00:00:00Z",
		NodeID:      "node-1",
		NetworkID:   "net-1",
		MapRevision: 3,
		STUN: client.AgentSTUNSnapshot{
			Error: "stun failed",
		},
		Relay: client.AgentRelaySnapshot{
			Error: "relay failed",
			Selected: &relayauth.Endpoint{
				ID:       "relay-1",
				Addr:     "127.0.0.1:8443",
				Protocol: relayauth.EndpointProtocolTLS,
			},
		},
		WireGuard: &client.WireGuardInspection{
			Error: "wireguard failed",
		},
		Apply: &client.WireGuardApplyResult{
			UpError: "apply failed",
		},
	}

	raw, err := json.Marshal(diagnosticsPayloadWithAgentState(cfg, &agentState))
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, "relay-1") {
		t.Fatalf("diagnostics payload missing agent state: %s", out)
	}
	if !strings.Contains(out, `"last_errors":["stun failed","relay failed","wireguard failed","apply failed"]`) {
		t.Fatalf("diagnostics payload missing agent last errors: %s", out)
	}
	for _, secret := range []string{"secret-token", "secret-identity-private-key", "secret-private-key", "secret-node-credential"} {
		if strings.Contains(out, secret) {
			t.Fatalf("diagnostics payload leaked %q: %s", secret, out)
		}
	}
}

func TestParseRelayMetrics(t *testing.T) {
	metrics := parseRelayMetrics(`# HELP endlessnet_relay_sessions_active Currently active relay sessions.
endlessnet_relay_sessions_active 2
endlessnet_relay_sessions_total 6
endlessnet_relay_sessions_revoked_total 1
endlessnet_relay_auth_failures_total 2
endlessnet_relay_frames_total{direction="inbound"} 3
endlessnet_relay_frames_total{direction="outbound"} 2
endlessnet_relay_bytes_total{direction="inbound"} 41
endlessnet_relay_bytes_total{direction="outbound"} 27
endlessnet_relay_drops_total{reason="invalid"} 2
endlessnet_relay_drops_total{reason="no_peer"} 1
endlessnet_relay_drops_total{reason="write_failed"} 0
endlessnet_relay_drops_total{reason="slow_consumer"} 0
endlessnet_relay_drops_total{reason="bandwidth"} 3
endlessnet_relay_slow_consumers_total 0
endlessnet_relay_draining 0
`)
	for key, want := range map[string]float64{
		"sessions_active":        2,
		"sessions_total":         6,
		"sessions_revoked_total": 1,
		"auth_failures_total":    2,
		"frames_inbound_total":   3,
		"frames_outbound_total":  2,
		"bytes_inbound_total":    41,
		"bytes_outbound_total":   27,
		"drops_invalid_total":    2,
		"drops_no_peer_total":    1,
		"drops_bandwidth_total":  3,
		"draining":               0,
	} {
		if metrics[key] != want {
			t.Fatalf("relay metric %s = %v, want %v in %#v", key, metrics[key], want, metrics)
		}
	}
}

func TestStatusPayloadReportsCachedMapWithoutSecrets(t *testing.T) {
	cached := signedTestNetworkMap(t, "net-1", "node-1", 7)
	cached.Network.Name = "default"
	cached.Node.AssignedIP = "100.64.0.2"
	cached.Node.AssignedIPv6 = "fd7a:115c:a1e0::2"
	cached.Peers = []clientapi.Peer{{
		ID:         "peer-1",
		Hostname:   "peer-a",
		PublicKey:  testWireGuardPublicKey("peer-public"),
		AllowedIPs: []string{"100.64.0.3/32", "fd7a:115c:a1e0::3/128"},
	}}
	cached.STUNEndpoints = []clientapi.STUNEndpoint{{ID: "stun-1", Addr: "127.0.0.1:3478"}}
	cached.Network.AccountID = "acct-map"
	cached.Relays = []relayauth.Endpoint{
		{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS},
		{ID: "relay-tls-1", Addr: "127.0.0.1:9443", Protocol: relayauth.EndpointProtocolTLS, Priority: 40},
	}
	signature, err := clientapi.SignNetworkMap(testMapSigningKey(t), cached)
	if err != nil {
		t.Fatal(err)
	}
	cached.MapSignature = signature
	cfg := client.Config{
		ControlPlaneURLs:   []string{"https://api.example.test"},
		ActiveAccountID:    "acct-1",
		Token:              "secret-token",
		IdentityPrivateKey: "secret-identity-private-key",
		PrivateKey:         "secret-private-key",
		NodeID:             "node-1",
		NetworkID:          "net-1",
		NodeCredential:     "secret-node-credential",
		MapSigningTrust:    testSigningTrustBundle(t, testMapSigningPublicKey(t, signature)),
		MapRevision:        7,
		CachedMap:          &cached,
	}

	status := statusPayload(cfg)
	if status["identity_private_key_present"] != true {
		t.Fatalf("status identity_private_key_present = %#v, want true", status["identity_private_key_present"])
	}
	if status["control_state"] != "ready" || status["overlay_ip"] != "100.64.0.2" || status["overlay_ipv6"] != "fd7a:115c:a1e0::2" || status["peer_count"] != float64(1) {
		t.Fatalf("status payload = %#v", status)
	}
	if status["account_id"] != "acct-1" || status["hostname"] != "node-a" {
		t.Fatalf("status overview fields = account_id:%#v hostname:%#v", status["account_id"], status["hostname"])
	}
	cfg.ActiveAccountID = ""
	status = statusPayload(cfg)
	if status["account_id"] != "acct-map" {
		t.Fatalf("status account fallback = %#v, want acct-map", status["account_id"])
	}
	if relays, ok := status["relay_endpoints"].([]any); !ok || len(relays) != 2 {
		t.Fatalf("status relay_endpoints = %#v, want 2 entries", status["relay_endpoints"])
	}
	if stun, ok := status["stun_endpoints"].([]any); !ok || len(stun) != 1 {
		t.Fatalf("status stun_endpoints = %#v, want 1 entry", status["stun_endpoints"])
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, want := range []string{`"protocol":"relay-v1-tls"`, `"priority":40`, "127.0.0.1:9443", "127.0.0.1:3478"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status payload missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, `"tier"`) {
		t.Fatalf("status payload contains removed relay tier: %s", out)
	}
	for _, secret := range []string{"secret-token", "secret-identity-private-key", "secret-private-key", "secret-node-credential"} {
		if strings.Contains(out, secret) {
			t.Fatalf("status payload leaked %q: %s", secret, out)
		}
	}
}

func TestAttachControlAvailabilityMarksDegradedWhenReadyzUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("control probe path = %s, want /client/readyz", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("storage unavailable"))
	}))
	defer server.Close()

	status := map[string]any{
		"control_state":    "ready",
		"cached_map_valid": true,
	}
	attachControlAvailability(context.Background(), status, server.URL)
	if fmt.Sprint(status["control_state"]) != "degraded" {
		t.Fatalf("control_state = %#v, want degraded: %#v", status["control_state"], status)
	}
	control, ok := status["control"].(map[string]any)
	if !ok || control["ok"] != false || control["http_status"] != http.StatusServiceUnavailable {
		t.Fatalf("control probe = %#v", status["control"])
	}
	if !strings.Contains(fmt.Sprint(control["error"]), "503") {
		t.Fatalf("control probe error = %#v", control["error"])
	}
}

func TestAgentIPCStatusReportsDegradedWhenControlPlaneUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("control probe path = %s, want /client/readyz", r.URL.Path)
		}
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "credential-1",
		MapRevision:      7,
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:        &networkMap,
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCStatus(context.Background(), agentIPCOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if payload.ControlState != ipc.ControlStateDegraded || payload.State != ipc.StateDegraded {
		t.Fatalf("IPC status = %#v, want degraded control and service state", payload)
	}
	if payload.Control == nil || payload.Control.OK || payload.Control.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("IPC status control probe = %#v", payload.Control)
	}
}

func TestAgentIPCStatusIgnoresSnapshotFromPreviousEnrollment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("control probe path = %s, want /client/readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "credential-1",
		MapRevision:      7,
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:        &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := json.Marshal(client.AgentSnapshot{
		GeneratedAt: "2026-07-22T23:10:33Z",
		LastError:   "node identity is missing; run up first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCStatus(context.Background(), agentIPCOptions{
		ConfigPath:  configPath,
		StateOutput: statePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.State != ipc.StateConnected || payload.ControlState != ipc.ControlStateReady {
		t.Fatalf("IPC status with stale snapshot = %#v, want connected/ready", payload)
	}
	if payload.Agent == nil || payload.Agent.StatePresent || payload.Agent.LastError != "" {
		t.Fatalf("stale agent snapshot was exposed: %#v", payload.Agent)
	}
}

func TestAgentIPCStatusExposesPreviousSnapshotDuringMapRevisionUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("control probe path = %s, want /client/readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	mapKey := testMapSigningKey(t)
	networkMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", 8)
	networkMap.Peers = []clientapi.Peer{{
		ID:         "peer-latvia",
		Hostname:   "latvia",
		PublicKey:  testWireGuardPublicKey("peer-latvia"),
		AllowedIPs: []string{"100.64.0.3/32"},
	}}
	signature, err := clientapi.SignNetworkMap(mapKey, networkMap)
	if err != nil {
		t.Fatal(err)
	}
	networkMap.MapSignature = signature

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		ControlPlaneURLs: []string{server.URL},
		NodeID:           "node-1",
		NetworkID:        "net-1",
		NodeCredential:   "credential-1",
		MapRevision:      8,
		MapSigningTrust:  testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:        &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	previous := client.AgentSnapshot{
		GeneratedAt: "2026-07-26T16:47:01Z",
		NodeID:      "node-1",
		NetworkID:   "net-1",
		NetworkName: "default",
		MapRevision: 7,
		PeerCount:   1,
		Paths: []client.PeerPathStatus{{
			PeerID:       "peer-latvia",
			Hostname:     "latvia",
			SelectedPath: "direct",
		}},
	}
	raw, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCStatus(context.Background(), agentIPCOptions{
		ConfigPath:  configPath,
		StateOutput: statePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.MapRevision != 8 || payload.PeerCount != 1 {
		t.Fatalf("authoritative status revision/peers = %d/%d, want 8/1: %#v", payload.MapRevision, payload.PeerCount, payload)
	}
	if payload.Agent == nil || !payload.Agent.StatePresent {
		t.Fatalf("previous agent snapshot is absent: %#v", payload.Agent)
	}
	if payload.Agent.SnapshotState != ipc.AgentSnapshotPrevious ||
		payload.Agent.MapRevision != 7 ||
		payload.Agent.TargetMapRevision != 8 ||
		payload.Agent.PeerCount != 1 {
		t.Fatalf("previous agent snapshot revision state = %#v", payload.Agent)
	}
	if len(payload.Agent.Peers) != 1 || payload.Agent.Peers[0].Hostname != "latvia" {
		t.Fatalf("previous agent peers = %#v, want latvia", payload.Agent.Peers)
	}

	offlineConfig, err := client.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	offlineConfig.ControlPlaneURLs = nil
	offlinePayload := agentIPCStatusForConfig(context.Background(), agentIPCOptions{
		ConfigPath: configPath,
	}, offlineConfig, &previous)
	if offlinePayload.ControlState != ipc.ControlStateOfflineCache ||
		offlinePayload.State != ipc.StateDegraded ||
		offlinePayload.Agent == nil ||
		offlinePayload.Agent.SnapshotState != ipc.AgentSnapshotPrevious {
		t.Fatalf("offline-cache previous snapshot status = %#v", offlinePayload)
	}

	future := previous
	future.MapRevision = 9
	raw, err = json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err = agentIPCStatus(context.Background(), agentIPCOptions{
		ConfigPath:  configPath,
		StateOutput: statePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Agent == nil || payload.Agent.StatePresent || payload.Agent.SnapshotState != ipc.AgentSnapshotAbsent {
		t.Fatalf("future agent snapshot was exposed: %#v", payload.Agent)
	}
}

func TestAgentIPCStatusReportsServerIdentityChangeRecoveryState(t *testing.T) {
	networkMap := signedTestNetworkMap(t, "net-1", "node-1", 7)
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "client.json")
	statePath := filepath.Join(tmp, "agent-state.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:          "node-1",
		NetworkID:       "net-1",
		NodeCredential:  "credential-1",
		MapRevision:     7,
		MapSigningTrust: testSigningTrustBundle(t, testMapSigningPublicKey(t, networkMap.MapSignature)),
		CachedMap:       &networkMap,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentFailureSnapshot(statePath, configPath, errors.New(serverMapSigningTrustChangedError)); err != nil {
		t.Fatal(err)
	}
	payload, err := agentIPCStatus(context.Background(), agentIPCOptions{ConfigPath: configPath, StateOutput: statePath})
	if err != nil {
		t.Fatal(err)
	}
	if payload.ControlState != ipc.ControlStateServerIdentityChanged || payload.State != ipc.StateServerIdentityChanged || payload.Recovery == nil || payload.Recovery.State != ipc.StateServerIdentityChanged {
		t.Fatalf("IPC server identity recovery state = %#v", payload)
	}
}

func TestAgentIPCStatusIncludesVersionedContractMetadata(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:         "node-1",
		NetworkID:      "net-1",
		NodeCredential: "credential-1",
	}); err != nil {
		t.Fatal(err)
	}

	payload, err := agentIPCStatus(context.Background(), agentIPCOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if payload.IPCProtocol != ipc.Protocol || payload.IPCVersion != ipc.Version || payload.IPCMinSupported != ipc.MinSupportedVersion {
		t.Fatalf("IPC metadata = %#v", payload)
	}
	if payload.ServiceVersion != version || payload.ServiceCommit != commit || payload.ServiceBuildDate != buildDate {
		t.Fatalf("service build metadata = %#v", payload)
	}
	if payload.DesiredState != client.ConnectionIntentDesiredConnected {
		t.Fatalf("desired_state = %#v, want connected; payload=%#v", payload.DesiredState, payload)
	}
}

func TestAgentIPCEventsStreamSendsHelloAndStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configPath := filepath.Join(t.TempDir(), "client.json")
	if err := client.SaveConfig(configPath, client.Config{
		NodeID:         "node-1",
		NetworkID:      "net-1",
		NodeCredential: "credential-1",
	}); err != nil {
		t.Fatal(err)
	}
	writer := &capturingServiceIPCEventWriter{cancel: cancel}
	if err := streamAgentIPCEvents(ctx, agentIPCOptions{ConfigPath: configPath}, writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 2 {
		t.Fatalf("events = %#v, want hello and status", writer.events)
	}
	if writer.events[0].EventType != ipc.EventTypeHello || writer.events[0].IPCProtocol != ipc.Protocol {
		t.Fatalf("hello event = %#v", writer.events[0])
	}
	statusEvent := writer.events[1]
	if statusEvent.EventType != ipc.EventTypeStatusChanged {
		t.Fatalf("status event = %#v", statusEvent)
	}
	if statusEvent.Status == nil || statusEvent.Status.State != ipc.StateConnected || statusEvent.Status.DesiredState != client.ConnectionIntentDesiredConnected {
		t.Fatalf("status event status = %#v", statusEvent.Status)
	}
}

type capturingServiceIPCEventWriter struct {
	cancel func()
	events []ipc.Event
}

func (w *capturingServiceIPCEventWriter) Send(value ipc.Event) error {
	w.events = append(w.events, value)
	if len(w.events) >= 2 && w.cancel != nil {
		w.cancel()
	}
	return nil
}

func TestAttachControlAvailabilityKeepsReadyWhenReadyzOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("control probe path = %s, want /client/readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	status := map[string]any{
		"control_state":    "ready",
		"cached_map_valid": true,
	}
	attachControlAvailability(context.Background(), status, server.URL)
	if status["control_state"] != "ready" {
		t.Fatalf("control_state = %#v, want ready: %#v", status["control_state"], status)
	}
	control, ok := status["control"].(map[string]any)
	if !ok || control["ok"] != true || control["http_status"] != http.StatusOK {
		t.Fatalf("control probe = %#v", status["control"])
	}
}

func TestAttachControlAvailabilityUsesFallbackCoordinator(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("primary control probe path = %s, want /client/readyz", r.URL.Path)
		}
		http.Error(w, "primary unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/readyz" {
			t.Fatalf("fallback control probe path = %s, want /client/readyz", r.URL.Path)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer fallback.Close()

	status := map[string]any{
		"control_state":    "ready",
		"cached_map_valid": true,
	}
	attachControlAvailability(context.Background(), status, primary.URL, fallback.URL)
	if status["control_state"] != "ready" {
		t.Fatalf("control_state = %#v, want ready via fallback: %#v", status["control_state"], status)
	}
	control, ok := status["control"].(map[string]any)
	if !ok || control["ok"] != true || control["http_status"] != http.StatusOK || control["url"] != fallback.URL+"/client/readyz" {
		t.Fatalf("control probe = %#v, want fallback client readiness success", status["control"])
	}
	attempts, ok := control["attempts"].([]map[string]any)
	if !ok || len(attempts) != 2 || attempts[0]["http_status"] != http.StatusServiceUnavailable || attempts[1]["http_status"] != http.StatusOK {
		t.Fatalf("control attempts = %#v, want primary failure and fallback success", control["attempts"])
	}
}

func TestStatusPayloadIncludesAgentStateWithoutSecrets(t *testing.T) {
	status := statusPayload(client.Config{
		ControlPlaneURLs: []string{"https://api.example.test"},
		Token:            "secret-token",
		PrivateKey:       "secret-private-key",
		NodeID:           "node-1",
		NodeCredential:   "secret-node-credential",
	})
	attachAgentStatus(status, client.AgentSnapshot{
		GeneratedAt: "2026-06-24T00:00:00Z",
		NodeID:      "node-1",
		NetworkID:   "net-1",
		NetworkName: "default",
		OverlayIP:   "100.64.0.2",
		OverlayIPv6: "fd7a:115c:a1e0::2",
		MapRevision: 9,
		PeerCount:   1,
		STUN:        client.AgentSTUNSnapshot{OK: true},
		Relay: client.AgentRelaySnapshot{
			OK: true,
			Selected: &relayauth.Endpoint{
				ID:       "relay-2",
				Addr:     "127.0.0.1:9443",
				Protocol: relayauth.EndpointProtocolTLS,
			},
			Attempts: []client.RelayDialAttempt{{ID: "relay-1"}, {ID: "relay-2"}},
		},
		Paths: []client.PeerPathStatus{{
			Hostname:     "peer-a",
			SelectedPath: "relay",
		}},
	})
	agent, ok := status["agent"].(map[string]any)
	if !ok || agent["state_present"] != true || agent["stun_ok"] != true || agent["relay_ok"] != true {
		t.Fatalf("agent status = %#v", status["agent"])
	}
	if agent["relay_attempt_count"] != 2 || agent["path_count"] != 1 {
		t.Fatalf("agent counts = %#v", agent)
	}
	if agent["overlay_ipv6"] != "fd7a:115c:a1e0::2" {
		t.Fatalf("agent overlay_ipv6 = %#v", agent)
	}
	selectedRelay, ok := agent["selected_relay"].(map[string]string)
	if !ok || selectedRelay["protocol"] != relayauth.EndpointProtocolTLS || selectedRelay["addr"] != "127.0.0.1:9443" {
		t.Fatalf("selected relay = %#v", agent["selected_relay"])
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, secret := range []string{"secret-token", "secret-private-key", "secret-node-credential"} {
		if strings.Contains(out, secret) {
			t.Fatalf("status payload leaked %q: %s", secret, out)
		}
	}
}

func TestAttachAgentStatusKeepsUnenrolledSnapshotAsNeedsEnrollment(t *testing.T) {
	status := serviceIPCStatusForConfig(client.Config{})
	attachServiceIPCAgentStatus(&status, client.AgentSnapshot{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		LastError:   "server URL is required; run up or login first",
	})
	if status.ControlState != ipc.ControlStateNotRegistered {
		t.Fatalf("control_state = %q, want not_registered; status=%#v", status.ControlState, status)
	}
	if got := status.State; got != ipc.StateNeedsEnrollment {
		t.Fatalf("service state = %q, want NeedsEnrollment; status=%#v", got, status)
	}
	if status.Agent == nil || !strings.Contains(status.Agent.LastError, "server URL is required") {
		t.Fatalf("agent status missing last_error: %#v", status)
	}
}

func TestAttachLivePathStatusIncludesDirectRTT(t *testing.T) {
	payload := map[string]any{}
	networkMap := clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  testWireGuardPublicKey("peer-public-key"),
			Endpoint:   "198.51.100.10:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	inspection := client.WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		PeerCount: 1,
		Peers: []client.WireGuardPeerInspection{{
			PublicKey:           testWireGuardPublicKey("peer-public-key"),
			LatestHandshakeUnix: 123,
		}},
		Routes: []client.WireGuardRouteInspection{{
			Target:        "100.64.0.3",
			Interface:     "wg0",
			UsesInterface: true,
		}},
	}
	attachLivePathStatus(payload, networkMap, inspection, map[string]client.DirectRTTProbe{
		"100.64.0.3": {Target: "100.64.0.3", RTTMS: 1.25},
	}, client.RelayDialResult{}, errors.New("relay probing was not run by status"))

	if payload["path_count"] != 1 {
		t.Fatalf("path_count = %#v", payload["path_count"])
	}
	counts, ok := payload["selected_path_counts"].(map[string]int)
	if !ok || counts["direct"] != 1 {
		t.Fatalf("selected path counts = %#v", payload["selected_path_counts"])
	}
	paths, ok := payload["paths"].([]client.PeerPathStatus)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v", payload["paths"])
	}
	if paths[0].SelectedPath != "direct" || paths[0].Direct.RTTMS != 1.25 {
		t.Fatalf("live path status = %#v", paths[0])
	}
}

func TestAttachLivePathStatusCanSelectRelay(t *testing.T) {
	payload := map[string]any{}
	relay := relayauth.Endpoint{ID: "relay-1", Addr: "127.0.0.1:8443", Protocol: relayauth.EndpointProtocolTLS}
	networkMap := clientapi.RegisterNodeResponse{
		Relays: []relayauth.Endpoint{relay},
		Peers: []clientapi.Peer{{
			ID:         "peer-1",
			Hostname:   "peer-a",
			PublicKey:  testWireGuardPublicKey("peer-public-key"),
			Endpoint:   "198.51.100.10:51820",
			AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	inspection := client.WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		PeerCount: 0,
	}
	attachLivePathStatus(payload, networkMap, inspection, nil, client.RelayDialResult{Selected: &relay}, nil)

	counts, ok := payload["selected_path_counts"].(map[string]int)
	if !ok || counts["relay"] != 1 {
		t.Fatalf("selected path counts = %#v", payload["selected_path_counts"])
	}
	paths, ok := payload["paths"].([]client.PeerPathStatus)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v", payload["paths"])
	}
	if paths[0].SelectedPath != "relay" || paths[0].Relay.Endpoint != relay.Addr || paths[0].Relay.RelayID != relay.ID {
		t.Fatalf("relay path status = %#v", paths[0])
	}
}

func TestSubnetRouteDiagnosticsIdentifyReturnPathIssue(t *testing.T) {
	networkMap := clientapi.RegisterNodeResponse{
		Peers: []clientapi.Peer{{
			ID:         "router-1",
			Hostname:   "router-a",
			PublicKey:  testWireGuardPublicKey("router-public-key"),
			AllowedIPs: []string{"100.64.0.2/32", "10.92.0.0/24"},
		}},
	}
	inspection := client.WireGuardInspection{
		OK:        true,
		Interface: "wg0",
		PeerCount: 1,
		Peers: []client.WireGuardPeerInspection{{
			PublicKey:           testWireGuardPublicKey("router-public-key"),
			LatestHandshakeUnix: 123,
		}},
		Routes: []client.WireGuardRouteInspection{
			{Target: "100.64.0.2", Interface: "wg0", UsesInterface: true},
			{Target: "10.92.0.10", Interface: "wg0", UsesInterface: true},
		},
	}

	diagnostics := subnetRouteDiagnostics(
		networkMap,
		inspection,
		[]string{"10.92.0.10"},
		true,
		map[string]client.DirectRTTProbe{
			"100.64.0.2": {Target: "100.64.0.2", RTTMS: 1.5},
		},
		map[string]client.DirectRTTProbe{
			"10.92.0.10": {Target: "10.92.0.10", Error: "100% packet loss"},
		},
	)

	if len(diagnostics) != 1 {
		t.Fatalf("subnet diagnostics = %#v", diagnostics)
	}
	diag := diagnostics[0]
	if diag.Status != "return_path_issue" ||
		diag.Target != "10.92.0.10" ||
		diag.PeerID != "router-1" ||
		diag.MatchedPrefix != "10.92.0.0/24" ||
		!diag.RouteUsesInterface ||
		!diag.PeerReachable ||
		diag.TargetReachable ||
		!strings.Contains(diag.Reason, "return route") ||
		!strings.Contains(diag.Reason, "SNAT") {
		t.Fatalf("subnet route diagnostic = %#v", diag)
	}
}

func TestAppendUniqueStringsTrimsAndDeduplicates(t *testing.T) {
	got := appendUniqueStrings([]string{" 100.64.0.3 ", ""}, []string{"100.64.0.3", "100.64.0.4"})
	want := []string{"100.64.0.3", "100.64.0.4"}
	if len(got) != len(want) {
		t.Fatalf("appendUniqueStrings = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("appendUniqueStrings = %#v, want %#v", got, want)
		}
	}
}

func testEnrollmentServer(t *testing.T, mapKey ed25519.PrivateKey, expectedToken string, revision uint64, mutate ...func(*clientapi.RegisterNodeResponse)) (*httptest.Server, func() (clientapi.RegisterNodeRequest, int)) {
	t.Helper()
	keyMap := testNetworkMapWithRevision(t, mapKey, "net-1", "node-1", revision)
	mapSigningPublicKey := testMapSigningPublicKey(t, keyMap.MapSignature)
	var mu sync.Mutex
	var gotReq clientapi.RegisterNodeRequest
	registerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			w.WriteHeader(http.StatusOK)
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, mapSigningPublicKey))
		case "/nodes/register":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req clientapi.RegisterNodeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.JoinToken != expectedToken {
				http.Error(w, "unexpected join token", http.StatusBadRequest)
				return
			}
			response := clientapi.RegisterNodeResponse{
				Network:             clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", Revision: revision},
				SchemaVersion:       clientapi.SchemaVersion,
				IdempotencyID:       req.IdempotencyID,
				RegistrationBinding: clientapi.RegistrationIdentityProofBinding(req),
				Node: clientapi.Node{
					ID:                "node-1",
					NetworkID:         "net-1",
					Hostname:          req.Hostname,
					IdentityPublicKey: req.IdentityPublicKey,
					PublicKey:         req.PublicKey,
					DeviceFingerprint: req.DeviceFingerprint,
					AssignedIP:        "100.64.0.2",
				},
			}
			credential, err := clientapi.SignNodeCredential(mapKey, response.Network.ID, response.Node.ID, []string{"node:register", "node:map", "node:endpoint", "node:delete"}, time.Now().UTC().Add(time.Hour))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response.NodeCredential = credential
			for _, apply := range mutate {
				apply(&response)
			}
			signature, err := clientapi.SignNetworkMap(mapKey, response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response.MapSignature = signature
			mu.Lock()
			gotReq = req
			registerCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	snapshot := func() (clientapi.RegisterNodeRequest, int) {
		mu.Lock()
		defer mu.Unlock()
		return gotReq, registerCalls
	}
	return server, snapshot
}

func testPendingEnrollmentServer(t *testing.T, mapKey ed25519.PrivateKey, expectedToken string, mutate ...func(*clientapi.RegisterNodeResponse)) (*httptest.Server, func() (clientapi.RegisterNodeRequest, int)) {
	t.Helper()
	keyMap := testNetworkMapWithRevision(t, mapKey, "net-pending", "node-pending", 1)
	mapSigningPublicKey := testMapSigningPublicKey(t, keyMap.MapSignature)
	var mu sync.Mutex
	var gotReq clientapi.RegisterNodeRequest
	registerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/readyz":
			_, _ = w.Write([]byte("ok"))
		case "/server-key":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testServerKeyResponse(t, mapSigningPublicKey))
		case "/nodes/register":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req clientapi.RegisterNodeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if req.JoinToken != expectedToken {
				http.Error(w, "unexpected join token", http.StatusBadRequest)
				return
			}
			mu.Lock()
			gotReq = req
			registerCalls++
			mu.Unlock()
			response := clientapi.RegisterNodeResponse{
				Network:             clientapi.Network{ID: "net-pending", Name: "default", CIDR: "100.64.0.0/24", Revision: 1},
				SchemaVersion:       clientapi.SchemaVersion,
				IdempotencyID:       req.IdempotencyID,
				RegistrationBinding: clientapi.RegistrationIdentityProofBinding(req),
				Node: clientapi.Node{
					ID:                "node-pending",
					NetworkID:         "net-pending",
					Hostname:          req.Hostname,
					IdentityPublicKey: req.IdentityPublicKey,
					PublicKey:         req.PublicKey,
					DeviceFingerprint: req.DeviceFingerprint,
					AssignedIP:        "100.64.0.2",
					ApprovalState:     clientapi.NodeApprovalPending,
				},
			}
			credential, err := clientapi.SignNodeCredential(mapKey, response.Network.ID, response.Node.ID, []string{"node:register", "node:map", "node:endpoint", "node:delete"}, time.Now().UTC().Add(time.Hour))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response.NodeCredential = credential
			for _, apply := range mutate {
				apply(&response)
			}
			signature, err := clientapi.SignNetworkMap(mapKey, response)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			response.MapSignature = signature
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	snapshot := func() (clientapi.RegisterNodeRequest, int) {
		mu.Lock()
		defer mu.Unlock()
		return gotReq, registerCalls
	}
	return server, snapshot
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func signedTestNetworkMap(t *testing.T, networkID, nodeID string, revision uint64) clientapi.RegisterNodeResponse {
	t.Helper()
	privateKey := testMapSigningKey(t)
	return testNetworkMapWithRevision(t, privateKey, networkID, nodeID, revision)
}

func testNetworkMapWithRevision(t *testing.T, privateKey ed25519.PrivateKey, networkID, nodeID string, revision uint64) clientapi.RegisterNodeResponse {
	t.Helper()
	response := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{
			ID:       networkID,
			Name:     "default",
			CIDR:     "100.64.0.0/24",
			Revision: revision,
		},
		Node: clientapi.Node{
			ID:         nodeID,
			NetworkID:  networkID,
			Hostname:   "node-a",
			PublicKey:  testWireGuardPublicKey("pub-a"),
			AssignedIP: "100.64.0.2",
		},
	}
	signature, err := clientapi.SignNetworkMap(privateKey, response)
	if err != nil {
		t.Fatal(err)
	}
	response.MapSignature = signature
	return response
}

var (
	testMapSigningPublicKeyRegistry  sync.Map
	testMapSigningPrivateKeyRegistry sync.Map
)

func testMapSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := base64.RawURLEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
	keyID, err := clientapi.SigningKeyID(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	testMapSigningPublicKeyRegistry.Store(keyID, publicKey)
	testMapSigningPrivateKeyRegistry.Store(keyID, privateKey)
	return privateKey
}

func testMapSigningPublicKey(t *testing.T, signature *clientapi.MapSignature) string {
	t.Helper()
	if signature == nil || strings.TrimSpace(signature.KeyID) == "" {
		t.Fatal("map signature key_id is required")
	}
	publicKey, ok := testMapSigningPublicKeyRegistry.Load(signature.KeyID)
	if !ok {
		t.Fatalf("map signing fixture key %q is not registered", signature.KeyID)
	}
	return publicKey.(string)
}

func testSigningTrustBundle(t *testing.T, publicKey string) *clientapi.SigningTrustBundle {
	t.Helper()
	bundle, err := clientapi.NewSigningTrustBundle(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &bundle
}

func testServerKeyResponse(t *testing.T, publicKey string) clientapi.ServerKeyResponse {
	t.Helper()
	return testServerKeyResponseFromBundle(*testSigningTrustBundle(t, publicKey))
}

func setTestMapStreamResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("X-EndlessNet-Map-Protocol", fmt.Sprint(clientapi.MapStreamProtocolVersion))
	w.Header().Set("X-EndlessNet-Map-Capabilities", strings.Join(clientapi.MapStreamSupportedCapabilities(), ","))
}

func testMapStreamSnapshotEvent(t *testing.T, response clientapi.RegisterNodeResponse) clientapi.MapStreamEvent {
	t.Helper()
	snapshot := networkMapSnapshotFromResponse(response)
	snapshot.Revision = clientapi.MapRevision{Network: response.Network.Revision}
	privateKey, ok := testMapSigningPrivateKeyRegistry.Load(response.MapSignature.KeyID)
	if !ok {
		t.Fatalf("map signing fixture key %q is not registered", response.MapSignature.KeyID)
	}
	signature, err := clientapi.SignNetworkMapSnapshot(privateKey.(ed25519.PrivateKey), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return clientapi.MapStreamEvent{
		Type:            "snapshot",
		ProtocolVersion: clientapi.MapStreamProtocolVersion,
		Capabilities:    clientapi.MapStreamSupportedCapabilities(),
		EventID:         "test-snapshot",
		To:              snapshot.Revision,
		Snapshot:        &snapshot,
		ResultSignature: signature,
	}
}

func testMapStreamSnapshotEventWithSignature(response clientapi.RegisterNodeResponse, signature *clientapi.MapSignature) clientapi.MapStreamEvent {
	snapshot := networkMapSnapshotFromResponse(response)
	snapshot.Revision = clientapi.MapRevision{Network: response.Network.Revision}
	return clientapi.MapStreamEvent{
		Type:            "snapshot",
		ProtocolVersion: clientapi.MapStreamProtocolVersion,
		Capabilities:    clientapi.MapStreamSupportedCapabilities(),
		EventID:         "test-snapshot",
		To:              snapshot.Revision,
		Snapshot:        &snapshot,
		ResultSignature: signature,
	}
}

func testServerKeyResponseFromBundle(bundle clientapi.SigningTrustBundle) clientapi.ServerKeyResponse {
	return clientapi.ServerKeyResponse{
		TrustBundle:               bundle,
		NodeCredentialTrustBundle: bundle,
		RelayTrustBundle:          bundle,
	}
}
