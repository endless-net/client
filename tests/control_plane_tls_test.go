package tests

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
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
	// The earlier failed enrollment can persist the IP origin. Use an isolated
	// profile and an explicit origin list so failover cannot reach that valid IP.
	wrongNode := testclient.New(t, s)
	beforeMismatch := len(s.Events())
	output, err := wrongNode.Run("up", "--config", wrongNode.Config, "--server", wrongName, "--coordinator", wrongName, "--network", network.Name, "--join-token", join, "--hostname", "tls-node", "--map-signing-trust-file", wrongNode.TrustFile, "--route-table", "off")
	if err == nil || !strings.Contains(strings.ToLower(string(output)), "certificate") {
		t.Fatal("trusted certificate with mismatched hostname did not return a certificate error")
	}
	if len(s.Events()) != beforeMismatch {
		t.Fatal("hostname-mismatched TLS connection reached the control HTTP handler")
	}
	for _, tc := range []struct {
		name        string
		from, until time.Duration
	}{
		{"expired", -2 * time.Hour, -time.Hour},
		{"not-yet-valid", time.Hour, 2 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			if err := s.SetTLSCertificateValidity(now.Add(tc.from), now.Add(tc.until)); err != nil {
				t.Fatal(err)
			}
			before := len(s.Events())
			output, err := n.Run("up", "--config", n.Config, "--server", s.URL(), "--coordinator", s.URL(), "--network", network.Name, "--join-token", join, "--hostname", "tls-node", "--map-signing-trust-file", n.TrustFile, "--route-table", "off")
			if err == nil || !strings.Contains(strings.ToLower(string(output)), "certificate") {
				t.Fatal("invalid TLS lifetime did not produce a certificate error (output withheld)")
			}
			if len(s.Events()) != before {
				t.Fatal("invalid TLS lifetime reached the control HTTP handler")
			}
		})
	}
	if err := s.SetTLSCertificateValidity(time.Now().Add(-time.Minute), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	n.Enroll(s, network.Name, join)
	n.Start()
	defer n.Stop()
	initial := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID != "" && v.NodeCredentialPresent && v.CachedMapValid
	})
	for _, tc := range []struct {
		name        string
		from, until time.Duration
	}{
		{"enrolled-expired", -2 * time.Hour, -time.Hour},
		{"enrolled-not-yet-valid", time.Hour, 2 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Stop closes established TLS connections: this checks a new handshake
			// after restart, not retroactive rejection of an existing session.
			n.Stop()
			now := time.Now()
			if err := s.SetTLSCertificateValidity(now.Add(tc.from), now.Add(tc.until)); err != nil {
				t.Fatal(err)
			}
			before := len(s.Events())
			n.Start()
			degraded := n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == initial.NodeID && v.NetworkID == initial.NetworkID &&
					v.NodeCredentialPresent && v.CachedMapValid && !v.UserDisconnected &&
					v.DesiredState == ipc.DesiredConnected && v.State == ipc.StateDegraded
			})
			if len(s.Events()) != before {
				t.Fatal("enrolled client reached control HTTP through an invalid TLS lifetime")
			}
			if err := s.UpdateMap(initial.NodeID, func(m *api.NetworkMapSnapshot) {}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetTLSCertificateValidity(time.Now().Add(-time.Minute), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			// Recovery must happen in the running agent without a new enrollment
			// or explicit trust override, and consume the newer signed map.
			n.AwaitStatus(func(v ipc.StatusResponse) bool {
				return v.NodeID == initial.NodeID && v.NetworkID == initial.NetworkID &&
					v.NodeCredentialPresent && v.CachedMapValid && !v.UserDisconnected &&
					v.DesiredState == ipc.DesiredConnected && v.State != ipc.StateDegraded &&
					v.MapRevision > degraded.MapRevision
			})
		})
		if t.Failed() {
			t.FailNow()
		}
	}
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
