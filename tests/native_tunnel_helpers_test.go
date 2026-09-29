package tests

import (
	"context"
	"net/netip"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
)

func nativeOverlayAddress(status *ipc.Status, ipv6 bool) netip.Addr {
	for _, raw := range status.GetOverlayAddresses() {
		ip, err := netip.ParseAddr(raw)
		if err == nil && ip.Is6() == ipv6 && !ip.Is4In6() {
			return ip
		}
	}
	return netip.Addr{}
}

func TestNativeOverlayAddressUsesExplicitFamily(t *testing.T) {
	status := &ipc.Status{OverlayAddresses: []string{"invalid", "::ffff:192.0.2.1", "2001:db8::1", "192.0.2.2"}}
	if nativeOverlayAddress(status, false).String() != "192.0.2.2" || nativeOverlayAddress(status, true).String() != "2001:db8::1" {
		t.Fatal("native overlay selection mixed address families or accepted mapped IPv4")
	}
	if nativeOverlayAddress(nil, false).IsValid() || nativeOverlayAddress(&ipc.Status{}, true).IsValid() {
		t.Fatal("missing overlay address invented a usable endpoint")
	}
}

// Port comes from current tunnel inspection, never from stale status/legacy DTOs.
func nativeTunnelPort(t *testing.T, n *testclient.Node, status *ipc.Status) uint16 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var port uint16
	var last *ipc.Diagnostics
	var lastRequestError error
	attempts, requestFailures := 0, 0
	err := testclient.Await(ctx, func() bool {
		attempts++
		response := &ipc.GetDiagnosticsResponse{}
		if err := n.NativeService("diagnostics", response, "--profile-id", status.ActiveProfileId, "--timeout", "1s"); err != nil {
			lastRequestError = err // NativeService returns only fixed, sanitized diagnostics.
			requestFailures++
			return false
		}
		d := response.GetDiagnostics()
		last = d
		if d.GetStatus().GetNodeId() != status.NodeId || d.GetStatus().GetActiveProfileId() != status.ActiveProfileId || d.GetStatus().GetMapRevision() < status.MapRevision ||
			!d.GetTunnel().GetOk() || d.GetTunnel().GetFailure() != nil || d.GetTunnel().GetListenPort() == 0 || d.GetTunnel().GetListenPort() > 65535 {
			return false
		}
		port = uint16(d.Tunnel.ListenPort)
		return true
	})
	if err != nil {
		t.Fatalf("native diagnostics did not expose a bound active tunnel port: attempts=%d request_failures=%d last_request_error=%v last={%s}",
			attempts, requestFailures, lastRequestError, nativePeerTunnelSummary(last, status))
	}
	return port
}
