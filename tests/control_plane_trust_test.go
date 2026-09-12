package tests

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-021/HC-059: rejected operator confirmations must not mutate pinned trust
// or enrollment. This does not qualify successful signing-key rotation.
func TestControlPlaneTrustConfirmation(t *testing.T) {
	s, n, id := controlScenario(t)
	identity := func(t *testing.T) ipc.ServerIdentityResponse {
		t.Helper()
		output, err := n.ServiceCommand("server-identity")
		if err != nil {
			t.Fatal("server identity inspection failed (output withheld)")
		}
		var v ipc.ServerIdentityResponse
		if json.Unmarshal(output, &v) != nil || v.ControlOrigin != s.URL() || v.TrustedKeyID == "" || v.TrustedKeyID != v.AnnouncedKeyID || v.Changed {
			t.Fatal("public identity does not match the enrolled test server")
		}
		return v
	}
	initial := identity(t)
	for _, tc := range []struct {
		name, message string
		args          []string
	}{
		{"missing-consent", "trust-server requires --yes", []string{"--confirmed-control-origin", initial.ControlOrigin, "--confirmed-key-id", initial.AnnouncedKeyID}},
		{"wrong-origin", "confirmed server signing key ID does not match the currently announced key", []string{"--yes", "--confirmed-control-origin", "https://wrong-origin.invalid", "--confirmed-key-id", initial.AnnouncedKeyID}},
		{"wrong-key", "confirmed server signing key ID does not match the currently announced key", []string{"--yes", "--confirmed-control-origin", initial.ControlOrigin, "--confirmed-key-id", "wrong-key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := n.ServiceCommand("trust-server", tc.args...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), tc.message) {
				t.Fatal("CLI did not reject the invalid confirmation (output withheld)")
			}
			current := identity(t)
			if current.TrustedKeyID != initial.TrustedKeyID || current.AnnouncedKeyID != initial.AnnouncedKeyID {
				t.Fatal("rejected confirmation changed server trust")
			}
			if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) { m.Network.Name = tc.name }); err != nil {
				t.Fatal(err)
			}
			m, err := s.Snapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && v.MapRevision >= m.Revision.Network
			})
		})
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	for range 2 {
		output, err := n.ServiceCommand("trust-server", "--yes", "--confirmed-control-origin", initial.ControlOrigin, "--confirmed-key-id", initial.AnnouncedKeyID)
		var response ipc.TrustServerResponse
		if err != nil || json.Unmarshal(output, &response) != nil || response.Outcome != ipc.RecoveryOutcomeAlreadyApplied || response.TrustedKeyID != initial.TrustedKeyID || response.State != ipc.StateDisconnected {
			t.Fatal("repeated unchanged trust confirmation did not preserve disconnected state")
		}
		status, err := n.Status()
		if err != nil || status.NodeID != id || !status.CachedMapValid || !status.UserDisconnected || status.DesiredState != ipc.DesiredDisconnected {
			t.Fatal("unchanged trust confirmation altered identity or disconnected intent")
		}
	}
	n.Stop()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapValid && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	if identity(t).TrustedKeyID != initial.TrustedKeyID {
		t.Fatal("rejected confirmation changed durable server trust")
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID == id && v.CachedMapValid && !v.UserDisconnected })
	registrations := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatal("rejected confirmation changed registration identity")
	}
}
