package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
)

const applicationDiscoveryLeaseSeconds = 60

type applicationResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func (e *WireGuardEngine) configureApplicationsLocked(cfg Config, m clientapi.RegisterNodeResponse) {
	if e.applicationFilter != nil && (len(m.Network.Applications) > 0 || e.applicationFilter.active()) {
		e.applicationFilter.update(m)
	}
	if e.applicationCancel != nil {
		e.applicationCancel()
		e.applicationCancel = nil
	}
	// Sources run on every supported client OS. Connector forwarding currently
	// uses Linux routing/NAT; unsupported connectors must never advertise leases.
	if runtime.GOOS != "linux" {
		return
	}
	selected := false
	for _, app := range m.Network.Applications {
		selected = selected || slices.Contains(app.Connectors, applicationSelf(m))
	}
	if !selected {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.applicationCancel = cancel
	m = cloneRegisterNodeResponse(m)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			reportApplicationDiscoveries(ctx, cfg, m, net.DefaultResolver, applicationHTTPClient())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func applicationHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func reportApplicationDiscoveries(ctx context.Context, cfg Config, m clientapi.RegisterNodeResponse, resolver applicationResolver, httpClient *http.Client) {
	if verifyApplicationMap(cfg, m) != nil || cfg.NodeCredential == "" {
		return
	}
	for _, app := range m.Network.Applications {
		if !slices.Contains(app.Connectors, applicationSelf(m)) {
			continue
		}
		target, err := clientapi.ParseApplicationTarget(app.TargetType, app.Target)
		if err != nil {
			continue
		}
		var addresses []string
		if target.Domain != "" {
			resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			resolved, err := resolver.LookupNetIP(resolveCtx, "ip", target.Domain)
			cancel()
			if err == nil {
				for _, address := range resolved {
					address = address.Unmap()
					if clientapi.ValidateApplicationAddress(address) != nil {
						continue
					}
					addresses = append(addresses, address.String())
				}
			}
			sort.Strings(addresses)
			addresses = slices.Compact(addresses)
			if len(addresses) > 64 {
				addresses = nil
			}
		}
		for _, endpoint := range cfg.ControlURLs() {
			base, err := url.Parse(endpoint)
			if err != nil || base.Host == "" || base.User != nil || base.Scheme != "https" && base.Scheme != "http" {
				continue
			}
			base.Path, base.RawPath, base.RawQuery, base.Fragment = "", "", "", ""
			client := clientrpcconnect.NewConnectorServiceClient(httpClient, base.String())
			request := connect.NewRequest(&clientrpc.ReportApplicationDiscoveryRequest{NodeId: m.Node.ID, ApplicationId: app.ID, PolicyHash: app.PolicyHash, Addresses: addresses, TtlSeconds: applicationDiscoveryLeaseSeconds})
			request.Header().Set("Authorization", "Bearer "+cfg.NodeCredential)
			if _, err := client.ReportApplicationDiscovery(ctx, request); err == nil {
				break
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
}

// The TUN filter authorizes every forwarded packet before these OS rules run.
// Lease expiry therefore also stops flows already tracked by conntrack.
func renderApplicationForwardingHooks(m clientapi.RegisterNodeResponse) []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	var hooks []string
	seen := map[string]bool{}
	for _, app := range m.Network.Applications {
		for _, route := range app.Routes {
			if route.Connector != applicationSelf(m) || !time.Now().Before(route.ExpiresAt) {
				continue
			}
			for _, value := range route.CIDRs {
				if seen[value] {
					continue
				}
				seen[value] = true
				p, err := netip.ParsePrefix(value)
				if err != nil {
					continue
				}
				tool, family, overlay, sysctl := "iptables", "-4", m.Network.CIDR, "net.ipv4.ip_forward"
				if p.Addr().Is6() {
					tool, family, overlay, sysctl = "ip6tables", "-6", m.Network.IPv6CIDR, "net.ipv6.conf.all.forwarding"
				}
				if _, err := netip.ParsePrefix(overlay); err != nil {
					continue
				}
				probe := subnetRouterSNATProbeAddress(p)
				setup := fmt.Sprintf("lan_if=\"$(ip %s route get %s | awk '{for (i=1;i<=NF;i++) if ($i==\"dev\") {print $(i+1); exit}}')\"; test -n \"$lan_if\"", family, probe)
				rules := []string{fmt.Sprintf("FORWARD -i %%i -o \"$lan_if\" -s %s -d %s -j ACCEPT", overlay, p), fmt.Sprintf("FORWARD -i \"$lan_if\" -o %%i -s %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", p, overlay), fmt.Sprintf("POSTROUTING -s %s -d %s -o \"$lan_if\" -j MASQUERADE", overlay, p)}
				up, down := fmt.Sprintf("PostUp = sysctl -w %s=1 >/dev/null; %s", sysctl, setup), "PreDown = "+setup
				for i, rule := range rules {
					command := tool
					if i == 2 {
						command += " -t nat"
					}
					up += fmt.Sprintf("; %s -C %s 2>/dev/null || %s -A %s", command, rule, command, rule)
					down += fmt.Sprintf("; %s -D %s 2>/dev/null || true", command, rule)
				}
				hooks = append(hooks, strings.TrimSpace(up), strings.TrimSpace(down))
			}
		}
	}
	return hooks
}
