//go:build darwin

package client

import (
	"context"
	"errors"
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
	for _, route := range splitDefaultRoutes(cfg.Routes) {
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
	previous := splitDefaultRoutes(r.current.Routes)
	desired := splitDefaultRoutes(cfg.Routes)
	installed := slices.Clone(previous)
	type mutation struct {
		operation string
		route     netip.Prefix
	}
	var changes []mutation
	change := func(m mutation) error {
		family := "-inet"
		if m.route.Addr().Is6() {
			family = "-inet6"
		}
		out, err := r.runner(ctx, "route", "-n", m.operation, family, m.route.String(), "-interface", cfg.Interface)
		if err != nil {
			return fmt.Errorf("%s darwin TUN route: %s", m.operation, commandError(err, out))
		}
		if m.operation == "add" {
			installed = append(installed, m.route)
		} else {
			installed = slices.DeleteFunc(installed, func(p netip.Prefix) bool { return p == m.route })
		}
		return nil
	}
	apply := func(m mutation) error {
		if err := change(m); err != nil {
			var rollbackErr error
			for i := len(changes) - 1; i >= 0; i-- {
				inverse := changes[i]
				if inverse.operation == "add" {
					inverse.operation = "delete"
				} else {
					inverse.operation = "add"
				}
				rollbackErr = errors.Join(rollbackErr, change(inverse))
			}
			if rollbackErr != nil {
				// Keep the actual successful mutations so later recovery or Down
				// does not mistake the old logical map for installed OS routes.
				r.current.Routes = slices.Clone(installed)
			}
			return errors.Join(err, rollbackErr)
		}
		changes = append(changes, m)
		return nil
	}
	for _, route := range previous {
		if !slices.Contains(desired, route) {
			if err := apply(mutation{"delete", route}); err != nil {
				return err
			}
		}
	}
	for _, route := range desired {
		if !slices.Contains(previous, route) {
			if err := apply(mutation{"add", route}); err != nil {
				return err
			}
		}
	}
	r.current = cloneWireGuardEngineRouterConfig(cfg)
	return nil
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
	for _, route := range splitDefaultRoutes(cfg.Routes) {
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
