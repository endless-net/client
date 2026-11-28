package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// Explicit per-query server selection exercises the Windows DNS client without
// installing machine-wide DNS rules. Each fixture owns its loopback sockets.
func TestWindowsDNSClientQueriesSignedZone(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Windows DNS client")
	}
	f := newCandidateFixture(t)
	f.resolverDNS = true
	f.snapshot.Network.DNSConfig = &coordinator.DNSConfig{SearchDomains: []string{"zone.test"}, Zones: []coordinator.DNSZone{{Name: "zone.test.", Version: 1, Records: []coordinator.DNSRecord{{Name: "db.zone.test.", Type: "A", Value: "192.0.2.41", TTL: 19}}}}}
	zoneRecords := &f.snapshot.Network.DNSConfig.Zones[0].Records
	*zoneRecords = append(*zoneRecords, coordinator.DNSRecord{Name: "service.zone.test.", Type: "CNAME", Value: "db.zone.test.", TTL: 9})
	f.verify(t, nil)
	signedConfig := func() coordinator.NetworkMapSnapshot {
		m, err := f.verified.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	var active atomic.Bool
	active.Store(true)
	assignment, err := prepareWindowsDNS(t.Context(), signedConfig(), 2*time.Second, active.Load, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(assignment.search, []string{"zone.test"}) || !slices.Equal(assignment.namespaces, []string{".zone.test", "zone.test"}) {
		t.Fatal("incorrect namespace plan", assignment.namespaces)
	}
	proxy := assignment.proxy
	defer proxy.close()
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	lookup := func(name string, tcp bool) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
		defer cancel()
		script := fmt.Sprintf("$ErrorActionPreference='Stop'; $records=Resolve-DnsName -DnsOnly -NoHostsFile -QuickTimeout -Server %s -Name %s -Type A", proxy.addr.Addr(), name)
		if tcp {
			script += " -TcpOnly"
		}
		script += "; foreach($r in $records){ if ($r.IPAddress) { Write-Output ($r.IPAddress + '/' + $r.TTL) } }"
		output, err := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		return strings.TrimSpace(string(output)), err
	}
	for _, name := range []string{"db.zone.test.", "service.zone.test."} {
		for _, tcp := range []bool{false, true} {
			value, err := lookup(name, tcp)
			if err != nil || value != "192.0.2.41/19" {
				t.Fatalf("%s TCP=%v: %q, %v", name, tcp, value, err)
			}
		}
	}
	if out, err := lookup("absent.zone.test.", false); err == nil || !strings.Contains(out, "DNS_ERROR_RCODE_NAME_ERROR") {
		t.Fatalf("absent name: %q, %v", out, err)
	}
	active.Store(false)
	if out, err := lookup("blocked.zone.test.", false); err == nil || !strings.Contains(out, "RCODE_SERVER_FAILURE") {
		t.Fatalf("revoked authority: %q, %v", out, err)
	}
	cursor := f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Network.DNSConfig.Zones[0].Version++
	record := &f.snapshot.Network.DNSConfig.Zones[0].Records[0]
	record.Value = "192.0.2.42"
	record.TTL = 3
	f.snapshot.Network.DNSConfig.Zones[0].Records = append(f.snapshot.Network.DNSConfig.Zones[0].Records, coordinator.DNSRecord{
		Name: "keep.zone.test.", Type: "A", Value: "192.0.2.43", TTL: 17,
	})
	f.verify(t, &cursor)
	next, err := prepareWindowsDNS(t.Context(), signedConfig(), 2*time.Second, func() bool { return true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.proxy.close()
	if proxy.addr == next.proxy.addr {
		t.Fatal("replacement reused a live endpoint")
	}
	proxy = next.proxy
	if out, err := lookup("db.zone.test.", false); err != nil || out != "192.0.2.42/3" {
		t.Fatalf("replacement DNS answer: %q, %v", out, err)
	}
	expires := time.Now().Add(3 * time.Second)
	cursor = f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	zone := &f.snapshot.Network.DNSConfig.Zones[0]
	zone.Version++
	zone.Records = slices.Clone(zone.Records[1:])
	f.verify(t, &cursor)
	trimmed, err := prepareWindowsDNS(t.Context(), signedConfig(), 2*time.Second, func() bool { return true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer trimmed.proxy.close()
	proxy = trimmed.proxy
	time.Sleep(time.Until(expires))
	if out, err := lookup("db.zone.test.", false); err == nil || !strings.Contains(out, "DNS_ERROR_RCODE_NAME_ERROR") {
		t.Fatalf("deleted record: %q, %v", out, err)
	}
	if out, err := lookup("keep.zone.test.", false); err != nil || out != "192.0.2.43/17" {
		t.Fatalf("retained record: %q, %v", out, err)
	}
	cursor = f.verified.Cursor()
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Network.DNSConfig = nil
	f.verify(t, &cursor)
	withdrawn, err := prepareWindowsDNS(t.Context(), signedConfig(), 2*time.Second, active.Load, nil)
	if err != nil || withdrawn != nil {
		t.Fatalf("unexpected withdrawn assignment: %+v %v", withdrawn, err)
	}

}

func TestWindowsSessionAppliesSignedDNSMaps(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("policyConflict=%t", refuse), func(t *testing.T) { runWindowsSessionDNSMaps(t, refuse) })
	}
}

func runWindowsSessionDNSMaps(t *testing.T, refuse bool) {
	s, _, _, initial, _, _, _, _, clock := wiredSessionFixture(t)
	base := `Software\EndlessNetTest-` + uuid.NewString()
	root, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Error(err)
		}
		_ = root.Close()
		if err := registry.DeleteKey(registry.CURRENT_USER, base); err != nil {
			t.Error(err)
		}
	}()
	s.cleanup.dnsRules = windowsDNSRegistry{registry.CURRENT_USER, base}
	var search []string
	var policyConfirmed bool
	s.readSearch = func(guid windows.GUID) ([]string, error) { return slices.Clone(search), nil }
	s.writeSearch = func(guid windows.GUID, domains []string) error {
		expected, err := canonicalGUID(s.id)
		if err != nil || guid != expected {
			t.Fatal("wrong interface", guid)
		}
		if len(domains) > 0 && !policyConfirmed {
			t.Fatal("suffix installed before routing")
		}
		search = slices.Clone(domains)
		return nil
	}
	s.effectiveDNS = func(ctx context.Context, old, next *windowsDNSAssignment) error {
		if got := s.ConnectionLocal().DNS; got.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_PENDING {
			t.Fatal("missing pending observation", got)
		}
		keys, err := root.ReadSubKeyNames(-1)
		if err != nil {
			return err
		}
		var rules []effectiveDNSRule
		for _, key := range keys {
			k, err := registry.OpenKey(root, key, registry.READ)
			if err != nil {
				return err
			}
			names, _, err := k.GetStringsValue("Name")
			server, _, serverErr := k.GetStringValue("GenericDNSServers")
			_ = k.Close()
			if err != nil {
				return err
			}
			if serverErr != nil {
				return serverErr
			}
			rules = append(rules, effectiveDNSRule{Namespaces: names, Servers: []string{server}})
		}
		if len(search) > 0 {
			t.Fatal("obsolete suffix active during switch", search)
		}
		policyConfirmed = true
		if !matchesWindowsDNSPolicy(rules, old, next) {
			return ErrIPConflict
		}
		return nil
	}
	withZone := func(snapshot *coordinator.NetworkMapSnapshot) {
		snapshot.Network.DNSConfig = &coordinator.DNSConfig{SearchDomains: []string{"zone.test"}, Zones: []coordinator.DNSZone{{Name: "zone.test.", Version: 1, Records: []coordinator.DNSRecord{{Name: "db.zone.test.", Type: "A", Value: "192.0.2.41", TTL: 19}}}}}
	}
	m := nextSessionMap(t, s, initial, clock, withZone)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	check := func(want string) string {
		t.Helper()
		keys, err := root.ReadSubKeyNames(-1)
		if err != nil || len(keys) != 1 {
			t.Fatal(keys, err)
		}
		k, err := registry.OpenKey(root, keys[0], registry.READ)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = k.Close() }()
		server, _, err := k.GetStringValue("GenericDNSServers")
		if err != nil {
			t.Fatal(err)
		}
		resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		ips, err := resolver.LookupIP(ctx, "ip4", "db.zone.test.")
		if err != nil || len(ips) != 1 || ips[0].String() != want {
			t.Fatal(ips, err)
		}
		return server
	}
	if obs := s.ConnectionLocal().DNS; obs.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_APPLIED || obs.GetLastAppliedMapRevision() != m.Cursor().Revision.Network {
		t.Fatal(obs)
	}
	if !slices.Equal(search, []string{"zone.test"}) {
		t.Fatal(search)
	}
	policyConfirmed = false
	old := check("192.0.2.41")
	oldProxy := s.dns.proxy
	if err := s.Offline(t.Context()); err != nil {
		t.Fatal(err)
	}
	if oldProxy.active() {
		t.Fatal("offline lease authorized a DNS answer")
	}
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		zone := &snapshot.Network.DNSConfig.Zones[0]
		snapshot.Network.DNSConfig.SearchDomains = []string{"apps.zone.test", "zone.test"}
		zone.Version++
		zone.Records[0].Value = "192.0.2.42"
	})
	if refuse {
		observe := s.effectiveDNS
		s.effectiveDNS = func(ctx context.Context, old, next *windowsDNSAssignment) error {
			if next != nil {
				return ErrIPConflict
			}
			return observe(ctx, old, next)
		}
		if err := s.Refresh(t.Context(), next); !errors.Is(err, ErrIPConflict) {
			t.Fatal(err)
		}
		obs := s.ConnectionLocal().DNS
		if obs.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED || obs.GetFailure() != clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY || obs.GetLastAppliedMapRevision() != m.Cursor().Revision.Network || obs.GetTargetMapRevision() != next.Cursor().Revision.Network {
			t.Fatal(obs)
		}
		if oldProxy.active() || s.dnsUncertain == nil || s.dnsUncertain.proxy.active() {
			t.Fatal("unconfirmed switch retained authority")
		}
		if err := s.Stop(cleanupContext(t)); err != nil {
			t.Fatal(err)
		}
		if obs := s.ConnectionLocal().DNS; obs.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN {
			t.Fatal(obs)
		}
		return
	}
	if err := s.Refresh(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if check("192.0.2.42") == old || oldProxy.active() || oldProxy.ctx.Err() == nil {
		t.Fatal("reused old listener")
	}
	if !slices.Equal(search, []string{"apps.zone.test", "zone.test"}) {
		t.Fatal(search)
	}
	withdraw := nextSessionMap(t, s, next, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Network.DNSConfig = nil })
	if err := s.Refresh(t.Context(), withdraw); err != nil {
		t.Fatal(err)
	}
	if obs := s.ConnectionLocal().DNS; obs.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN || obs.LastAppliedMapRevision != nil {
		t.Fatal(obs)
	}
	keys, err := root.ReadSubKeyNames(-1)
	if err != nil || len(search) != 0 || len(keys) != 0 || s.dns != nil {
		t.Fatal("withdrawal", keys, err)
	}
}

