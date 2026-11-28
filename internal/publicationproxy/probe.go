package publicationproxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/netip"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

func probeAvailable(ctx context.Context, host string, routes *connectivity.PublicationRoutes, dial func(context.Context, netip.AddrPort) (net.Conn, error)) bool {
	seen := map[netip.AddrPort]bool{}
	for ctx.Err() == nil {
		target, err := routes.ProbeTarget(host)
		if err != nil || seen[target.Address] {
			return false
		}
		seen[target.Address] = true
		if probeBackend(ctx, host, routes.ProbeTarget, dial) {
			return true
		}
		if !routes.AdvanceProbe(host, target) {
			return false
		}
	}
	return false
}

// Check the exact backend transport without sending an application request.
// Both the destination and upstream TLS name come from the verified map.
func probeBackend(ctx context.Context, host string, resolve func(string) (connectivity.PublicationTarget, error), dial func(context.Context, netip.AddrPort) (net.Conn, error)) bool {
	target, err := resolve(host)
	if err != nil || target.Generation == nil || target.Generation.Err() != nil || !time.Now().Before(target.Until) {
		return false
	}
	ctx, cancel := context.WithDeadline(ctx, target.Until)
	defer cancel()
	stop := context.AfterFunc(target.Generation, cancel)
	defer stop()
	conn, err := dial(ctx, target.Address)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	current := func() bool {
		next, err := resolve(host)
		return err == nil && next == target && ctx.Err() == nil && target.Generation.Err() == nil
	}
	if !current() {
		return false
	}
	if target.HTTPS {
		client := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.TLSName})
		if client.HandshakeContext(ctx) != nil {
			return false
		}
	}
	return current()
}
