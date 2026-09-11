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

// HC-008: expire a request after the running CLI has started repeated polling,
// then recover through a separately approved replacement request.
func TestControlPlaneBrowserEnrollmentExpiresDuringPolling(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	if _, _, err := s.AddNetwork("poll-expiry", "100.95.0.0/24"); err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	up := func(timeout string) ([]byte, error) {
		return n.Run("up", "--server", s.URL(), "--network", "poll-expiry", "--hostname", "poll-expiry-node", "--config", n.Config, "--map-signing-trust-file", n.TrustFile, "--approval-timeout", timeout)
	}
	type result struct {
		output []byte
		err    error
	}
	completed, exited := make(chan result, 1), make(chan struct{})
	go func() {
		defer close(exited)
		output, err := up("10s")
		completed <- result{output: output, err: err}
	}()
	// Join the bounded CLI invocation before its temporary configuration and
	// server are removed, including when a synchronization assertion fails.
	t.Cleanup(func() { <-exited })
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	created, err := s.Await(ctx, func(e testcontrol.Event) bool { return e.Kind == "enrollment" })
	if err != nil {
		t.Fatal("CLI did not create an enrollment request")
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
		t.Fatal("CLI did not continue polling the pending request")
	}
	select {
	case <-completed:
		t.Fatal("CLI stopped before request expiry was injected")
	default:
	}
	if err := s.ExpireEnrollment(created.Path); err != nil {
		t.Fatal(err)
	}
	first := <-completed
	if first.err == nil || !strings.Contains(string(first.output), "enrollment request expired") {
		t.Fatal("active CLI did not report request expiry")
	}
	for _, event := range s.Events() {
		if event.Kind == "registered" || event.Kind == "registration-refreshed" {
			t.Fatal("expired pending request produced a node credential")
		}
		if event.Kind == "request" && event.Path == "POST /nodes/enrollment-requests/"+created.Path+"/complete" {
			t.Fatal("CLI attempted to complete expired enrollment")
		}
	}
	if _, err := up("0s"); err == nil {
		t.Fatal("replacement enrolled without approval")
	}
	var requests []string
	for _, event := range s.Events() {
		if event.Kind == "enrollment" {
			requests = append(requests, event.Path)
		}
	}
	if len(requests) != 2 || requests[0] != created.Path || requests[1] == created.Path {
		t.Fatal("CLI did not create exactly one distinct replacement")
	}
	if err := s.DecideEnrollment(created.Path, true); err == nil {
		t.Fatal("expired request was still approvable")
	}
	if err := s.DecideEnrollment(requests[1], true); err != nil {
		t.Fatal(err)
	}
	if output, err := up("1s"); err != nil || !strings.Contains(string(output), "enrolled node") {
		t.Fatal("approved replacement did not recover enrollment")
	}
	n.Start()
	state := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NodeCredentialPresent && v.CachedMapValid
	})
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
			if event.NodeID != state.NodeID {
				t.Fatal("agent identity differs from approved enrollment")
			}
		}
	}
	if registered != 1 {
		t.Fatal("expiry recovery did not create exactly one approved node")
	}
}
