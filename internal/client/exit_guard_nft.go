package client

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// linuxExitGuard owns only its interface-specific inet table. Kernel rules
// survive process termination and interface deletion. There is deliberately no
// automatic cleanup on an apply error or cancellation.
//
// This is the OS protection component, not a complete exit adapter. The caller
// must serialize it with engine changes, install Contain before changing routes,
// and call OpenTunnel only after the authenticated TUN filters are installed.
// Release is permitted only after an explicit clear and confirmed route cleanup.
// LAN exceptions require the separately observed, leased BPF packet gate.
type linuxExitGuard struct {
	mu            sync.Mutex
	interfaceName string
	mark          uint32
	table         string
	run           commandInputRunner
	lan           *exitLANFirewallState
}

func newLinuxExitGuard(interfaceName string, mark uint32, runner commandInputRunner) (*linuxExitGuard, error) {
	if strings.TrimSpace(interfaceName) != interfaceName || !safeWireGuardInterfaceName(interfaceName) || interfaceName == "lo" || mark == 0 || mark == 253 || mark == 254 || mark == 255 {
		return nil, errors.New("invalid Linux exit guard identity")
	}
	if runner == nil {
		if runtime.GOOS != "linux" {
			return nil, errors.New("exit guard requires Linux")
		}
		runner = runExitCommand
	}
	scope := sha256.Sum256([]byte(interfaceName))
	return &linuxExitGuard{interfaceName: interfaceName, mark: mark,
		table: fmt.Sprintf("endlessnet_exit_%x", scope[:12]), run: runner}, nil
}

// Contain blocks both IP families outside loopback, marked TCP/UDP underlay and
// host Neighbor Discovery, including physical-default application traffic.
// It also closes TUN egress while routes, identity or packet filters are changing.
func (g *linuxExitGuard) Contain(ctx context.Context) error {
	return g.replace(ctx, false, "")
}

func (g *linuxExitGuard) OpenTunnel(ctx context.Context, family api.ExitFamilyMode) error {
	if !exitGuardFamilyValid(family) {
		return errors.New("invalid exit guard family")
	}
	g.mu.Lock()
	g.lan = nil
	g.mu.Unlock()
	return g.replace(ctx, true, family)
}

func (g *linuxExitGuard) replace(ctx context.Context, tunnel bool, family api.ExitFamilyMode) error {
	return g.apply(ctx, g.rulesBatch(tunnel, family), tunnel, false, family)
}

func (g *linuxExitGuard) rulesBatch(tunnel bool, family api.ExitFamilyMode) string {
	// Recreate the exclusively owned table in one netlink transaction. Flushing
	// rules alone would retain old chain hooks and table flags after restart.
	// The initial idempotent add also permits first use when the table is absent.
	// Never submit deletion separately: failure must preserve the prior table.
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", g.table)
	fmt.Fprintf(&b, "delete table inet %s\nadd table inet %s\n", g.table, g.table)
	fmt.Fprintf(&b, "add chain inet %s output { type filter hook output priority 0; policy drop; }\n", g.table)
	fmt.Fprintf(&b, "add chain inet %s forward { type filter hook forward priority 0; policy drop; }\n", g.table)
	fmt.Fprintf(&b, "add rule inet %s output oifname \"lo\" accept\n", g.table)
	// Never allow all established traffic: pre-existing direct connections must
	// be blocked too. SO_MARK is reserved for the privileged tunnel transport.
	fmt.Fprintf(&b, "add rule inet %s output meta mark %d meta l4proto { tcp, udp } accept\n", g.table, g.mark)
	// The physical IPv6 underlay still needs address resolution, DAD and router
	// solicitation. RFC 4861 requires hop limit 255 and ICMP code 0. Do not open
	// the guarded TUN, echo traffic, router advertisements or redirects here.
	fmt.Fprintf(&b, "add rule inet %s output oifname != %q ip6 hoplimit 255 icmpv6 type { nd-router-solicit, nd-neighbor-solicit, nd-neighbor-advert } icmpv6 code 0 accept\n", g.table, g.interfaceName)
	b.WriteString(exitGuardDHCPBatch(g.table, g.interfaceName))
	if tunnel {
		selected, ordinary := exitGuardFamilyProtocols(family)
		if ordinary != "" {
			fmt.Fprintf(&b, "add rule inet %s output meta nfproto %s accept\n", g.table, ordinary)
			fmt.Fprintf(&b, "add rule inet %s forward meta nfproto %s accept\n", g.table, ordinary)
			fmt.Fprintf(&b, "add rule inet %s output meta nfproto %s oifname %q accept\n", g.table, selected, g.interfaceName)
		} else {
			fmt.Fprintf(&b, "add rule inet %s output oifname %q accept\n", g.table, g.interfaceName)
		}
	}
	if tunnel && g.lan != nil {
		batch, _, _, err := g.lan.rules(g.table, g.mark)
		if err == nil {
			fmt.Fprintf(&b, "add chain inet %s lanroute { type route hook output priority -150; policy accept; }\n", g.table)
			fmt.Fprintf(&b, "add chain inet %s lanfinal { type filter hook postrouting priority 2147483645; policy accept; }\n", g.table)
			b.WriteString(batch)
		}
	}
	// Selected-family forwarding remains closed; this is not an exit provider.
	return b.String()
}

