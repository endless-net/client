package connectivity

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"time"

	"connectrpc.com/connect"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
)

type lookupDomain func(context.Context, string, string) ([]netip.Addr, error)

// Discovery runs independently of map application. In particular the map
// revision produced by a report must not immediately cause another report.
func (t *Transport) RunDomainDiscovery(ctx context.Context, v *Verifier, maps <-chan Map, interval time.Duration) {
	if t == nil || v == nil || v.config.Clock == nil || interval <= 0 {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	t.runDomainDiscovery(ctx, v, maps, interval, tick.C, net.DefaultResolver.LookupNetIP)
}

func (t *Transport) runDomainDiscovery(ctx context.Context, v *Verifier, maps <-chan Map, interval time.Duration, ticks <-chan time.Time, lookup lookupDomain) {
	var latest Map
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-maps:
			if !ok {
				return
			}
			first := latest.snapshot.MapSignature == nil
			latest = m
			if !first {
				continue
			}
		case <-ticks:
			// A replacement can arrive during DNS or RPC. Prefer it before
			// starting the next round, including when a tick is already due.
			select {
			case m, ok := <-maps:
				if !ok {
					return
				}
				latest = m
			default:
			}
		}
		if latest.Denied() || latest.profileID != v.profileID || latest.generation != v.generation {
			continue
		}
		snapshot := latest.snapshot
		local := coordinator.ServiceHost{NodeID: v.node, PublicKey: v.public}
		for _, app := range snapshot.Network.Applications {
			if ctx.Err() != nil {
				return
			}
			if app.TargetType != "domain" || !slices.Contains(app.Connectors, local) {
				continue
			}
			call, cancel := context.WithTimeout(ctx, t.config.CallTimeout)
			// Lookup and delivery failures leave the previous observation to
			// expire. The next round performs a fresh lookup.
			_ = t.discoverDomain(call, v, latest, app, interval, lookup)
			cancel()
		}
	}
}

func (t *Transport) discoverDomain(ctx context.Context, v *Verifier, m Map, app coordinator.Application, interval time.Duration, lookup lookupDomain) error {
	remaining := func() time.Duration {
		now, err := v.config.Clock.Sample()
		if err != nil {
			return 0
		}
		margin, err := m.WallDeadlineAdvance(now)
		if err != nil {
			return 0
		}
		return m.ExpiresAt().Sub(now.Wall.Add(margin))
	}
	name, valid := dnsDomain(app.Target)
	if !valid || !validDiscoveryHash(app.PolicyHash) || remaining() < time.Second {
		return ErrMap
	}
	select {
	case t.activeCalls <- struct{}{}:
		defer func() { <-t.activeCalls }()
	default:
		return ErrUnavailable
	}
	if _, err := v.currentTrust(ctx, "node:endpoint"); err != nil {
		return err
	}
	answers, err := lookup(ctx, "ip4", name+".")
	if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			return err
		}
		answers = nil
	}
	overlay, err := netip.ParsePrefix(m.snapshot.Network.CIDR)
	if err != nil {
		return ErrMap
	}
	var addresses []string
	for _, ip := range answers {
		ip = ip.Unmap()
		if ip.Is4() && ip.IsGlobalUnicast() && !overlay.Contains(ip) && !slices.Contains(addresses, ip.String()) {
			addresses = append(addresses, ip.String())
			if len(addresses) > 64 {
				return ErrMap
			}
		}
	}
	slices.Sort(addresses)
	// A fresh observation can extend an earlier path. The server clips the
	// requested interval to the router's current membership and endpoint.
	ttl := 2 * interval / time.Second
	if remaining() < time.Second || ttl < 1 || ttl > 1<<32-1 {
		return ErrMap
	}
	req := connect.NewRequest(&wire.ReportApplicationDiscoveryRequest{NodeId: v.node, ApplicationId: app.ID, PolicyHash: app.PolicyHash, Addresses: addresses, TtlSeconds: uint32(ttl)})
	req.Header().Set("X-EndlessNet-Node-Credential", v.credential)
	client := publicv1connect.NewConnectorServiceClient(t.calls, v.origin, connect.WithReadMaxBytes(t.config.MaxResponseBytes), connect.WithSendMaxBytes(t.config.MaxRequestBytes))
	reply, err := client.ReportApplicationDiscovery(ctx, req)
	if err != nil {
		return connectivityCallError(err, false)
	}
	if reply == nil || reply.Msg == nil || len(reply.Msg.ProtoReflect().GetUnknown()) != 0 {
		return ErrProtocol
	}
	return nil
}