func TestEffectiveWindowsDNSPolicy(t *testing.T) {
	if !testing.Short() {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if _, err := readWindowsDNSPolicy(ctx); err != nil {
			t.Fatal("read effective OS policy", err)
		}
	}
	makeAssignment := func(server string) *windowsDNSAssignment {
		return &windowsDNSAssignment{proxy: &sessionDNSProxy{addr: netip.MustParseAddrPort(server)}, namespaces: []string{".zone.test", "zone.test"}}
	}
	old, next := makeAssignment("127.17.0.1:53"), makeAssignment("127.17.0.2:53")
	rule := effectiveDNSRule{Namespaces: next.namespaces, Servers: []string{"127.17.0.2"}}
	if !matchesWindowsDNSPolicy([]effectiveDNSRule{rule}, old, next) {
		t.Fatal("expected complete new policy")
	}
	for _, r := range []effectiveDNSRule{
		{Namespaces: []string{"db.zone.test"}, Servers: []string{"9.9.9.9"}},
		{Namespaces: []string{"old.test"}, Servers: []string{"127.17.0.1"}},
		{Namespaces: []string{"outside.test"}, Servers: []string{"127.17.0.2"}},
		rule,
	} {
		if matchesWindowsDNSPolicy([]effectiveDNSRule{rule, r}, old, next) {
			t.Fatal("accepted conflicting policy", r)
		}
	}
	if matchesWindowsDNSPolicy(nil, old, next) || matchesWindowsDNSPolicy([]effectiveDNSRule{rule}, next, nil) {
		t.Fatal("accepted incomplete transition")
	}
}

func TestWindowsReadAdapterSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("reads IP Helper on the host")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		luid, err := winipcfg.LUIDFromIndex(uint32(iface.Index))
		if err != nil {
			t.Fatal(err)
		}
		guid, err := luid.GUID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowsSearch(*guid); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Skip("requires an active non-loopback interface")
}

func TestWindowsSearchRejectsAdministratorEdit(t *testing.T) {
	s, _, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	s.readSearch = func(windows.GUID) ([]string, error) { return []string{"administrator.test"}, nil }
	s.writeSearch = func(windows.GUID, []string) error { t.Fatal("overwrote administrator list"); return nil }
	if err := s.changeSearch(t.Context(), []string{"zone.test"}, nil); !errors.Is(err, ErrIPConflict) {
		t.Fatal(err)
	}
}
