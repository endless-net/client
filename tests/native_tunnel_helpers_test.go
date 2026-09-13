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
	err := testclient.Await(ctx, func() bool {
		response := &ipc.GetDiagnosticsResponse{}
		if n.NativeService("diagnostics", response, "--profile-id", status.ActiveProfileId, "--timeout", "1s") != nil {
			return false
		}
		d := response.GetDiagnostics()
		if d.GetStatus().GetNodeId() != status.NodeId || d.GetStatus().GetActiveProfileId() != status.ActiveProfileId || d.GetStatus().GetMapRevision() < status.MapRevision ||
			!d.GetTunnel().GetOk() || d.GetTunnel().GetFailure() != nil || d.GetTunnel().GetListenPort() == 0 || d.GetTunnel().GetListenPort() > 65535 {
			return false
		}
		port = uint16(d.Tunnel.ListenPort)
		return true
	})
	if err != nil {
		t.Fatal("native diagnostics did not expose a bound active tunnel port")
	}
	return port
}
