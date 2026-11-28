package dataplane

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/endless-net/client/internal/underlay"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func linuxPolicyFixture() (linuxFilterIdentity, linuxFilterPlan) {
	local := netip.MustParseAddr("100.96.0.1")
	remote := netip.MustParseAddr("100.96.0.2")
	return linuxFilterIdentity{table: "en0123456789ab", tunnel: "en0123456789ab", owner: "e1bbf427-b4d5-4e6e-b745-68e908a45ee3", zone: 1234, mark: 56789},
		linuxFilterPlan{address: local, protected: []netip.Addr{local, remote},
			grants: []linuxFlowGrant{
				{source: local, destination: netip.PrefixFrom(remote, 32), protocol: "tcp", port: 8000, egress: true},
				{source: remote, destination: netip.PrefixFrom(local, 32), protocol: "udp", port: 5300},
			},
		}
}

func TestLinuxFilterBoundsIndividualGrantsInBothDirections(t *testing.T) {
	id, _ := linuxPolicyFixture()
	local, remote := "100.96.0.1", "100.96.0.2"
	end := time.Unix(1800000000, 999999999)
	snapshot := coordinator.NetworkMapSnapshot{Node: coordinator.Node{AssignedIP: local}, Peers: []coordinator.Peer{{
		AllowedIPs: []string{remote + "/32"}, ACLRestricted: true,
		ACLGrants: []coordinator.ACLGrant{
			{DestinationCIDRs: []string{remote + "/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "tcp", Port: 443}}, ValidUntil: &end},
			{DestinationCIDRs: []string{remote + "/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: "udp", Port: 53}}},
		},
		IngressACLGrants: []coordinator.ACLGrant{{DestinationCIDRs: []string{local + "/32"}, AllowedPorts: []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}, ValidUntil: &end}},
	}}}
	plan, err := prepareLinuxFilter(snapshot, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	plan.deadlineAdvance = 2 * time.Second
	for _, mode := range []linuxFilterMode{linuxOnline, linuxExisting} {
		rules, err := plan.transaction(id, false, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		timed, independent := 0, 0
		for _, rule := range strings.Split(rules, "\n") {
			if !strings.Contains(rule, "_accept") {
				continue
			}
			bounded := strings.Contains(rule, " meta time < 1799999998 ")
			if strings.Contains(rule, "udp ") {
				if bounded {
					t.Fatal("DNS inherited the posture deadline", rule)
				}
				independent++
			} else {
				if !bounded {
					t.Fatal("missing deadline on timed packet path", rule)
				}
				timed++
			}
		}
		if mode == linuxOnline && (timed != 7 || independent != 3) || mode == linuxExisting && (timed != 4 || independent != 2) {
			t.Fatal("grant paths missing", mode, timed, independent)
		}
	}
}

func TestUnavailableSubnetGuardSurvivesOffline(t *testing.T) {
	id, p := linuxPolicyFixture()
	p.unavailable = []netip.Prefix{netip.MustParsePrefix("10.20.0.0/24")}
	for _, mode := range []linuxFilterMode{linuxOnline, linuxExisting, linuxDeny} {
		rules, err := p.transaction(id, false, mode, nil)
		if err != nil || !strings.Contains(rules, "output ip daddr 10.20.0.0/24 goto output_drop") || !strings.Contains(rules, "forward ip daddr 10.20.0.0/24 drop") {
			t.Fatalf("missing resource guard in mode %v: %s, %v", mode, rules, err)
		}
	}
}

func TestLinuxFlowSnapshotGeneration(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	id, p := linuxPolicyFixture()
	ctx := t.Context()
	apply := func(rules string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err = linuxNFTApply(ctx, rules); err != nil {
			t.Fatal(err)
		}
	}
	apply(p.transaction(id, true, linuxOnline, nil))
	owner := state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}
	apply(linuxFlowRules(id, true, 4, time.Minute))
	first, err := readLinuxFlowSnapshot(ctx, owner, 65536)
	if err != nil || first.Generation == 0 || len(first.Details) != 0 || len(first.Totals) != 4 {
		t.Fatal("first snapshot", first, err)
	}
	duplicate, err := linuxFlowRules(id, true, 4, time.Minute)
	if err != nil || linuxNFTApply(ctx, duplicate) == nil {
		t.Fatal("second arm must not reuse live counters", err)
	}
	foreign := owner
	foreign.Owner = "different"
	if _, err := readLinuxFlowSnapshot(ctx, foreign, 65536); err != ErrPolicyOwnership {
		t.Fatal("foreign observer", err)
	}
	if _, err := readLinuxFlowSnapshot(ctx, owner, 64); err == nil {
		t.Fatal("truncated snapshot accepted")
	}
	apply(linuxFlowRules(id, false, 0, 0))
	apply(linuxFlowRules(id, false, 0, 0))
	if _, err := readLinuxFlowSnapshot(ctx, owner, 65536); err == nil {
		t.Fatal("disabled observer accepted")
	}
	apply(linuxFlowRules(id, true, 4, time.Minute))
	second, err := readLinuxFlowSnapshot(ctx, owner, 65536)
	if err != nil || second.Generation == first.Generation || second.TableHandle != first.TableHandle {
		t.Fatal("rearmed observation generation", second, err)
	}
	if _, err := sealLinuxFlowSnapshot(ctx, owner, first, 65536); err == nil {
		t.Fatal("stale baseline sealed a newer observation")
	}
	for _, args := range [][]string{{"link", "set", "lo", "name", id.tunnel}, {"link", "set", id.tunnel, "up"}} {
		if out, err := exec.CommandContext(ctx, linuxIP, args...).CombinedOutput(); err != nil {
			t.Fatalf("configure isolated loopback: %v: %s", err, out)
		}
	}
	apply("insert rule inet "+id.table+" output goto output_accept\ninsert rule inet "+id.table+" input goto input_accept\n", nil)
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	sender, err := net.DialUDP("udp4", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	exchange := func() {
		t.Helper()
		if _, err := sender.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var packet [1]byte
		if _, _, err := listener.ReadFromUDP(packet[:]); err != nil {
			t.Fatal(err)
		}
	}
	exchange()
	traffic, stopTraffic := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		for traffic.Err() == nil {
			if _, err := sender.Write([]byte{1}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	defer func() {
		stopTraffic()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	sealed, err := sealLinuxFlowSnapshot(ctx, owner, second, 65536)
	if err != nil || len(sealed.Details) != 2 {
		t.Fatalf("sealed counters: %+v, %v", sealed, err)
	}
	exchange()
	final, err := readLinuxCounters(ctx, owner, 65536, second.Generation)
	if err != nil || !maps.Equal(sealed.Details, final.Details) || !maps.Equal(sealed.Totals, final.Totals) {
		t.Fatalf("sealed counters changed: %+v, %v", final, err)
	}
	if _, err := readLinuxFlowSnapshot(ctx, owner, 65536); err == nil {
		t.Fatal("sealed gate accepted as live")
	}
	apply(linuxFlowRules(id, false, 0, 0))

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
		"output ip daddr @protected goto output_drop",
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
	enable, err := linuxFlowRules(id, true, 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	disable, err := linuxFlowRules(id, false, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = strings.NewReader(deny + online + enable + disable + offline + "delete table inet " + id.table + "\n")
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
		plan.grants = append(plan.grants, linuxFlowGrant{source: plan.address, destination: netip.PrefixFrom(p, 32), protocol: "udp", port: 8800, egress: true})
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
		defer func() { _ = conn.Close() }()
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

func TestLinuxResourcePortDirectionExpiryAndPhysicalFallback(t *testing.T) {
	id, _ := linuxPolicyFixture()
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	deadline := now.Add(30 * time.Second)
	endpoint := deadline.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: deadline}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID = "source"
	m.Peers[0].ID = "router"
	m.Peers[0].EndpointExpiresAt = &endpoint
	host := coordinator.ServiceHost{NodeID: "router", PublicKey: m.Peers[0].PublicKey}
	const subnet = "10.71.0.0/24"
	m.Network.Applications = []coordinator.Application{{ID: "db", TargetType: "cidr", Target: subnet, Connectors: []coordinator.ServiceHost{host}, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Routes: []coordinator.ApplicationRoute{{Connector: host, Metric: 10, CIDRs: []string{subnet}, ExpiresAt: endpoint}}}}
	plan, err := prepareLinuxFilter(m, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareLinuxFilter(m, 4, 2); err == nil {
		t.Fatal("resource guard exceeded the total native scope limit")
	}
	extra := m.Network.Applications[0]
	extra.ID = "ssh"
	extra.AllowedPorts = []coordinator.ServicePort{{Protocol: "tcp", Port: 22}}
	m.Network.Applications = append(m.Network.Applications, extra)
	if _, err := prepareLinuxFilter(m, 1, 4); err == nil {
		t.Fatal("two resource rules exceeded the grant limit")
	}
	m.Network.Applications = m.Network.Applications[:1]
	plan.deadlineAdvance = time.Second
	for _, mode := range []linuxFilterMode{linuxDeny, linuxExisting, linuxOnline} {
		rules, err := plan.transaction(id, false, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		drop := strings.Index(rules, "output ip daddr "+subnet+" goto output_drop")
		if drop < 0 || !strings.Contains(rules, "forward ip daddr "+subnet+" drop") {
			t.Fatal("missing protected destination", mode)
		}
		before := rules[:drop]
		outbound := "output oifname \"" + id.tunnel + "\" ip saddr 100.64.0.1 ip daddr " + subnet + " meta time < 1800000029 ct direction original tcp dport 443"
		reply := "input iifname \"" + id.tunnel + "\" ip saddr " + subnet + " ip daddr 100.64.0.1 ct direction reply meta time < 1800000029 tcp sport 443"
		if strings.Contains(before, outbound+" ct state established") != (mode != linuxDeny) ||
			strings.Contains(before, reply+" ct state established") != (mode != linuxDeny) ||
			strings.Contains(before, outbound+" ct state new tcp flags & (syn | ack) == syn") != (mode == linuxOnline) {
			t.Fatal("incorrect flow permission", mode, before)
		}
		if strings.Contains(before, "tcp sport 443 ct state new") || strings.Contains(before, "tcp dport 22") || strings.Contains(before, "input iifname \""+id.tunnel+"\" ip saddr "+subnet+" ip daddr 100.64.0.1 meta time < 1800000029 ct direction original") {
			t.Fatal("reverse or unrelated port opened")
		}
	}
	m.Network.Applications[0].Routes = nil
	m.Network.Applications[0].Connectors = nil
	unavailable, err := prepareLinuxFilter(m, 4, 4)
	if err != nil || len(unavailable.unavailable) != 1 || len(unavailable.grants) != 0 {
		t.Fatal("withdrawal did not replace permission with block", err)
	}
}

func TestLinuxNestedResourceKeepsItsOwnPortPolicy(t *testing.T) {
	id, p := linuxPolicyFixture()
	broad, narrow := netip.MustParsePrefix("10.71.0.0/16"), netip.MustParsePrefix("10.71.2.0/24")
	p.activeResources = []netip.Prefix{broad, narrow}
	for i, prefix := range p.activeResources {
		port := []int{22, 443}[i]
		p.grants = append(p.grants, linuxFlowGrant{source: p.address, destination: prefix, egress: true, protocol: "tcp", port: port})
	}
	for _, withdraw := range []bool{false, true} {
		if withdraw {
			p.unavailable = []netip.Prefix{narrow}
		}
		rules, err := p.transaction(id, false, linuxOnline, nil)
		if err != nil {
			t.Fatal(err)
		}
		block := strings.Index(rules, "output ip daddr 10.71.2.0/24 goto output_drop")
		replyBlock := strings.Index(rules, "ip saddr 10.71.2.0/24 goto input_drop")
		wide := strings.Index(rules, "ip daddr 10.71.0.0/16 ct direction original tcp dport 22")
		wideReply := strings.Index(rules, "ip saddr 10.71.0.0/16 ip daddr "+p.address.String()+" ct direction reply tcp sport 22")
		if block < 0 || replyBlock < 0 || wide <= block || wideReply <= replyBlock {
			t.Fatal("broader route could bypass a more specific policy", rules)
		}
		forward := strings.Index(rules, "ip daddr 10.71.2.0/24 ct direction original tcp dport 443")
		if (!withdraw && (forward < 0 || forward >= block)) || (withdraw && forward != -1) {
			t.Fatal("more specific grant survived withdrawal or was shadowed")
		}
	}
}

func TestLinuxRouterTransitBindsSourceRouteAndMasquerade(t *testing.T) {
	id, _ := linuxPolicyFixture()
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	end := now.Add(30 * time.Second)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID = "router"
	m.Node.EndpointExpiresAt = &end
	m.Peers[0].ID = "source"
	m.Peers[0].EndpointExpiresAt = &end
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	source := coordinator.ServiceHost{NodeID: m.Peers[0].ID, PublicKey: m.Peers[0].PublicKey}
	const target = "10.47.0.0/16"
	app := coordinator.Application{ID: "lan", TargetType: "cidr", Target: target, Connectors: []coordinator.ServiceHost{local}, Sources: []coordinator.ServiceHost{source}, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 8443}}, Routes: []coordinator.ApplicationRoute{{Connector: local, CIDRs: []string{target}, Metric: 1, ExpiresAt: end}}}
	shadow := coordinator.Application{ID: "offline", TargetType: "ip", Target: "10.47.1.9", Connectors: []coordinator.ServiceHost{local}}
	m.Network.Applications = []coordinator.Application{app, shadow}
	for _, nat := range []bool{false, true} {
		app.Routes[0].Masquerade = nat
		p, err := prepareLinuxFilter(m, 4, 5)
		if err != nil || p.transitCount != 1 {
			t.Fatal(err, p.transitCount)
		}
		replaced, err := p.transaction(id, false, linuxDeny, nil)
		if err != nil || !strings.Contains(replaced, "flush chain inet "+id.table+" resource_nat") ||
			!strings.Contains(replaced, "flush chain inet "+id.table+" ingress_zone") ||
			strings.Contains(replaced, " masquerade\n") {
			t.Fatal("old transit rules survived replacement", err)
		}
		for _, mode := range []linuxFilterMode{linuxDeny, linuxExisting, linuxOnline} {
			rules, err := p.transaction(id, true, mode, nil)
			if err != nil {
				t.Fatal(err)
			}
			open := strings.Index(rules, "ip saddr 100.64.0.2 ip daddr "+target+" meta time < 1800000030 ct direction original tcp dport 8443 ct state new")
			block := strings.Index(rules, "forward iifname \""+id.tunnel+"\" ip daddr 10.47.1.9/32 drop")
			if block < 0 || (open >= 0) != (mode == linuxOnline) || open >= 0 && block >= open {
				t.Fatal("scoped forwarding", mode)
			}
			if strings.Contains(rules, " masquerade\n") != (nat && mode == linuxOnline) || strings.Contains(rules, "output ip daddr "+target) {
				t.Fatal("unexpected NAT or local LAN guard", mode)
			}
			reply := "ip saddr " + target + " ip daddr 100.64.0.2 meta time < 1800000030 ct direction reply tcp sport 8443 ct state established ct mark 56789 "
			if nat {
				reply += "ct status snat accept"
			} else {
				reply += "ct status & snat == 0 accept"
			}
			if strings.Contains(rules, reply) != (mode != linuxDeny) {
				t.Fatal("invalid reply policy", mode, reply)
			}
		}
	}
	m.Network.Applications[0].Sources[0].PublicKey = local.PublicKey
	if _, err := prepareLinuxFilter(m, 4, 5); err == nil {
		t.Fatal("wrong source key")
	}
	m.Network.Applications[0].Sources[0] = source
	short := end.Add(-time.Second)
	m.Peers[0].EndpointExpiresAt = &short
	if _, err := prepareLinuxFilter(m, 4, 5); err == nil {
		t.Fatal("source lease shorter than map")
	}
	m.Peers[0].EndpointExpiresAt = &end
	m.Network.Applications[0].Routes[0].ExpiresAt = short
	if _, err := prepareLinuxFilter(m, 4, 5); err == nil {
		t.Fatal("router lease shorter than map")
	}
}

func TestLinuxExitRecipientFiltersOnlyInternetAndRetainsSubnet(t *testing.T) {
	id, plan := linuxPolicyFixture()
	plan.overlay = netip.MustParsePrefix("100.96.0.0/24")
	lan := netip.MustParsePrefix("10.20.0.0/24")
	exit := netip.MustParsePrefix("0.0.0.0/0")
	plan.activeResources = []netip.Prefix{lan, exit}
	for _, dst := range plan.activeResources {
		plan.grants = append(plan.grants, linuxFlowGrant{source: plan.address, destination: dst, protocol: "tcp", port: 443, egress: true})
	}
	for _, mode := range []linuxFilterMode{linuxOnline, linuxExisting, linuxDeny} {
		rules, err := plan.transaction(id, true, mode, nil)
		if err != nil {
			t.Fatal(err)
		}
		lanDrop := strings.Index(rules, "output ip daddr 10.20.0.0/24 goto output_drop")
		exitDrop := strings.Index(rules, "output ip daddr 0.0.0.0/0")
		if lanDrop < 0 || exitDrop < lanDrop || strings.Contains(rules, " forward ip daddr 0.0.0.0/0") {
			t.Fatal("exit overrides subnet or foreign forwarded traffic")
		}
		markGuard := fmt.Sprintf("meta mark != %#x goto output_drop", underlay.Mark)
		mark := strings.Index(rules, markGuard)
		if strings.Count(rules, markGuard) != 1 || mark < exitDrop || strings.Index(rules, "output ip saddr @protected") < mark {
			t.Fatal("underlay exception escaped the exit guard", rules)
		}
		for _, line := range strings.Split(rules, "\n") {
			if !strings.Contains(line, exit.String()) {
				continue
			}
			field := "daddr"
			if strings.Contains(line, "ip saddr 0.0.0.0/0") {
				field = "saddr"
			}
			for _, excluded := range append([]netip.Prefix{plan.overlay}, exitLocalNetworks[:]...) {
				if !strings.Contains(line, "ip "+field+" != "+excluded.String()) {
					t.Fatalf("missing %s exclusion: %s", excluded, line)
				}
			}
		}
	}
	// During withdrawal the old prefix is guarded until its route is removed.
	plan.activeResources = []netip.Prefix{lan}
	plan.grants = plan.grants[:len(plan.grants)-1]
	plan.unavailable = []netip.Prefix{exit}
	rules, err := plan.transaction(id, false, linuxOnline, nil)
	if err != nil || !strings.Contains(rules, "output ip daddr 0.0.0.0/0 ip daddr !=") ||
		!strings.Contains(rules, "ip daddr 10.20.0.0/24 ct direction original tcp dport 443") {
		t.Fatal("withdrawing exit disturbed subnet", err)
	}
}

func TestLinuxRouterDiscoveryKeepsExistingPolicyWithoutGrantingTraffic(t *testing.T) {
	m, _ := engineMap(t)
	m.Node.ID, m.Network.CIDR = "connector", "100.64.0.0/24"
	m.Peers[0].ID = "peer"
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	id, _ := linuxPolicyFixture()
	rules := func() string {
		t.Helper()
		plan, err := prepareLinuxFilter(m, 8, 8)
		if err != nil {
			t.Fatal(err)
		}
		policy, err := plan.transaction(id, true, linuxOnline, nil)
		if err != nil {
			t.Fatal(err)
		}
		return policy
	}
	before := rules()
	m.Network.Applications = []coordinator.Application{{ID: "office", TargetType: "domain", Target: "printer.corp.example", PolicyHash: strings.Repeat("ab", 32), Connectors: []coordinator.ServiceHost{local}}}
	if after := rules(); before != after {
		t.Fatal("discovery changed native rules")
	}
	transit, sources, err := routerPacketPolicy(m)
	if err != nil || len(transit)+len(sources) != 0 {
		t.Fatal(transit, sources, err)
	}
	recipient, routers, err := recipientPacketRules(m)
	if err != nil || len(recipient)+len(routers) != 0 {
		t.Fatal(recipient, routers, err)
	}
}

func TestLinuxExitRouterKeepsSubnetGrantIndependent(t *testing.T) {
	id, _ := linuxPolicyFixture()
	m, _ := engineMap(t)
	now := time.Unix(1800000000, 0)
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID = "router"
	m.Node.EndpointExpiresAt = &end
	m.Peers[0].ID = "source"
	m.Peers[0].EndpointExpiresAt = &end
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	src := coordinator.ServiceHost{NodeID: m.Peers[0].ID, PublicKey: m.Peers[0].PublicKey}
	source := netip.MustParsePrefix(m.Peers[0].AllowedIPs[0]).Addr()
	app := func(name, cidr string, port uint32, nat bool) coordinator.Application {
		return coordinator.Application{ID: name, TargetType: "cidr", Target: cidr, Connectors: []coordinator.ServiceHost{local}, Sources: []coordinator.ServiceHost{src}, AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: port}}, Routes: []coordinator.ApplicationRoute{{Connector: local, CIDRs: []string{cidr}, ExpiresAt: end, Metric: 1, Masquerade: nat}}}
	}
	lan := app("lan", "10.20.0.0/24", 443, false)
	exit := app("exit", "0.0.0.0/0", 8443, true)
	for _, active := range []bool{true, false} {
		m.Network.Applications = []coordinator.Application{lan}
		if active {
			m.Network.Applications = append(m.Network.Applications, exit)
		}
		paths, err := mapResourcePaths(m)
		if err != nil || len(paths) != 0 {
			t.Fatal("router acquired recipient route", paths, err)
		}
		p, err := prepareLinuxFilter(m, 8, 8)
		if err != nil {
			t.Fatal(err)
		}
		rules, err := p.transaction(id, true, linuxOnline, nil)
		if err != nil || strings.Contains(rules, " masquerade\n") != active || !strings.Contains(rules, "tcp dport 443 ct state new") {
			t.Fatal("independent nft grants", err)
		}
		if active && (!strings.Contains(rules, "ip daddr != 10.0.0.0/8") || !strings.Contains(rules, "ip saddr != "+m.Network.CIDR)) {
			t.Fatal("exit missing local exclusions")
		}
		transit, _, err := routerPacketPolicy(m)
		if err != nil {
			t.Fatal(err)
		}
		lookup := func(dst string) resourcePacketRule { return transit.lookup(netip.MustParseAddr(dst), source) }
		if r := lookup("10.20.0.8"); r.peer != src.NodeID || !r.ports[coordinator.ACLPort{Protocol: "tcp", Port: 443}] || r.ports[coordinator.ACLPort{Protocol: "tcp", Port: 8443}] {
			t.Fatal("exit masked private resource")
		}
		if r := lookup("1.1.1.1"); (r.peer == src.NodeID && r.ports[coordinator.ACLPort{Protocol: "tcp", Port: 8443}]) != active {
			t.Fatal("wrong internet grant")
		}
		for _, dst := range []string{"10.20.1.8", "100.64.0.1", "127.0.0.1", "169.254.1.2", "224.0.0.1"} {
			if lookup(dst).peer != "" {
				t.Fatal("exit admitted local address", dst)
			}
		}
		if transit.lookup(netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("100.64.0.77")).peer != "" {
			t.Fatal("unassigned source admitted")
		}
	}
}
