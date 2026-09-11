//go:build windows

package client

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

type windowsWireGuardEngineRouter struct {
	interfaceName string
	runner        CommandRunner
	configured    bool
	current       wireGuardEngineRouterConfig
}

func newPlatformWireGuardEngineRouter(interfaceName string, runner CommandRunner, _ commandInputRunner) (wireGuardEngineRouter, error) {
	if runner == nil {
		runner = runCommand
	}
	return &windowsWireGuardEngineRouter{interfaceName: interfaceName, runner: runner}, nil
}

func platformWireGuardEngineFirewallMark(_ []netip.Prefix, _ string) uint32 { return 0 }

func (r *windowsWireGuardEngineRouter) Configure(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	script := windowsUserspaceRouterScript(cfg, false)
	if r.configured {
		withoutRouteChange := cfg
		withoutRouteChange.Routes = r.current.Routes
		if wireGuardEngineRouterConfigsEqual(withoutRouteChange, r.current) {
			if slices.Equal(cfg.Routes, r.current.Routes) {
				return nil
			}
			script = windowsUserspaceRouteUpdateScript(r.current, cfg)
		}
	}
	r.configured = true
	out, err := r.runner(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		_, _ = r.runner(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsUserspaceRouterScript(wireGuardEngineRouterConfig{Interface: r.interfaceName}, true))
		r.configured = false
		r.current = wireGuardEngineRouterConfig{}
		return fmt.Errorf("configure Windows wireguard-go interface: %s", commandError(err, out))
	}
	r.current = cloneWireGuardEngineRouterConfig(cfg)
	return nil
}

func (r *windowsWireGuardEngineRouter) Down(ctx context.Context) error {
	if !r.configured {
		return nil
	}
	cfg := wireGuardEngineRouterConfig{Interface: r.interfaceName}
	out, err := r.runner(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsUserspaceRouterScript(cfg, true))
	r.configured = false
	r.current = wireGuardEngineRouterConfig{}
	if err != nil {
		return fmt.Errorf("remove Windows wireguard-go interface configuration: %s", commandError(err, out))
	}
	return nil
}

// Updating routes must not delete/recreate the interface addresses: that
// interrupts established flows and starts duplicate-address detection again.
func windowsUserspaceRouteUpdateScript(previous, next wireGuardEngineRouterConfig) string {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference='Stop';")
	fmt.Fprintf(&b, "$ifName=%s;", quotePowerShellSingle(next.Interface))
	for _, route := range previous.Routes {
		if !slices.Contains(next.Routes, route) {
			fmt.Fprintf(&b, "Remove-NetRoute -DestinationPrefix %s -InterfaceAlias $ifName -PolicyStore ActiveStore -Confirm:$false;", quotePowerShellSingle(route.String()))
		}
	}
	for _, route := range next.Routes {
		if !slices.Contains(previous.Routes, route) {
			nextHop := "0.0.0.0"
			if route.Addr().Is6() {
				nextHop = "::"
			}
			fmt.Fprintf(&b, "New-NetRoute -DestinationPrefix %s -InterfaceAlias $ifName -NextHop %s -RouteMetric 5 -PolicyStore ActiveStore | Out-Null;", quotePowerShellSingle(route.String()), quotePowerShellSingle(nextHop))
		}
	}
	return b.String()
}

func windowsUserspaceRouterScript(cfg wireGuardEngineRouterConfig, down bool) string {
	iface := quotePowerShellSingle(cfg.Interface)
	var b strings.Builder
	b.WriteString("$ErrorActionPreference='Stop';")
	fmt.Fprintf(&b, "$ifName=%s;", iface)
	b.WriteString("Get-NetRoute -InterfaceAlias $ifName -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object {$_.Protocol -eq 'NetMgmt'} | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue;")
	b.WriteString("Get-NetIPAddress -InterfaceAlias $ifName -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object {$_.PrefixOrigin -eq 'Manual'} | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue;")
	b.WriteString("Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -like ('EndlessNet-'+$ifName+'-*')} | Remove-DnsClientNrptRule -Force -ErrorAction SilentlyContinue;")
	if down {
		b.WriteString("Set-DnsClientServerAddress -InterfaceAlias $ifName -ResetServerAddresses -ErrorAction SilentlyContinue;")
		b.WriteString("Set-DnsClient -InterfaceAlias $ifName -ConnectionSpecificSuffix '' -ErrorAction SilentlyContinue;")
		return b.String()
	}
	fmt.Fprintf(&b, "Set-NetIPInterface -InterfaceAlias $ifName -NlMtuBytes %d;", cfg.MTU)
	for _, address := range cfg.Addresses {
		fmt.Fprintf(&b, "New-NetIPAddress -InterfaceAlias $ifName -IPAddress %s -PrefixLength %d -PolicyStore ActiveStore | Out-Null;", quotePowerShellSingle(address.Addr().String()), address.Bits())
	}
	for _, route := range cfg.Routes {
		nextHop := "0.0.0.0"
		if route.Addr().Is6() {
			nextHop = "::"
		}
		fmt.Fprintf(&b, "New-NetRoute -DestinationPrefix %s -InterfaceAlias $ifName -NextHop %s -RouteMetric 5 -PolicyStore ActiveStore | Out-Null;", quotePowerShellSingle(route.String()), quotePowerShellSingle(nextHop))
	}
	if len(cfg.DNS) > 0 && (cfg.DNSOverride || !cfg.DNSConfigPresent) {
		values := make([]string, 0, len(cfg.DNS))
		for _, server := range cfg.DNS {
			values = append(values, quotePowerShellSingle(server.String()))
		}
		fmt.Fprintf(&b, "Set-DnsClientServerAddress -InterfaceAlias $ifName -ServerAddresses @(%s);", strings.Join(values, ","))
	} else {
		b.WriteString("Set-DnsClientServerAddress -InterfaceAlias $ifName -ResetServerAddresses -ErrorAction SilentlyContinue;")
	}
	if cfg.DNSConfigPresent && !cfg.DNSOverride && len(cfg.DNS) > 0 {
		for index, domain := range cfg.DNSDomains {
			fmt.Fprintf(&b, "Add-DnsClientNrptRule -Namespace %s -NameServers %s -DisplayName ('EndlessNet-'+$ifName+'-%d') | Out-Null;", quotePowerShellSingle("."+strings.TrimSuffix(domain, ".")), quotePowerShellSingle(cfg.DNS[0].String()), index)
		}
	}
	if len(cfg.SearchDomains) > 0 {
		fmt.Fprintf(&b, "Set-DnsClient -InterfaceAlias $ifName -ConnectionSpecificSuffix %s;", quotePowerShellSingle(strings.TrimSuffix(cfg.SearchDomains[0], ".")))
	} else {
		b.WriteString("Set-DnsClient -InterfaceAlias $ifName -ConnectionSpecificSuffix '' -ErrorAction SilentlyContinue;")
	}
	return b.String()
}
