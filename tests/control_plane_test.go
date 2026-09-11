package tests

import (
	"context"
	"encoding/json"
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

// IT-20 / HC-014, HC-030, HC-063: public errors, not status codes or
// diagnostic words, decide whether the real agent forgets enrollment.
func TestControlPlaneRecoveryErrorMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     api.ErrorCode
		terminal bool
	}{
		{"unknown", api.ErrorCodeNodeCredentialUnknown, true},
		{"revoked", api.ErrorCodeNodeCredentialRevoked, true},
		{"expired", api.ErrorCodeNodeCredentialExpired, true},
		{"invalid", api.ErrorCodeNodeCredentialInvalid, false},
		{"binding", api.ErrorCodeNodeIdentityBindingMismatch, false},
		{"session", api.ErrorCodeAuthenticationRequired, false},
		{"policy", api.ErrorCodeAuthorizationDenied, false},
		{"temporary", api.ErrorCodeTemporarilyUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, n, id := controlScenario(t)
			n.Stop()
			body, err := json.Marshal(api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: tc.code, DiagnosticMessage: "node_credential_unknown revoked expired", RequestID: "test-request"})
			if err != nil {
				t.Fatal(err)
			}
			status, ok := tc.code.HTTPStatus()
			if !ok {
				t.Fatal("missing error status")
			}
			path := "/maps/" + id + "/stream"
			if err := s.SetResponseFault("GET", path, status, "application/json", string(body)); err != nil {
				t.Fatal(err)
			}
			n.Start()
			if tc.terminal {
				n.AwaitStatus(func(v ipc.StatusResponse) bool {
					return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && !v.NodeCredentialPresent && !v.CachedMapPresent
				})
			} else {
				v := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.State == ipc.StateDegraded })
				if v.NodeID != id || !v.NodeCredentialPresent || !v.CachedMapValid {
					t.Fatal("nonterminal code cleared enrollment")
				}
				s.ClearResponseFault("GET", path)
				update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "after-error" })
				n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid && v.MapRevision > 1 })
			}
		})
	}
}

