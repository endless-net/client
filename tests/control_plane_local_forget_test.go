package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testclient"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-022: unconfirmed remote revocation is distinct from explicit local cleanup.
// Public CLI/IPC and testserver observations are the only sources of evidence.
func TestControlPlaneLocalForgetAfterUnconfirmedLogout(t *testing.T) {
	s, n, id := controlScenario(t)
	requireExit := func(output []byte, err error, message string) {
		t.Helper()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), message) {
			t.Fatal("CLI did not report the expected cleanup failure (output withheld)")
		}
	}
	output, err := n.ServiceCommand("local-forget")
	requireExit(output, err, "local-forget requires --confirm-local-forget")
	retained := func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.NodeCredentialPresent && v.CachedMapPresent
	}
	n.AwaitStatus(retained)
	s.SetUnavailable(true)
	output, err = n.ServiceCommand("logout")
	requireExit(output, err, "remote cleanup was not confirmed")
	n.AwaitStatus(retained)
	output, err = n.ServiceCommand("local-forget", "--confirm-local-forget")
	if err != nil {
		t.Fatal("explicit local cleanup failed (output withheld)")
	}
	var response ipc.LocalForgetResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal("local cleanup returned invalid public JSON")
	}
	if response.Outcome != ipc.LogoutOutcomeRemoteCleanupUnconfirmed || response.State != ipc.StateNeedsEnrollment {
		t.Fatal("local cleanup incorrectly reported confirmed remote revocation")
	}
	clean := func(v ipc.StatusResponse) bool {
		return v.State == ipc.StateNeedsEnrollment && v.NodeID == "" && v.NetworkID == "" && !v.NodeCredentialPresent && !v.TokenPresent && !v.CachedMapPresent && v.PeerCount == 0 && v.DesiredState == ipc.DesiredDisconnected && v.MapSigningTrustPresent
	}
	n.AwaitStatus(clean)
	s.SetUnavailable(false)
	n.Stop()
	before := len(s.Events())
	n.Start()
	n.AwaitStatus(clean)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = testclient.Await(ctx, func() bool {
		for _, event := range s.Events()[before:] {
			if event.Kind == "registration-request" || event.Kind == "registered" || event.Kind == "registration-refreshed" {
				return true
			}
		}
		return false
	})
	if err == nil {
		t.Fatal("locally forgotten client attempted automatic reenrollment after restart")
	}
	for _, event := range s.Events() {
		if event.Kind == "deleted" || event.Kind == "logout" {
			t.Fatal("testserver unexpectedly confirmed remote cleanup")
		}
	}
}
