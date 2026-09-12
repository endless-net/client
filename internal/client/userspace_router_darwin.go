//go:build darwin

package client

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"slices"
	"strings"
)

func platformPrepareDNSProxy(_ *wireGuardEngineRouterConfig) {}

type darwinWireGuardEngineRouter struct {
	interfaceName string
	runner        CommandRunner
	inputRunner   commandInputRunner
	current       wireGuardEngineRouterConfig
	configured    bool
}

func runCommandInput(ctx context.Context, input string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}

func newPlatformWireGuardEngineRouter(interfaceName string, runner CommandRunner, inputRunner commandInputRunner) (wireGuardEngineRouter, error) {
	if runner == nil {
		runner = runCommand
	}
	if inputRunner == nil {
		inputRunner = runCommandInput
	}
	return &darwinWireGuardEngineRouter{interfaceName: interfaceName, runner: runner, inputRunner: inputRunner}, nil
}

func platformWireGuardEngineFirewallMark(_ []netip.Prefix, _ string) uint32 { return 0 }

func (r *darwinWireGuardEngineRouter) Configure(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	if r.configured {
		withoutRouteChange := cfg
		withoutRouteChange.Routes = r.current.Routes
		if wireGuardEnginePlatformRouterConfigsEqual(withoutRouteChange, r.current) {
			return r.reconcileRoutes(ctx, cfg)
		}
	}
	if r.configured {
		_ = r.Down(ctx)
	}
	// Failed setup owns only routes whose add command succeeded. In
	// particular, EEXIST does not grant ownership of another interface's route.
	applied := cfg
	applied.Routes = nil
	fail := func(err error) error {
		_ = r.cleanup(ctx, applied)
		return err
	}
	if out, err := r.runner(ctx, "ifconfig", cfg.Interface, "mtu", fmt.Sprint(cfg.MTU), "up"); err != nil {
		return fail(fmt.Errorf("configure darwin TUN: %s", commandError(err, out)))
	}
	for _, address := range cfg.Addresses {
		family := "inet"
		if address.Addr().Is6() {
			family = "inet6"
		}
		args := []string{cfg.Interface, family, address.String()}
		if address.Addr().Is4() {
			// utun is point-to-point: Darwin requires an IPv4 destination,
			// even though WireGuard selects the remote peer from its routes.
			args = append(args, address.Addr().String())
		}
		args = append(args, "alias")
		if out, err := r.runner(ctx, "ifconfig", args...); err != nil {
			return fail(fmt.Errorf("configure darwin TUN address: %s", commandError(err, out)))
		}
	}
	for _, route := range darwinSystemRoutes(cfg.Routes) {
		family := "-inet"
		if route.Addr().Is6() {
			family = "-inet6"
		}
		if out, err := r.runner(ctx, "route", "-n", "add", family, route.String(), "-interface", cfg.Interface); err != nil {
			return fail(fmt.Errorf("configure darwin TUN route: %s", commandError(err, out)))
		}
		applied.Routes = append(applied.Routes, route)
	}
	if darwinShouldConfigureDNS(cfg) {
		if out, err := r.inputRunner(ctx, darwinUserspaceDNSCommands(cfg), "scutil"); err != nil {
			return fail(fmt.Errorf("configure darwin wireguard-go DNS: %s", commandError(err, out)))
		}
	}
	r.current = cfg
	r.configured = true
	return nil
}

// Route-only map changes must not flap utun: asynchronous interface-down
// events can stop WireGuard while its new peer configuration is being applied.
func (r *darwinWireGuardEngineRouter) reconcileRoutes(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	change := func(operation string, route netip.Prefix) error {
		changed := make([]netip.Prefix, 0, 2)
		for _, systemRoute := range darwinSystemRoutes([]netip.Prefix{route}) {
			family := "-inet"
			if systemRoute.Addr().Is6() {
				family = "-inet6"
			}
			out, err := r.runner(ctx, "route", "-n", operation, family, systemRoute.String(), "-interface", cfg.Interface)
			if err != nil {
				rollback := "delete"
				if operation == "delete" {
					rollback = "add"
				}
				for i := len(changed) - 1; i >= 0; i-- {
					rollbackFamily := "-inet"
					if changed[i].Addr().Is6() {
						rollbackFamily = "-inet6"
					}
					_, _ = r.runner(ctx, "route", "-n", rollback, rollbackFamily, changed[i].String(), "-interface", cfg.Interface)
				}
				return fmt.Errorf("%s darwin TUN route: %s", operation, commandError(err, out))
			}
			changed = append(changed, systemRoute)
		}
		return nil
	}
	// Track each successful mutation so engine rollback can reconcile back to
	// the previous map even if a later route command fails.
	for _, route := range slices.Clone(r.current.Routes) {
		if !slices.Contains(cfg.Routes, route) {
			if err := change("delete", route); err != nil {
				return err
			}
			r.current.Routes = slices.DeleteFunc(slices.Clone(r.current.Routes), func(p netip.Prefix) bool { return p == route })
		}
	}
	for _, route := range cfg.Routes {
		if !slices.Contains(r.current.Routes, route) {
			if err := change("add", route); err != nil {
				return err
			}
			r.current.Routes = append(slices.Clone(r.current.Routes), route)
		}
	}
	r.current = cloneWireGuardEngineRouterConfig(cfg)
	return nil
}

