//go:build linux

package client

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const defaultWireGuardEngineRouteTable = uint32(51820)

type linuxWireGuardEngineRouter struct {
	interfaceName string
	runner        CommandRunner
	current       wireGuardEngineRouterConfig
	configured    bool
}

func newPlatformWireGuardEngineRouter(interfaceName string, runner CommandRunner, _ commandInputRunner) (wireGuardEngineRouter, error) {
	if runner == nil {
		runner = runCommand
	}
	return &linuxWireGuardEngineRouter{interfaceName: interfaceName, runner: runner}, nil
}

func platformWireGuardEngineFirewallMark(routes []netip.Prefix, routeTable string) uint32 {
	for _, route := range routes {
		if route.Bits() != 0 {
			continue
		}
		if value, err := strconv.ParseUint(strings.TrimSpace(routeTable), 10, 32); err == nil && value > 0 {
			return uint32(value)
		}
		return defaultWireGuardEngineRouteTable
	}
	return 0
}

func (r *linuxWireGuardEngineRouter) Configure(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	if r.configured {
		r.cleanup(ctx, r.current)
	}
	r.current = cfg
	r.configured = true
	fail := func(err error) error {
		r.cleanup(ctx, cfg)
		r.current = wireGuardEngineRouterConfig{}
		r.configured = false
		return err
	}
	if err := r.run(ctx, "ip", "link", "set", "dev", cfg.Interface, "mtu", strconv.Itoa(cfg.MTU), "up"); err != nil {
		return fail(err)
	}
	for _, family := range []string{"-4", "-6"} {
		_, _ = r.runner(ctx, "ip", family, "addr", "flush", "dev", cfg.Interface, "scope", "global")
	}
	for _, address := range cfg.Addresses {
		family := "-4"
		if address.Addr().Is6() {
			family = "-6"
		}
		if err := r.run(ctx, "ip", family, "addr", "add", address.String(), "dev", cfg.Interface); err != nil {
			return fail(err)
		}
	}
	for _, route := range cfg.Routes {
		if err := r.addRoute(ctx, cfg, route); err != nil {
			return fail(err)
		}
	}
	if linuxShouldConfigureDNS(cfg) {
		args := []string{"dns", cfg.Interface}
		for _, server := range cfg.DNS {
			args = append(args, server.String())
		}
		if err := r.run(ctx, "resolvectl", args...); err != nil {
			return fail(err)
		}
		domains := linuxResolvedDomains(cfg)
		if len(domains) > 0 {
			if err := r.run(ctx, "resolvectl", append([]string{"domain", cfg.Interface}, domains...)...); err != nil {
				return fail(err)
			}
		}
	}
	for _, command := range cfg.PostUp {
		if err := r.runHook(ctx, "apply", command); err != nil {
			return fail(err)
		}
	}
	return nil
}

func linuxResolvedDomains(cfg wireGuardEngineRouterConfig) []string {
	if cfg.DNSOverride || (!cfg.DNSConfigPresent && len(cfg.DNS) > 0) {
		return []string{"~."}
	}
	result := make([]string, 0, len(cfg.DNSDomains)+len(cfg.SearchDomains))
	for _, domain := range cfg.DNSDomains {
		result = append(result, "~"+domain)
	}
	result = append(result, cfg.SearchDomains...)
	return result
}

func linuxShouldConfigureDNS(cfg wireGuardEngineRouterConfig) bool {
	return len(cfg.DNS) > 0 && (!cfg.DNSConfigPresent || cfg.DNSOverride || len(cfg.DNSDomains) > 0 || len(cfg.SearchDomains) > 0)
}