func exitGuardFamilyValid(family api.ExitFamilyMode) bool {
	return family == api.ExitFamilyIPv4Only || family == api.ExitFamilyIPv6Only || family == api.ExitFamilyDualStack
}

func exitGuardFamilyProtocols(family api.ExitFamilyMode) (selected, ordinary string) {
	switch family {
	case api.ExitFamilyIPv4Only:
		return "ipv4", "ipv6"
	case api.ExitFamilyIPv6Only:
		return "ipv6", "ipv4"
	default:
		return "", ""
	}
}

func (g *linuxExitGuard) Observe(ctx context.Context, family api.ExitFamilyMode) error {
	if !exitGuardFamilyValid(family) {
		return errors.New("invalid exit guard family")
	}
	return g.observe(ctx, true, false, family)
}
func (g *linuxExitGuard) ObserveContained(ctx context.Context) error {
	return g.observe(ctx, false, false, "")
}
func (g *linuxExitGuard) ObserveAbsent(ctx context.Context) error {
	return g.observe(ctx, false, true, "")
}
func (g *linuxExitGuard) observe(ctx context.Context, tunnel, absent bool, family api.ExitFamilyMode) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !lockExitRuntime(ctx, &g.mu) {
		return ctx.Err()
	}
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	args := []string{"-j", "-n", "list", "table", "inet", g.table}
	if absent {
		args = []string{"-j", "-n", "list", "tables"}
	}
	raw, err := g.run(ctx, "", "nft", args...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("exit guard readback failed")
	}
	if absent {
		if !exitGuardTableAbsent(raw, g.table) {
			return errors.New("exit guard release is not confirmed")
		}
	} else if !exitGuardRulesObserved(raw, g.table, g.interfaceName, g.mark, tunnel, family, g.lan) {
		return errors.New("exit guard enforcement is not confirmed")
	}
	return nil
}

func (g *linuxExitGuard) Release(ctx context.Context) error {
	// Creating an absent table and deleting it in the same transaction makes
	// crash/restart cleanup idempotent without parsing localized command errors.
	return g.apply(ctx, fmt.Sprintf("add table inet %s\ndelete table inet %s\n", g.table, g.table), false, true, "")
}

func (g *linuxExitGuard) apply(ctx context.Context, batch string, tunnel, release bool, family api.ExitFamilyMode) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	err := g.applyObserved(ctx, batch, tunnel, release, family)
	if err != nil && (tunnel || release) {
		// Either transaction could have opened traffic before cancellation or a
		// failed readback. Restore closed protection with a separately bounded
		// context; never turn an ambiguous result into success.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(err, g.applyObserved(recovery, g.rulesBatch(false, ""), false, false, ""))
	}
	return err
}

func (g *linuxExitGuard) applyObserved(ctx context.Context, batch string, tunnel, release bool, family api.ExitFamilyMode) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := g.run(ctx, batch, "nft", "-f", "-")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		// Command output can contain host details. The caller retains its durable
		// operation and guard on any error; neither absence nor cleanup is inferred.
		return errors.New("exit guard transaction failed")
	}
	var raw []byte
	if release {
		raw, err = g.run(ctx, "", "nft", "-j", "-n", "list", "tables")
	} else {
		raw, err = g.run(ctx, "", "nft", "-j", "-n", "list", "table", "inet", g.table)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("exit guard readback failed")
	}
	if release {
		if !exitGuardTableAbsent(raw, g.table) {
			return errors.New("exit guard release is not confirmed")
		}
	} else if !exitGuardRulesObserved(raw, g.table, g.interfaceName, g.mark, tunnel, family, g.lan) {
		return errors.New("exit guard enforcement is not confirmed")
	}
	return ctx.Err()
}