// Darwin already has a default route for the physical interface. Installing a
// second /0 either fails with EEXIST or replaces the route that keeps the host
// online. Two /1 routes win by longest-prefix match while leaving that default
// route intact for recovery on withdrawal. Endpoint reachability while these
// routes are active requires separate underlay routing; retaining /0 alone
// does not bypass the more specific tunnel routes.
func darwinSystemRoutes(routes []netip.Prefix) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(routes)+2)
	for _, route := range routes {
		if route.Bits() != 0 {
			result = append(result, route)
			continue
		}
		if route.Addr().Is4() {
			result = append(result, netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1"))
		} else {
			result = append(result, netip.MustParsePrefix("::/1"), netip.MustParsePrefix("8000::/1"))
		}
	}
	return result
}

func (r *darwinWireGuardEngineRouter) Down(ctx context.Context) error {
	if !r.configured {
		return nil
	}
	r.cleanup(ctx, r.current)
	r.configured = false
	r.current = wireGuardEngineRouterConfig{}
	return nil
}

func (r *darwinWireGuardEngineRouter) cleanup(ctx context.Context, cfg wireGuardEngineRouterConfig) error {
	if darwinShouldConfigureDNS(cfg) {
		_, _ = r.inputRunner(ctx, darwinUserspaceDNSRemoveCommands(cfg.Interface), "scutil")
	}
	for _, route := range darwinSystemRoutes(cfg.Routes) {
		family := "-inet"
		if route.Addr().Is6() {
			family = "-inet6"
		}
		_, _ = r.runner(ctx, "route", "-n", "delete", family, route.String(), "-interface", cfg.Interface)
	}
	_, _ = r.runner(ctx, "ifconfig", cfg.Interface, "down")
	return nil
}

func darwinUserspaceDNSKey(interfaceName string) string {
	return "State:/Network/Service/EndlessNet-" + interfaceName + "/DNS"
}

func darwinUserspaceDNSCommands(cfg wireGuardEngineRouterConfig) string {
	servers := make([]string, 0, len(cfg.DNS))
	for _, server := range cfg.DNS {
		servers = append(servers, server.String())
	}
	domains := append([]string(nil), cfg.DNSDomains...)
	if cfg.DNSOverride || !cfg.DNSConfigPresent {
		domains = []string{"."}
	}
	if len(domains) == 0 {
		domains = append(domains, cfg.SearchDomains...)
	}
	commands := []string{
		"open",
		"d.init",
		"d.add ServerAddresses * " + strings.Join(servers, " "),
		"d.add InterfaceName " + cfg.Interface,
		"d.add SupplementalMatchDomains * " + strings.Join(domains, " "),
		"d.add SupplementalMatchDomainsNoSearch # 1",
	}
	if len(cfg.SearchDomains) > 0 {
		commands = append(commands, "d.add SearchDomains * "+strings.Join(cfg.SearchDomains, " "))
	}
	commands = append(commands,
		"set "+darwinUserspaceDNSKey(cfg.Interface),
		"quit",
		"",
	)
	return strings.Join(commands, "\n")
}

func darwinShouldConfigureDNS(cfg wireGuardEngineRouterConfig) bool {
	return len(cfg.DNS) > 0 && (!cfg.DNSConfigPresent || cfg.DNSOverride || len(cfg.DNSDomains) > 0 || len(cfg.SearchDomains) > 0)
}

func darwinUserspaceDNSRemoveCommands(interfaceName string) string {
	return "open\nremove " + darwinUserspaceDNSKey(interfaceName) + "\nquit\n"
}
