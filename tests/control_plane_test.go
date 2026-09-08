package tests

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

func controlScenario(t *testing.T) (*testcontrol.Server, *testclient.Node, string) {
	t.Helper()
	if testing.Short() || os.Getenv("ENDLESSNET_CONTROL_TEST") != "1" {
		t.Skip("requires isolated control-plane CI")
	}
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("requires a disposable Linux GitHub-hosted runner")
	}
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("scenario", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, token)
	n.Start()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
	return s, n, status.NodeID
}
func update(t *testing.T, s *testcontrol.Server, id string, edit func(*api.NetworkMapSnapshot)) {
	t.Helper()
	if err := s.UpdateMap(id, edit); err != nil {
		t.Fatal(err)
	}
}
func TestControlPlaneLifecycle(t *testing.T) {
	s, n, id := controlScenario(t)
	private, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	update(t, s, id, func(m *api.NetworkMapSnapshot) {
		m.Peers = []api.Peer{{ID: "test-peer", Hostname: "peer", PublicKey: public, AllowedIPs: []string{"100.90.0.20/32"}, ACLRestricted: true, ACLGrants: []api.ACLGrant{{DestinationCIDRs: []string{"100.90.0.20/32"}, AllowedPorts: []api.ACLPort{{Protocol: "tcp", Port: 443}}}}}}
		m.Network.DNS = []string{"1.1.1.1"}
	})
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.PeerCount == 1 && v.MapRevision >= 2 })
	var diagnostics ipc.DiagnosticsResponse
	n.Service("diagnostics", &diagnostics)
	if diagnostics.Diagnostics.DNSSummary == nil || len(diagnostics.Diagnostics.DNSSummary.NetworkDNSServers) != 1 || diagnostics.Diagnostics.RouteSummary == nil || diagnostics.Diagnostics.RouteSummary.PeerCount != 1 {
		t.Fatal("DNS/route projection did not reach IPC diagnostics")
	}
	before := len(s.Events())
	s.BreakStreams()
	update(t, s, id, func(m *api.NetworkMapSnapshot) {
		m.Peers[0].Hostname = "renamed"
		m.Peers[0].ACLGrants[0].AllowedPorts[0].Port = 8443
	})
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.MapRevision > status.MapRevision })
	found := false
	for _, e := range s.Events()[before:] {
		if e.Kind == "stream" && e.Cursor.Revision.Network >= status.MapRevision {
			found = true
		}
	}
	if !found {
		t.Fatal("client did not resume with a saved cursor")
	}
	s.SetUnavailable(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requests := len(s.Events())
	if err := testclient.Await(ctx, func() bool { return len(s.Events()) > requests }); err != nil {
		t.Fatal("client did not retry unavailable control")
	}
	s.SetUnavailable(false)
	update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Peers = nil })
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.PeerCount == 0 && v.MapRevision > status.MapRevision })
	registrations := 0
	for _, e := range s.Events() {
		if e.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("transport recovery repeated enrollment")
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.Stop()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	var logout ipc.LogoutResponse
	n.Service("logout", &logout)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return !v.NodeCredentialPresent && !v.CachedMapPresent && v.NodeID == ""
	})
}
func TestControlPlaneRejectsInvalidMaps(t *testing.T) {
	s, n, id := controlScenario(t)
	n.Stop()
	for _, fault := range []string{"signature", "unknown-key", "expired"} {
		t.Run(fault, func(t *testing.T) {
			if err := s.FaultNextMap(id, fault); err != nil {
				t.Fatal(err)
			}
			if _, err := n.Run("sync", "--config", n.Config, "--timeout", "1s"); err == nil {
				t.Fatal("client accepted invalid signed map")
			}
			if _, err := n.Run("sync", "--config", n.Config, "--offline"); err != nil {
				t.Fatal("invalid map replaced the verified cache")
			}
		})
	}
	n.Start()
	if err := s.Revoke(id); err != nil {
		t.Fatal(err)
	}
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return !v.NodeCredentialPresent && v.NodeID == "" })
}
func TestControlPlaneBrowserEnrollment(t *testing.T) {
	// Use the same CI opt-in and isolation requirements as agent scenarios.
	s, n, _ := controlScenario(t)
	n.Stop()
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approve", false: "reject"}[approve], func(t *testing.T) {
			other := testclient.New(t, s)
			_, err := other.Run("up", "--server", s.URL(), "--network", "scenario", "--hostname", "browser-node", "--config", other.Config, "--map-signing-trust-file", other.TrustFile, "--approval-timeout", "0s")
			if err == nil {
				t.Fatal("pending browser enrollment completed")
			}
			events := s.Events()
			var requestID string
			for i := len(events) - 1; i >= 0; i-- {
				if events[i].Kind == "enrollment" {
					requestID = events[i].Path
					break
				}
			}
			if requestID == "" {
				t.Fatal("browser enrollment was not created")
			}
			if err = s.DecideEnrollment(requestID, approve); err != nil {
				t.Fatal(err)
			}
			out, err := other.Run("up", "--server", s.URL(), "--network", "scenario", "--hostname", "browser-node", "--config", other.Config, "--approval-timeout", "1s")
			if approve {
				if err != nil {
					t.Fatal("approved browser enrollment failed")
				}
				if !strings.Contains(string(out), "enrolled node") {
					t.Fatal("client did not report enrollment")
				}
			} else if err == nil {
				t.Fatal("rejected browser enrollment succeeded")
			}
		})
	}
}