func (r *linuxWireGuardEngineRouter) addRoute(ctx context.Context, cfg wireGuardEngineRouterConfig, route netip.Prefix) error {
	family := "-4"
	if route.Addr().Is6() {
		family = "-6"
	}
	if route.Bits() == 0 && cfg.FirewallMark != 0 {
		table := strconv.FormatUint(uint64(cfg.FirewallMark), 10)
		if family == "-4" {
			if err := r.run(ctx, "sysctl", "-q", "net.ipv4.conf.all.src_valid_mark=1"); err != nil {
				return err
			}
		}
		if err := r.run(ctx, "ip", family, "route", "replace", "default", "dev", cfg.Interface, "table", table); err != nil {
			return err
		}
		_, _ = r.runner(ctx, "ip", family, "rule", "del", "not", "fwmark", table, "table", table)
		_, _ = r.runner(ctx, "ip", family, "rule", "del", "table", "main", "suppress_prefixlength", "0")
		if err := r.run(ctx, "ip", family, "rule", "add", "not", "fwmark", table, "table", table); err != nil {
			return err
		}
		return r.run(ctx, "ip", family, "rule", "add", "table", "main", "suppress_prefixlength", "0")
	}
	args := []string{family, "route", "replace", route.String(), "dev", cfg.Interface}
	if table := linuxUserspaceRouteTable(cfg.RouteTable); table != "" {
		args = append(args, "table", table)
	}
	return r.run(ctx, "ip", args...)
}

func linuxUserspaceRouteTable(value string) string {
	value = strings.TrimSpace(value)
	if _, err := strconv.ParseUint(value, 10, 32); err == nil {
		return value
	}
	return ""
}

func (r *linuxWireGuardEngineRouter) Down(ctx context.Context) error {
	if !r.configured {
		return nil
	}
	r.cleanup(ctx, r.current)
	r.configured = false
	r.current = wireGuardEngineRouterConfig{}
	return nil
}

func (r *linuxWireGuardEngineRouter) cleanup(ctx context.Context, cfg wireGuardEngineRouterConfig) {
	for _, command := range cfg.PreDown {
		_, _ = r.runner(ctx, "sh", "-c", command)
	}
	if linuxShouldConfigureDNS(cfg) {
		_, _ = r.runner(ctx, "resolvectl", "revert", cfg.Interface)
	}
	for _, route := range cfg.Routes {
		family := "-4"
		if route.Addr().Is6() {
			family = "-6"
		}
		if route.Bits() == 0 && cfg.FirewallMark != 0 {
			table := strconv.FormatUint(uint64(cfg.FirewallMark), 10)
			_, _ = r.runner(ctx, "ip", family, "rule", "del", "not", "fwmark", table, "table", table)
			_, _ = r.runner(ctx, "ip", family, "rule", "del", "table", "main", "suppress_prefixlength", "0")
			_, _ = r.runner(ctx, "ip", family, "route", "del", "default", "dev", cfg.Interface, "table", table)
			continue
		}
		args := []string{family, "route", "del", route.String(), "dev", cfg.Interface}
		if table := linuxUserspaceRouteTable(cfg.RouteTable); table != "" {
			args = append(args, "table", table)
		}
		_, _ = r.runner(ctx, "ip", args...)
	}
	for _, family := range []string{"-4", "-6"} {
		_, _ = r.runner(ctx, "ip", family, "addr", "flush", "dev", cfg.Interface, "scope", "global")
	}
	_, _ = r.runner(ctx, "ip", "link", "set", "dev", cfg.Interface, "down")
}

func (r *linuxWireGuardEngineRouter) runHook(ctx context.Context, action, command string) error {
	out, err := r.runner(ctx, "sh", "-c", command)
	if err != nil {
		return fmt.Errorf("%s wireguard-go firewall policy: %s", action, secretSafeCommandError(err, out))
	}
	return nil
}

func secretSafeCommandError(err error, output []byte) string {
	message := commandError(err, output)
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), "privatekey") {
			lines[i] = "[redacted private key line]"
		}
	}
	return strings.Join(lines, "\n")
}

func (r *linuxWireGuardEngineRouter) run(ctx context.Context, name string, args ...string) error {
	out, err := r.runner(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), commandError(err, out))
	}
	return nil
}
