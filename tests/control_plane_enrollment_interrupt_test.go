package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-008/HC-010: process interruption while approval is pending must preserve
// the enrollment operation for an explicit later CLI invocation. This tests
// process termination on every OS, not interactive signal handling or a
// server-side cancellation/revocation API.
func TestControlPlaneBrowserEnrollmentInterrupted(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	if _, _, err := s.AddNetwork("interrupted-enrollment", "100.96.0.0/24"); err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	args := []string{"up", "--server", s.URL(), "--network", "interrupted-enrollment", "--hostname", "interrupted-node", "--config", n.Config, "--map-signing-trust-file", n.TrustFile, "--approval-timeout", "10s"}
	processContext, stop := context.WithCancel(t.Context())
	exited := make(chan struct{})
	var processErr error
	go func() {
		defer close(exited)
		_, processErr = n.RunContext(processContext, args...)
	}()
	t.Cleanup(func() { stop(); <-exited })
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	created, err := s.Await(ctx, func(e testcontrol.Event) bool { return e.Kind == "enrollment" })
	if err != nil {
		t.Fatal("CLI did not create enrollment before interruption")
	}
	if err := testclient.Await(ctx, func() bool {
		polls := 0
		for _, event := range s.Events() {
			if event.Kind == "request" && event.Path == "GET /nodes/enrollment-requests/"+created.Path {
				polls++
			}
		}
		return polls >= 2
	}); err != nil {
		t.Fatal("CLI did not poll the pending enrollment")
	}
	select {
	case <-exited:
		t.Fatal("CLI exited before the test interrupted it")
	default:
	}
	stop()
	<-exited
	if processErr == nil {
		t.Fatal("interrupted pending CLI reported success")
	}
	if err := s.DecideEnrollment(created.Path, true); err != nil {
		t.Fatal(err)
	}
	for _, event := range s.Events() {
		if event.Kind == "registered" || event.Kind == "registration-refreshed" || (event.Kind == "request" && event.Path == "POST /nodes/enrollment-requests/"+created.Path+"/complete") {
			t.Fatal("terminated pending CLI completed enrollment")
		}
	}
	output, err := n.Run(args...)
	if err != nil || !strings.Contains(string(output), "enrolled node") {
		t.Fatal("explicit CLI restart did not complete approved enrollment (output withheld)")
	}
	n.Start()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NodeCredentialPresent && v.CachedMapValid
	})
	createdCount, registeredCount := 0, 0
	for _, event := range s.Events() {
		switch event.Kind {
		case "enrollment":
			createdCount++
			if event.Path != created.Path {
				t.Fatal("resumed CLI created a different enrollment operation")
			}
		case "registered":
			registeredCount++
			if event.NodeID != status.NodeID {
				t.Fatal("agent identity differs from resumed enrollment")
			}
		}
	}
	if createdCount != 1 || registeredCount != 1 {
		t.Fatal("interrupted enrollment did not resume exactly once")
	}
}
