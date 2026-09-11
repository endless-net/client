package tests

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
)

// HC-021: real CLI/agent HTTPS verifies OS trust before enrollment. Map-signing
// trust remains a separate input; it must not implicitly trust the TLS server.
func TestControlPlaneTLSTrustBoundary(t *testing.T) {
	requireControlScenario(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := testcontrol.NewWithListener(t, listener)
	network, join, err := s.AddNetwork("tls-control", "100.97.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	trustedEnvironment := n.Environment
	n.Environment = nil
	_, err = n.Run("up", "--config", n.Config, "--server", s.URL(), "--network", network.Name, "--join-token", join, "--hostname", "tls-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off")
	if err == nil {
		t.Fatal("untrusted HTTPS server enrolled the client")
	}
	for _, event := range s.Events() {
		if event.Kind == "registered" || event.Kind == "registration-refreshed" || event.Kind == "enrollment" {
			t.Fatal("untrusted HTTPS server received enrollment")
		}
	}
	n.Environment = trustedEnvironment
	n.TrustControlTLS(s)
	// The CA is trusted, but its certificate contains only the listener IP.
	// Resolve the alias explicitly so a DNS failure cannot satisfy this case.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	addresses, lookupErr := net.DefaultResolver.LookupIP(ctx, "ip4", "localhost")
	cancel()
	loopbackFound := false
	for _, address := range addresses {
		loopbackFound = loopbackFound || address.Equal(net.ParseIP("127.0.0.1"))
	}
	if lookupErr != nil || !loopbackFound {
		t.Fatal("localhost fixture did not resolve to the TLS listener")
	}
	wrongName := strings.Replace(s.URL(), "127.0.0.1", "localhost", 1)
	beforeMismatch := len(s.Events())
	output, err := n.Run("up", "--config", n.Config, "--server", wrongName, "--network", network.Name, "--join-token", join, "--hostname", "tls-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off")
	if err == nil || !strings.Contains(strings.ToLower(string(output)), "certificate") {
		t.Fatal("trusted certificate with mismatched hostname did not return a certificate error")
	}
	if len(s.Events()) != beforeMismatch {
		t.Fatal("hostname-mismatched TLS connection reached the control HTTP handler")
	}
	n.Enroll(s, network.Name, join)
	n.Start()
	defer n.Stop()
	initial := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NodeCredentialPresent && v.CachedMapValid
	})
	n.Stop()
	n.Start()
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == initial.NodeID && v.NodeCredentialPresent && v.CachedMapValid
	})
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
			if event.NodeID != initial.NodeID {
				t.Fatal("HTTPS enrollment and agent identities differ")
			}
		}
	}
	if registered != 1 {
		t.Fatal("HTTPS restart did not preserve one enrollment")
	}
}
