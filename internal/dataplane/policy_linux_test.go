package dataplane

import (
	"context"
	"net/netip"
	"os"
	"os/exec"
	"strings"
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
