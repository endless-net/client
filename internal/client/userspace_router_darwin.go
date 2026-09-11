//go:build darwin

package client

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

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
		_ = r.Down(ctx)
	}
	fail := func(err error) error {
		_ = r.cleanup(ctx, cfg)
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
	for _, route := range cfg.Routes {
		family := "-inet"
		if route.Addr().Is6() {
			family = "-inet6"
		}
		if out, err := r.runner(ctx, "route", "-n", "add", family, route.String(), "-interface", cfg.Interface); err != nil && !strings.Contains(strings.ToLower(string(out)), "file exists") {
			return fail(fmt.Errorf("configure darwin TUN route: %s", commandError(err, out)))
		}
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
	for _, route := range cfg.Routes {
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
