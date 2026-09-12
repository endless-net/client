package tests

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-012/HC-034: advertisement input and signed registration projection.
// Advertising a prefix alone does not prove approval or effective routing.
func TestControlPlaneRouteAdvertisement(t *testing.T) {
	requireControlScenario(t)
	for _, tc := range []struct{ flag, invalid, valid string }{
		{"--advertise", "not-a-cidr", "192.0.2.0/24"},
		{"--hostname", "invalid hostname", "route-node"},
		{"--endpoint", "not-an-endpoint", "127.0.0.1:51820"},
	} {
		t.Run("browser-invalid-input-recovery/"+strings.TrimPrefix(tc.flag, "--"), func(t *testing.T) {
			s := testcontrol.New(t)
			network, _, err := s.AddNetwork("browser-advertisement", "100.90.0.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			args := []string{"up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--hostname", "route-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off", "--approval-timeout", "0", tc.flag}
			_, err = n.Run(append(args, tc.invalid)...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatal("invalid browser input was not rejected by the CLI")
			}
			for _, event := range s.Events() {
				if event.Kind == "request" && event.Path == "POST /nodes/enrollment-requests" {
					t.Fatal("invalid browser input reached enrollment")
				}
			}
			// No approval is granted: correction must create exactly one pending
			// request instead of remaining bound to the invalid input.
			output, err := n.Run(append(args, tc.valid)...)
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "browser approval is required") {
				t.Fatal("corrected browser input did not report approval required (output withheld)")
			}
			created := 0
			requestID := ""
			for _, event := range s.Events() {
				if event.Kind == "enrollment" {
					created++
					requestID = event.Path
				}
				if event.Kind == "registered" {
					t.Fatal("browser input enrolled without approval")
				}
			}
			if created != 1 {
				t.Fatal("corrected browser input did not create one approval request")
			}
			if err := s.DecideEnrollment(requestID, true); err != nil {
				t.Fatal(err)
			}
			if _, err := n.Run(append(args, tc.valid)...); err != nil {
				t.Fatal("corrected browser input could not complete approved enrollment")
			}
			n.Start()
			status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID != "" && v.CachedMapValid && v.NodeCredentialPresent
			})
			projection, err := s.Snapshot(status.NodeID)
			if err != nil {
				t.Fatal(err)
			}
			if projection.Node.Hostname != "route-node" {
				t.Fatal("approved browser enrollment lost the corrected hostname")
			}
			if tc.flag == "--advertise" && !slices.Equal(projection.Node.AdvertisedIPs, []string{tc.valid}) {
				t.Fatal("approved browser enrollment lost the corrected prefix")
			}
			created, registered := 0, 0
			for _, event := range s.Events() {
				if event.Kind == "enrollment" {
					created++
				}
				if event.Kind == "registered" {
					registered++
				}
			}
			if created != 1 || registered != 1 {
				t.Fatal("browser correction or completion duplicated enrollment")
			}
		})
	}
	s := testcontrol.New(t)
	network, token, err := s.AddNetwork("advertisement", "100.90.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	_, err = n.Run("up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", token, "--hostname", "route-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off", "--advertise", "not-a-cidr")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("invalid advertised CIDR was not rejected by the CLI")
	}
	for _, event := range s.Events() {
		if event.Kind == "request" && event.Path == "POST /nodes/register" || event.Kind == "registration-request" || event.Kind == "registered" {
			t.Fatal("invalid advertisement reached registration")
		}
	}
	prefixes := []string{"192.0.2.0/24", "2001:db8:94::/64"}
	n.Enroll(s, network.Name, token, "--hostname", "route-node", "--advertise", prefixes[0], "--advertise", prefixes[1])
	n.Start()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return v.NodeID != "" && v.CachedMapValid && v.NodeCredentialPresent })
	projection, err := s.Snapshot(status.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Node.Hostname != "route-node" || !slices.Equal(projection.Node.AdvertisedIPs, prefixes) {
		t.Fatal("signed registration projection lost the requested hostname or prefixes")
	}
	n.Stop()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == status.NodeID && v.CachedMapValid && v.NodeCredentialPresent
	})
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Fatal("advertisement recovery registered another node")
	}
}
