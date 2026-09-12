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

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testwireguard"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-003: reinstall the same artifact while enrolled. Version upgrades and
// complete state removal require their own scenarios. Only the real CLI writes
// enrollment state; assertions never inspect its private files or keys.
func exerciseInstalledReinstall(t *testing.T, s *testcontrol.Server, binary, configPath string, start, stop, reinstall, repair func(*testing.T)) {
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
	initial := waitInstalledCondition(t, binary, "bootstrap enrollment", func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NetworkID == network.ID && v.NodeCredentialPresent && v.CachedMapValid
	})
	snapshot, err := s.Snapshot(initial.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	peerIP := netip.MustParseAddr("198.18.95.20")
	clientIP := netip.MustParseAddr(initial.OverlayIP)
	underlay := nativePeerUnderlay(t, clientIP, peerIP)
	reference := testwireguard.NewTCP(t, snapshot.Node.PublicKey, clientIP, peerIP, underlay)
	peer := api.Peer{ID: "installed-reference", Hostname: "installed-reference", PublicKey: reference.PublicKey, Endpoint: reference.Endpoint, EndpointCandidates: []string{reference.Endpoint}, AllowedIPs: []string{netip.PrefixFrom(peerIP, 32).String()}}
	if err := s.UpdatePeers(initial.NodeID, []api.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort(peerIP.String(), "24001")
	fresh := func() bool { return applicationProbe(t, probe, "", "tcp", address) }
	sameIdentity := func(v ipc.StatusResponse) bool {
		return v.NodeID == initial.NodeID && v.NetworkID == initial.NetworkID && v.Hostname == initial.Hostname && v.OverlayIP == initial.OverlayIP && v.MapSigningTrustPresent && v.NodeCredentialPresent && v.CachedMapValid
	}
	connected := func(phase string) {
		t.Helper()
		defer func() {
			if !t.Failed() {
				return
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, binary, "service", "status", "--timeout", "2s").Output()
			var current ipc.StatusResponse
			if err != nil || json.Unmarshal(output, &current) != nil {
				t.Log("installed connection diagnostic: public_status_available=false")
				return
			}
			wgOK, validPort, endpointMatches := false, false, false
			peers := 0
			if current.WireGuard != nil {
				wgOK = current.WireGuard.OK
				validPort = current.WireGuard.ListenPort > 0 && current.WireGuard.ListenPort <= 65535
				peers = len(current.WireGuard.Peers)
				endpointMatches = peers == 1 && current.WireGuard.Peers[0].Endpoint == reference.Endpoint
			}
			t.Logf("installed connection diagnostic: public_status_available=true same_identity=%t wireguard_ok=%t valid_listen_port=%t wireguard_peers=%d endpoint_matches=%t", sameIdentity(current), wgOK, validPort, peers, endpointMatches)
		}()
		v := waitInstalledCondition(t, binary, phase, func(v ipc.StatusResponse) bool {
			return sameIdentity(v) && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected && v.PeerCount == 1 && v.WireGuard != nil && v.WireGuard.OK && v.WireGuard.ListenPort > 0 && v.WireGuard.ListenPort <= 65535 && len(v.WireGuard.Peers) == 1 && v.WireGuard.Peers[0].Endpoint == reference.Endpoint
		})
		reference.SetClientEndpoint(t, netip.AddrPortFrom(underlay, uint16(v.WireGuard.ListenPort)))
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		err := testclient.Await(ctx, fresh)
		cancel()
		if err != nil {
			t.Fatal("installed service did not restore real TCP traffic")
		}
	}
	var response ipc.ConnectResponse
	request(t, binary, "connect", &response)
	connected("initial connect")
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
	offline := waitInstalledCondition(t, binary, "service startup without control", func(v ipc.StatusResponse) bool {
		return sameIdentity(v) && v.State == ipc.StateDegraded && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected
	})
	connected("cached traffic after service startup without control")
	s.SetUnavailable(false)
	if err := s.UpdatePeers(initial.NodeID, []api.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	waitInstalledCondition(t, binary, "control recovery after service startup", func(v ipc.StatusResponse) bool {
		return sameIdentity(v) && v.MapRevision > offline.MapRevision && v.State != ipc.StateDegraded
	})
	connected("traffic after late control recovery")

	var disconnected ipc.DisconnectResponse
	request(t, binary, "disconnect", &disconnected)
	assertDisconnected := func(phase string) {
		t.Helper()
		waitInstalledCondition(t, binary, phase, func(v ipc.StatusResponse) bool {
			return sameIdentity(v) && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
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
	request(t, binary, "connect", &response)
	connected("reconnect after reinstall")
	created := 0
	for _, e := range s.Events() {
		if e.Kind == "registered" {
			created++
		}
		if (e.Kind == "registered" || e.Kind == "registration-refreshed") && e.NodeID != initial.NodeID {
			t.Fatal("reinstall changed the enrollment identity")
		}
	}
	if created != 1 {
		t.Fatal("reinstall created another enrollment")
	}
}

func waitInstalledCondition(t *testing.T, binary, phase string, predicate func(ipc.StatusResponse) bool) ipc.StatusResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	var status ipc.StatusResponse
	responses := 0
	err := testclient.Await(ctx, func() bool {
		output, err := exec.CommandContext(ctx, binary, "service", "status", "--timeout", "2s").Output()
		if err != nil {
			return false
		}
		if err := json.Unmarshal(output, &status); err != nil {
			t.Fatal("installed service returned invalid public status")
		}
		responses++
		return predicate(status)
	})
	if err != nil {
		t.Fatalf("installed service phase=%q timed out: responses=%d state=%s control=%s desired=%s disconnected=%t node=%t network=%t credential=%t trust=%t cache=%t cache_valid=%t peers=%d wireguard=%t local_error=%t cache_error=%t intent_error=%t", phase, responses, status.State, status.ControlState, status.DesiredState, status.UserDisconnected, status.NodeID != "", status.NetworkID != "", status.NodeCredentialPresent, status.MapSigningTrustPresent, status.CachedMapPresent, status.CachedMapValid, status.PeerCount, status.WireGuard != nil, status.LocalStateError != "", status.CachedMapError != "", status.ConnectionIntentError != "")
	}
	return status
}
