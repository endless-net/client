package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
)

// HC-003/HC-060: upgrade and reinstall the native artifact while enrolled.
// Complete state removal remains a separate scenario. Only the real CLI writes
// enrollment state; assertions never inspect its private files or keys.
func exerciseInstalledReinstall(t *testing.T, s *testcontrol.Server, binary, configPath, initialVersion, upgradeVersion string, start, stop, reinstall, upgrade, repair func(*testing.T)) {
	t.Helper()
	probe := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	network, join, err := s.AddNetwork("installed-client", "198.18.95.0/24")
	if err != nil {
		t.Fatal(err)
	}
	trust, err := json.Marshal(s.Trust())
	if err != nil {
		t.Fatal(err)
	}
	trustFile := filepath.Join(t.TempDir(), "public-trust.json")
	if err := os.WriteFile(trustFile, trust, 0o600); err != nil {
		t.Fatal(err)
	}

	// Stop the service for the public CLI bootstrap, then let the real service
	// manager resume the installed agent. The CLI consumes the token on stdin.
	stop(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	cmd := exec.CommandContext(ctx, binary, "up", "--config", configPath, "--server", s.URL(), "--network", network.Name, "--join-token-file", "-", "--hostname", "installed-node", "--map-signing-trust-file", trustFile, "--route-table", "auto")
	cmd.Stdin = strings.NewReader(join)
	err = cmd.Run()
	cancel()
	if err != nil {
		t.Fatal("installed CLI enrollment failed (output withheld)")
	}
	start(t)
	initial := waitInstalledCondition(t, binary, "bootstrap enrollment", func(v *ipc.Status) bool {
		return v.NodeId != "" && v.ActiveProfileId != "" && nativeOverlayAddress(v, false).IsValid() && v.GetNetwork().GetId() == network.ID && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
	})
	snapshot, err := s.Snapshot(initial.NodeId)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("198.18.95.20")
	clientIP := nativeOverlayAddress(initial, false)
	underlay := nativePeerUnderlay(t, clientIP, peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{ID: "installed-reference", Hostname: "installed-reference", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{netip.PrefixFrom(peerIP, 32).String()}}
	if err := s.UpdatePeers(initial.NodeId, []api.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort(peerIP.String(), "24001")
	fresh := func() bool { return applicationProbe(t, probe, "", "tcp", address) }
	sameIdentity := func(v *ipc.Status) bool {
		return v != nil && v.NodeId == initial.NodeId && v.ActiveProfileId == initial.ActiveProfileId && v.GetNetwork().GetId() == initial.GetNetwork().GetId() && v.Hostname == initial.Hostname && nativeOverlayAddress(v, false) == nativeOverlayAddress(initial, false) && v.GetStoredState().GetMapSigningTrustPresent() && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid()
	}
	connected := func(phase string) *ipc.Status {
		t.Helper()
		defer func() {
			if !t.Failed() {
				return
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, binary, "service", "diagnostics", "--profile-id", initial.ActiveProfileId, "--timeout", "2s").Output()
			response := &ipc.GetDiagnosticsResponse{}
			if err != nil || protojson.Unmarshal(output, response) != nil {
				t.Log("installed connection diagnostic: native_diagnostics_available=false")
				return
			}
			d := response.GetDiagnostics()
			tunnel := d.GetTunnel()
			endpointMatches := len(tunnel.GetPeers()) == 1 && tunnel.Peers[0].GetEndpoint() == reference.Endpoint
			t.Logf("installed connection diagnostic: native_diagnostics_available=true same_identity=%t tunnel_ok=%t valid_listen_port=%t tunnel_peers=%d endpoint_matches=%t",
				sameIdentity(d.GetStatus()), tunnel.GetOk(), tunnel.GetListenPort() > 0 && tunnel.GetListenPort() <= 65535, len(tunnel.GetPeers()), endpointMatches)
		}()
		v := waitInstalledCondition(t, binary, phase, func(v *ipc.Status) bool {
			return sameIdentity(v) && !v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED && v.PeerCount == 1 && v.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED && v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && v.Agent.MapRevision == v.MapRevision
		})
		diagnostics := &ipc.GetDiagnosticsResponse{}
		awaitInstalledNative(t, binary, "diagnostics", diagnostics, func() bool {
			d := diagnostics.GetDiagnostics()
			tunnel := d.GetTunnel()
			return sameIdentity(d.GetStatus()) && d.GetStatus().GetMapRevision() >= v.MapRevision && tunnel.GetOk() && tunnel.GetFailure() == nil &&
				tunnel.GetListenPort() > 0 && tunnel.GetListenPort() <= 65535 && len(tunnel.GetPeers()) == 1 && tunnel.Peers[0].GetPeerId() == peer.ID && tunnel.Peers[0].GetEndpoint() == reference.Endpoint
		}, "--profile-id", v.ActiveProfileId)
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(diagnostics.Diagnostics.Tunnel.ListenPort)))
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		err := testclient.Await(ctx, func() bool {
			beforeReceived, beforeEchoed := reference.PacketCounts()
			if !fresh() {
				return false
			}
			received, echoed := reference.PacketCounts()
			return received > beforeReceived && echoed > beforeEchoed
		})
		cancel()
		if err != nil {
			t.Fatal("installed service did not restore real TCP traffic")
		}
		return v
	}
	runInstalledNativeMutation(t, binary, "connect", "6b110000-0000-4000-8000-000000000001")
	initialConnected := connected("initial connect")
	assertInstalledVersion(t, binary, initialVersion, initialConnected)
	t.Log("upgrade: connected enrolled service")
	upgrade(t)
	upgraded := connected("connected version upgrade")
	assertInstalledVersion(t, binary, upgradeVersion, upgraded)
	t.Log("reinstall: connected enrolled service")
	reinstall(t)
	connected("connected reinstall")
	assertInstalledPeerDenied(t, binary)
	connected("connected intent after restricted local IPC attempts")
	stop(t)
	assertStoppedServiceCommands(t, binary)
	start(t)
	connected("restart after unavailable IPC while connected")
	repairMissingBinary := func() {
		t.Helper()
		stop(t)
		info, err := os.Lstat(binary)
		if !filepath.IsAbs(binary) || err != nil || !info.Mode().IsRegular() {
			t.Fatal("repair fixture requires the known installed executable")
		}
		if err := os.Remove(binary); err != nil {
			t.Fatal("could not remove the stopped service executable")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		err = exec.CommandContext(ctx, binary, "version").Run()
		cancel()
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing installed executable did not produce the expected launch failure")
		}
		repair(t)
	}
	t.Log("repair: missing executable with connected intent")
	repairMissingBinary()
	connected("connected repair after executable loss")
	// HC-006/HC-030: the OS service manager must start an enrolled agent even
	// when control is unavailable. A valid cached map must still carry traffic.
	stop(t)
	s.SetUnavailable(true)
	start(t)
	offline := waitInstalledCondition(t, binary, "service startup without control", func(v *ipc.Status) bool {
		return sameIdentity(v) && nativeCurrentAgentFailure(v) && !v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED
	})
	connected("cached traffic after service startup without control")
	s.SetUnavailable(false)
	if err := s.UpdatePeers(initial.NodeId, []api.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	waitInstalledCondition(t, binary, "control recovery after service startup", func(v *ipc.Status) bool {
		return sameIdentity(v) && v.MapRevision > offline.MapRevision && v.Agent != nil && v.Agent.SnapshotState == ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT && v.Agent.MapRevision == v.MapRevision && v.Agent.LastFailure == nil
	})
	connected("traffic after late control recovery")

	runInstalledNativeMutation(t, binary, "disconnect", "6b110000-0000-4000-8000-000000000002")
	assertDisconnected := func(phase string) {
		t.Helper()
		waitInstalledCondition(t, binary, phase, func(v *ipc.Status) bool {
			return sameIdentity(v) && v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
		})
		if fresh() {
			t.Fatal("disconnected installed service delivered overlay traffic")
		}
	}
	assertDisconnected("initial disconnect")
	registrationRequests := func() int {
		count := 0
		for _, e := range s.Events() {
			// Count at the HTTP boundary, including requests rejected while
			// unavailable before registration validation can record an event.
			if e.Kind == "request" && (e.Path == "POST /nodes/register" || strings.HasPrefix(e.Path, "PATCH /nodes/") && strings.HasSuffix(e.Path, "/endpoint")) {
				count++
			}
		}
		return count
	}
	before := registrationRequests()
	t.Log("reinstall: disconnected enrolled service")
	reinstall(t)
	assertDisconnected("disconnected reinstall")
	assertInstalledPeerDenied(t, binary)
	assertDisconnected("disconnected intent after restricted local IPC attempts")
	if registrationRequests() != before {
		t.Fatal("disconnected reinstall attempted registration or refresh")
	}
	stop(t)
	assertStoppedServiceCommands(t, binary)
	start(t)
	assertDisconnected("restart after unavailable IPC while disconnected")
	t.Log("repair: missing executable with disconnected intent")
	repairMissingBinary()
	assertDisconnected("disconnected repair after executable loss")
	stop(t)
	s.SetUnavailable(true)
	start(t)
	assertDisconnected("disconnected startup without control")
	if registrationRequests() != before {
		t.Fatal("disconnected startup without control attempted registration or refresh")
	}
	s.SetUnavailable(false)
	assertDisconnected("disconnected intent after control becomes available")
	if registrationRequests() != before {
		t.Fatal("failed IPC commands or disconnected restart attempted registration or refresh")
	}
	runInstalledNativeMutation(t, binary, "connect", "6b110000-0000-4000-8000-000000000003")
	connected("reconnect after reinstall")
	created := 0
	for _, e := range s.Events() {
		if e.Kind == "registered" {
			created++
		}
		if (e.Kind == "registered" || e.Kind == "registration-refreshed") && e.NodeID != initial.NodeId {
			t.Fatal("reinstall changed the enrollment identity")
		}
	}
	if created != 1 {
		t.Fatal("reinstall created another enrollment")
	}
}

func assertInstalledVersion(t *testing.T, binary, expected string, status *ipc.Status) {
	t.Helper()
	expected = strings.TrimSpace(expected)
	if expected == "" {
		t.Fatal("installation test expected version is empty")
	}
	output := strings.Split(strings.ReplaceAll(string(command(t, binary, "version")), "\r\n", "\n"), "\n")
	if len(output) == 0 || output[0] != "endlessnet-client "+expected {
		t.Fatal("installed CLI did not report the expected artifact version")
	}
	info := waitInstalledNativeRuntime(t, binary)
	if info.GetBuild().GetVersion() != expected || info.InstanceId != status.GetMetadata().GetInstanceId() {
		t.Fatal("running service did not report the expected upgraded version")
	}
}

func waitInstalledCondition(t *testing.T, binary, phase string, predicate func(*ipc.Status) bool) *ipc.Status {
	t.Helper()
	t.Logf("installed native phase: %s", phase)
	return waitInstalledNativeCondition(t, binary, predicate)
}
