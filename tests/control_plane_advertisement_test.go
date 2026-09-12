package tests

import (
	"errors"
	"os/exec"
	"slices"
	"testing"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-012/HC-034: advertisement input and signed registration projection.
// Advertising a prefix alone does not prove approval or effective routing.
func TestControlPlaneRouteAdvertisement(t *testing.T) {
	requireControlScenario(t)
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
