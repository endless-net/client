package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func linuxPolicyFixture() (linuxFilterIdentity, linuxFilterPlan) {
	local := netip.MustParseAddr("100.96.0.1")
	remote := netip.MustParseAddr("100.96.0.2")
	return linuxFilterIdentity{table: "en0123456789ab", tunnel: "en0123456789ab", owner: "e1bbf427-b4d5-4e6e-b745-68e908a45ee3", zone: 1234, mark: 56789},
		linuxFilterPlan{address: local, protected: []netip.Addr{local, remote},
			grants: []linuxFlowGrant{
				{source: local, destination: remote, protocol: "tcp", port: 8000, egress: true},
				{source: remote, destination: local, protocol: "udp", port: 5300},
			},
		}
}

func TestLinuxFilterRejectsUnownedPathsAndDoesNotRenewOffline(t *testing.T) {
	id, plan := linuxPolicyFixture()
	online, err := plan.transaction(id, true, linuxOnline, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"create table inet en0123456789ab",
		`output oifname "en0123456789ab" ip saddr 100.96.0.1 ip daddr 100.96.0.2 ct direction original tcp dport 8000 ct state new`,
		`input iifname "en0123456789ab" ip saddr 100.96.0.2 ip daddr 100.96.0.1 ct direction reply tcp sport 8000 ct state established ct mark 56789`,
		"output ip daddr @protected drop",
		`forward iifname "en0123456789ab" drop`,
	} {
		if !strings.Contains(online, expected) {
			t.Fatalf("missing filter rule %s", expected)
		}
	}
	offline, err := plan.transaction(id, false, linuxExisting, []netip.Addr{netip.MustParseAddr("100.96.0.3")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(offline, "ct state new") || strings.Contains(offline, "ct mark set") ||
		!strings.Contains(offline, "100.96.0.3") || !strings.Contains(offline, "ct state established ct mark 56789") {
		t.Fatal("offline filter minted new state or lost retained protection")
	}
}

func TestLinuxFilterNFTTransaction(t *testing.T) {
	if testing.Short() || os.Geteuid() != 0 {
		t.Skip("requires root and nftables in an isolated Linux network namespace")
	}
	id, plan := linuxPolicyFixture()
	deny, err := plan.transaction(id, true, linuxDeny, nil)
	if err != nil {
		t.Fatal(err)
	}
	online, err := plan.transaction(id, false, linuxOnline, nil)
	if err != nil {
		t.Fatal(err)
	}
	offline, err := plan.transaction(id, false, linuxExisting, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "unshare", "--net", "--", "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(deny + online + offline + "delete table inet " + id.table + "\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft transaction: %v\n%s", err, output)
	}
}

// Exercise the kernel output hook on each route. Dummy devices allow both
// permitted datagrams and nftables rejections to be observed without a remote.
func TestLinuxPerPeerPathKeepsSignedPortGrants(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	ctx := t.Context()
	id, plan := linuxPolicyFixture()
	prefix, group := linuxPeerScope(id.owner)
	direct := prefix + "00001"
	spare := prefix + "00002"
	for _, name := range []string{id.tunnel, direct, spare} {
		runLinuxTestCommand(t, ctx, linuxIP, "link", "add", name, "type", "dummy")
		if name != id.tunnel {
			runLinuxTestCommand(t, ctx, linuxIP, "link", "set", name, "group", strconv.FormatUint(uint64(group), 10))
		}
		runLinuxTestCommand(t, ctx, linuxIP, "link", "set", name, "up")
	}
	runLinuxTestCommand(t, ctx, linuxIP, "address", "add", plan.address.String()+"/32", "dev", id.tunnel)
	peer := plan.protected[1]
	other := netip.MustParseAddr("100.96.0.3")
	plan.protected = append(plan.protected, other)
	for _, p := range []netip.Addr{peer, other} {
		plan.grants = append(plan.grants, linuxFlowGrant{source: plan.address, destination: p, protocol: "udp", port: 8800, egress: true})
	}
	apply := func(install bool, mode linuxFilterMode) {
		t.Helper()
		rules, err := plan.transaction(id, install, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := linuxNFTApply(ctx, rules); err != nil {
			t.Fatal(err)
		}
	}
	route := func(ip netip.Addr, name string) {
		runLinuxTestCommand(t, ctx, linuxIP, "route", "replace", ip.String()+"/32", "dev", name, "src", plan.address.String())
	}
	send := func(ip netip.Addr, port int, allowed bool) {
		t.Helper()
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip.AsSlice(), Port: port})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, err = conn.Write([]byte{1})
		if (allowed && err != nil) || (!allowed && !errors.Is(err, syscall.EPERM)) {
			t.Fatalf("send to %s:%d, expected allowed=%t: %v", ip, port, allowed, err)
		}
	}
	apply(true, linuxOnline)
	route(peer, id.tunnel)
	route(other, id.tunnel)
	send(peer, 8800, true)
	plan.direct = map[netip.Addr]string{peer: direct}
	apply(false, linuxOnline)
	send(peer, 8800, false)
	route(peer, direct)
	send(peer, 8800, true)
	send(peer, 8801, false)
	send(other, 8800, true)
	route(other, direct)
	send(other, 8800, false)
	route(peer, spare)
	send(peer, 8800, false)
	route(peer, direct)
	apply(false, linuxExisting)
	send(peer, 8800, false)
	plan.direct = nil
	apply(false, linuxOnline)
	send(peer, 8800, false)
	route(peer, id.tunnel)
	send(peer, 8800, true)
}