func TestControlPlaneMalformedErrorsPreserveEnrollment(t *testing.T) {
	unknown, err := json.Marshal(api.PublicError{SchemaVersion: api.SchemaVersion, ErrorCode: "future_terminal_code", DiagnosticMessage: "node_credential_revoked", RequestID: "test-request"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name              string
		status            int
		contentType, body string
	}{
		{"plain-401", 401, "text/plain", "node_credential_unknown revoked expired"},
		{"plain-403", 403, "text/plain", "forbidden node_credential_revoked"},
		{"unknown-code", 401, "application/json", string(unknown)},
		{"generic-json", 401, "application/json", `{"error":"node_credential_expired"}`},
		{"truncated-json", 401, "application/json", `{"error_code":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, n, id := controlScenario(t)
			n.Stop()
			path := "/maps/" + id + "/stream"
			if err := s.SetResponseFault("GET", path, tc.status, tc.contentType, tc.body); err != nil {
				t.Fatal(err)
			}
			n.Start()
			v := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.State == ipc.StateDegraded })
			if v.NodeID != id || !v.NodeCredentialPresent || !v.CachedMapValid {
				t.Fatal("malformed error cleared enrollment")
			}
			s.ClearResponseFault("GET", path)
			update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "after-malformed" })
			n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid && v.MapRevision > 1 })
		})
	}
}

func requireControlScenario(t *testing.T) {
	t.Helper()
	if testing.Short() || os.Getenv("ENDLESSNET_CONTROL_TEST") != "1" {
		t.Skip("requires isolated control-plane CI")
	}
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("requires a disposable Linux GitHub-hosted runner")
	}
}

func controlScenario(t *testing.T) (*testcontrol.Server, *testclient.Node, string) {
	t.Helper()
	requireControlScenario(t)
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

// IT-05 / HC-010: retry a committed registration after its response was lost.
// A second CLI process must recover the operation without exposing its store.
// IT-04/IT-10: even a trusted signature cannot authorize a response for a
// different request or a credential for a different node/network.
func TestControlPlaneRejectsRegistrationResponseMismatch(t *testing.T) {
	for _, fault := range []string{"operation", "binding", "fingerprint", "credential-node", "credential-network", "map-signature"} {
		t.Run(fault, func(t *testing.T) {
			requireControlScenario(t)
			s := testcontrol.New(t)
			network, token, err := s.AddNetwork("response-binding", "100.92.0.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			if err := s.FaultNextRegistrationResponse(fault); err != nil {
				t.Fatal(err)
			}
			args := []string{"up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "bound-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off"}
			if _, err := n.Run(args...); err == nil {
				t.Fatal("invalid registration response accepted")
			}
			n.Start()
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && !v.NodeCredentialPresent && !v.CachedMapPresent
			})
			n.Stop()
			n.MustRun(args...)
			n.Start()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
			var first testcontrol.Event
			attempts, registrations, injected := 0, 0, 0
			for _, event := range s.Events() {
				switch event.Kind {
				case "registration-request":
					if attempts == 0 {
						first = event
					} else if event.OperationID != first.OperationID || event.RequestHash != first.RequestHash {
						t.Fatal("rejected response changed retry identity")
					}
					attempts++
				case "registered":
					registrations++
					if event.NodeID != status.NodeID {
						t.Fatal("recovery selected a different node")
					}
				case "registration-response-faulted":
					injected++
				}
			}
			if attempts != 2 || registrations != 1 || injected != 1 {
				t.Fatal("mismatch recovery did not reuse the single committed operation")
			}
		})
	}
}

func TestControlPlaneRegistrationResponseLoss(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("retry", "100.91.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	s.DropNextRegistrationResponse()
	args := []string{"up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "retry-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off"}
	// The client may retry within the first command. If it exits, the second
	// command is the process-restart variant of the same operation.
	if _, err = n.Run(args...); err != nil {
		// Changed input must fail before another registration is sent. The
		// original operation remains recoverable after this rejected command.
		changed := append([]string(nil), args...)
		for i := range changed {
			if changed[i] == "retry-node" {
				changed[i] = "different-node"
			}
		}
		before := len(s.Events())
		if _, changedErr := n.Run(changed...); changedErr == nil {
			t.Fatal("changed pending registration was accepted")
		}
		for _, event := range s.Events()[before:] {
			if event.Kind == "registration-request" {
				t.Fatal("changed pending request reached registration endpoint")
			}
		}
		if _, err = n.Run(args...); err != nil {
			t.Fatal("registration did not recover after response loss")
		}
	}
	n.Start()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid })
	var first testcontrol.Event
	attempts, registrations := 0, 0
	dropped := ""
	for _, event := range s.Events() {
		switch event.Kind {
		case "registration-request":
			if attempts == 0 {
				first = event
			} else if event.OperationID != first.OperationID || event.RequestHash != first.RequestHash {
				t.Fatal("client changed the operation or signed input on retry")
			}
			attempts++
		case "registered":
			registrations++
		case "registration-response-dropped":
			dropped = event.NodeID
		}
	}
	if attempts < 2 || registrations != 1 || dropped != status.NodeID {
		t.Fatal("retry did not recover the original registered node")
	}
}

// IT-20 / HC-030: a temporary service failure must not destroy enrollment.
func TestControlPlaneTemporaryFailurePreservesEnrollment(t *testing.T) {
	s, n, id := controlScenario(t)
	s.SetUnavailable(true)
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.State == ipc.StateDegraded })
	if status.NodeID != id || !status.NodeCredentialPresent || !status.CachedMapValid {
		t.Fatal("temporary failure destroyed verified enrollment")
	}
	n.Stop()
	n.Start()
	status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.State == ipc.StateDegraded })
	if status.NodeID != id || !status.NodeCredentialPresent {
		t.Fatal("restart during outage discarded enrollment")
	}
	s.SetUnavailable(false)
	update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "recovered" })
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.CachedMapValid && v.MapRevision > status.MapRevision
	})
	for _, event := range s.Events() {
		if event.Kind == "registration-request" && event.OperationID == "" {
			t.Fatal("registration lacked operation identity")
		}
	}
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("outage recovery created another registration")
	}
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
	})
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.PeerCount == 1 && v.MapRevision >= 2 })
	var diagnostics ipc.DiagnosticsResponse
	n.Service("diagnostics", &diagnostics)
	if diagnostics.Diagnostics.RouteSummary == nil || diagnostics.Diagnostics.RouteSummary.PeerCount != 1 {
		t.Fatal("route projection did not reach IPC diagnostics")
	}
	before := len(s.Events())
	s.BreakStreams()
	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer resumeCancel()
	if err := testclient.Await(resumeCtx, func() bool {
		for _, event := range s.Events()[before:] {
			if event.Kind == "stream" && event.Cursor.Revision.Network >= status.MapRevision {
				return true
			}
		}
		return false
	}); err != nil {
		n.Service("diagnostics", &diagnostics)
		t.Fatalf("client did not reconnect with a saved cursor: errors=%v requests=%d", diagnostics.Diagnostics.LastErrors, len(s.Events())-before)
	}
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
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.State == ipc.StateDegraded })
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

// IT-07/IT-10: delta acceptance, rejection and cursor loss through public CLI/IPC.
func TestControlPlanePeerDeltaRecovery(t *testing.T) {
	s, n, id := controlScenario(t)
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.Stop()
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	peer := api.Peer{ID: "delta-peer", Hostname: "peer", PublicKey: pub, AllowedIPs: []string{"100.90.0.20/32"}}
	// Disconnect can publish a node-status revision. Establish a known cached
	// cursor after teardown rather than assuming registration is still revision 1.
	update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "delta-baseline" })
	n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
	base, err := s.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	revision := base.Revision.Network
	for _, peers := range [][]api.Peer{{peer}, nil} {
		if err := s.UpdatePeers(id, peers); err != nil {
			t.Fatal(err)
		}
		n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
		revision++
		n.Start()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.UserDisconnected && v.CachedMapValid && v.MapRevision == revision && v.PeerCount == len(peers)
		})
		n.Stop()
	}
	for _, fault := range []string{"delta-base", "delta-revision"} {
		if err := s.UpdatePeers(id, []api.Peer{peer}); err != nil {
			t.Fatal(err)
		}
		if err := s.FaultNextMap(id, fault); err != nil {
			t.Fatal(err)
		}
		if _, err := n.Run("sync", "--config", n.Config, "--timeout", "1s"); err == nil {
			t.Fatal("invalid delta was accepted")
		}
		n.MustRun("sync", "--config", n.Config, "--offline")
		n.Start()
		n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return v.UserDisconnected && v.CachedMapValid && v.MapRevision == revision && v.NodeID == id
		})
		n.Stop()
		n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
		revision++
	}
	// Skip two revisions: the double retains only the latest delta. The saved
	// cursor must recover through the full, signed resync response.
	if err := s.UpdatePeers(id, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePeers(id, []api.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	beforeResync := len(s.Events())
	n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
	revision += 2
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.UserDisconnected && v.CachedMapValid && v.MapRevision == revision && v.PeerCount == 1 && v.NodeID == id
	})
	deltas := 0
	for _, event := range s.Events() {
		if event.Kind == "map-delta" {
			deltas++
		}
	}
	if deltas < 6 {
		t.Fatal("scenario did not exercise delta transport")
	}
	resynced := false
	for _, event := range s.Events()[beforeResync:] {
		if event.Kind == "map-resync" && event.Cursor.Revision.Network == revision-2 {
			resynced = true
		}
	}
	if !resynced {
		t.Fatal("missing delta history did not exercise resync transport")
	}
}

func TestControlPlaneDNSProjection(t *testing.T) {
	s, n, id := controlScenario(t)
	// Keep system DNS outside this acceptance test. The normal sync command
	// verifies and caches the map; a disconnected agent exposes its projection.
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.Stop()
	update(t, s, id, func(m *api.NetworkMapSnapshot) {
		m.Network.DNS = []string{"1.1.1.1"}
	})
	n.MustRun("sync", "--config", n.Config, "--timeout", "1s")
	n.Start()
	var diagnostics ipc.DiagnosticsResponse
	n.Service("diagnostics", &diagnostics)
	dns := diagnostics.Diagnostics.DNSSummary
	if dns == nil || len(dns.NetworkDNSServers) != 1 || dns.NetworkDNSServers[0] != "1.1.1.1" {
		t.Fatal("DNS projection did not reach IPC diagnostics")
	}
}
func TestControlPlaneRejectsInvalidMaps(t *testing.T) {
	s, n, id := controlScenario(t)
	n.Stop()
	for _, fault := range []string{"signature", "unknown-key", "expired"} {
		t.Run(fault, func(t *testing.T) {
			// A duplicate of the already verified map can be ignored by hash.
			// Exercise rejection of a genuinely new projection instead.
			update(t, s, id, func(m *api.NetworkMapSnapshot) { m.Network.Name = "fault-" + fault })
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
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateNeedsEnrollment && !v.NodeCredentialPresent && v.NodeID == ""
	})
}
func TestControlPlaneBrowserEnrollmentExpiryRecovery(t *testing.T) {
	s, n, _ := controlScenario(t)
	n.Stop()
	other := testclient.New(t, s)
	up := func(timeout string) ([]byte, error) {
		return other.Run("up", "--server", s.URL(), "--network", "scenario", "--hostname", "expiry-node", "--config", other.Config, "--map-signing-trust-file", other.TrustFile, "--approval-timeout", timeout)
	}
	requests := func() []string {
		var ids []string
		for _, e := range s.Events() {
			if e.Kind == "enrollment" {
				ids = append(ids, e.Path)
			}
		}
		return ids
	}
	if _, err := up("0s"); err == nil {
		t.Fatal("pending enrollment succeeded")
	}
	first := requests()
	if len(first) != 1 {
		t.Fatal("initial browser request was not created once")
	}
	if err := s.ExpireEnrollment(first[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideEnrollment(first[0], true); err == nil {
		t.Fatal("expired testserver request could still be approved")
	}
	if _, err := up("0s"); err == nil {
		t.Fatal("expired enrollment granted access")
	}
	second := requests()
	if len(second) != 2 || second[0] == second[1] {
		t.Fatal("client did not replace the expired request with a new enrollment")
	}
	// A separate CLI process must resume the replacement instead of creating
	// another request or silently enrolling before explicit approval.
	if _, err := up("0s"); err == nil {
		t.Fatal("replacement enrolled without approval")
	}
	if len(requests()) != 2 {
		t.Fatal("pending replacement was not reused")
	}
	if err := s.DecideEnrollment(second[1], true); err != nil {
		t.Fatal(err)
	}
	out, err := up("1s")
	if err != nil || !strings.Contains(string(out), "enrolled node") {
		t.Fatal("approved replacement did not enroll")
	}
	other.Start()
	other.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NodeCredentialPresent && v.CachedMapPresent
	})
	if len(requests()) != 2 {
		t.Fatal("agent created another browser enrollment")
	}
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
