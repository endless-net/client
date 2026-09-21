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
)

// linuxExitGuard owns only its interface-specific inet table. Kernel rules
// survive process termination and interface deletion. There is deliberately no
// automatic cleanup on an apply error or cancellation.
//
// This is the OS protection component, not a complete exit adapter. The caller
// must serialize it with engine changes, install Contain before changing routes,
// and call OpenTunnel only after the authenticated TUN filters are installed.
// Release is permitted only after an explicit clear and confirmed route cleanup.
// LAN bypass is not supported by this component.
type linuxExitGuard struct {
	mu            sync.Mutex
	interfaceName string
	mark          uint32
	table         string
	run           commandInputRunner
}

func newLinuxExitGuard(interfaceName string, mark uint32, runner commandInputRunner) (*linuxExitGuard, error) {
	if strings.TrimSpace(interfaceName) != interfaceName || !safeWireGuardInterfaceName(interfaceName) || interfaceName == "lo" || mark == 0 {
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
	return g.replace(ctx, false)
}

func (g *linuxExitGuard) OpenTunnel(ctx context.Context) error {
	return g.replace(ctx, true)
}

func (g *linuxExitGuard) replace(ctx context.Context, tunnel bool) error {
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
	if tunnel {
		fmt.Fprintf(&b, "add rule inet %s output oifname %q accept\n", g.table, g.interfaceName)
	}
	// Forwarding remains closed: this guard does not implement an exit provider.
	return g.apply(ctx, b.String())
}

func (g *linuxExitGuard) Release(ctx context.Context) error {
	// Creating an absent table and deleting it in the same transaction makes
	// crash/restart cleanup idempotent without parsing localized command errors.
	return g.apply(ctx, fmt.Sprintf("add table inet %s\ndelete table inet %s\n", g.table, g.table))
}

func (g *linuxExitGuard) apply(ctx context.Context, batch string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
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
	return nil
}
