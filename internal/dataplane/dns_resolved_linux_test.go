package dataplane

import (
	"context"
	"errors"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
)

func isolatedResolved(t *testing.T) *os.Process {
	t.Helper()
	if testing.Short() || os.Geteuid() != 0 {
		t.Skip("requires Linux namespaces, root and systemd-resolved")
	}
	if os.Getenv("EN_TEST_RESOLVED_NAMESPACE") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "unshare", "-mn", "--propagation", "private", binary, "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "EN_TEST_RESOLVED_NAMESPACE=1")
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("private resolver: %v\n%s", err, raw)
		}
		return nil
	}
	for _, kind := range []string{"net", "mnt"} {
		own, e1 := os.Readlink("/proc/self/ns/" + kind)
		init, e2 := os.Readlink("/proc/1/ns/" + kind)
		if e1 != nil || e2 != nil || own == init {
			t.Fatal("fixture needs an isolated " + kind + " namespace")
		}
	}
	if err := unix.Mount("tmpfs", "/run", "tmpfs", 0, ""); err != nil {
		t.Fatal(err)
	}
	// Make libc and Go resolver consumers in this mount namespace use the
	// private resolved instance as they would on a stub-configured host.
	if err := os.WriteFile("/run/test-resolv.conf", []byte("nameserver 127.0.0.53\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{"/run/dbus", "/run/systemd/resolve"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []*exec.Cmd{exec.Command("chown", "systemd-resolve:systemd-resolve", "/run/systemd/resolve"), exec.Command(linuxIP, "link", "set", "lo", "up")} {
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	var resolver *os.Process
	for _, cmd := range []*exec.Cmd{exec.Command("dbus-daemon", "--system", "--nofork", "--nopidfile"), exec.Command("/usr/lib/systemd/systemd-resolved")} {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		if cmd.Path == "/usr/lib/systemd/systemd-resolved" {
			resolver = cmd.Process
		}
		if strings.HasSuffix(cmd.Path, "dbus-daemon") {
			for i := 0; i < 100; i++ {
				if _, err := os.Stat("/run/dbus/system_bus_socket"); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	for i := 0; i < 100; i++ {
		if exec.Command("/usr/bin/busctl", "--system", "status", resolvedBus).Run() == nil {
			if err := unix.Mount("/run/test-resolv.conf", "/etc/resolv.conf", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			return resolver
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("private resolver unavailable")
	return nil
}

type dnsTestLog struct {
	mu    sync.Mutex
	names []string
}

func (l *dnsTestLog) received(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.names {
		if n == name {
			return true
		}
	}
	return false
}
func (l *dnsTestLog) serve(t *testing.T, address string, answer [4]byte) func() {
	t.Helper()
	c, err := net.ListenPacket("udp4", address+":53")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, src, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			l.mu.Lock()
			l.names = append(l.names, q.Name.String())
			l.mu.Unlock()
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: h.ID, Response: true, RecursionDesired: h.RecursionDesired, RecursionAvailable: true}, Questions: []dnsmessage.Question{q}}
			if q.Type == dnsmessage.TypeA {
				reply.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Class: q.Class, TTL: 1}, Body: &dnsmessage.AResource{A: answer}}}
			}
			wire, err := reply.Pack()
			if err == nil {
				c.WriteTo(wire, src)
			}
		}
	}()
	return func() { c.Close() }
}

func TestLinuxResolvedAssignedServersAndWithdrawal(t *testing.T) {
	t.Run("replace conflict", func(t *testing.T) { testLinuxResolved(t, false) })
	t.Run("withdraw conflict", func(t *testing.T) { testLinuxResolved(t, true) })
}
func testLinuxResolved(t *testing.T, withdrawConflict bool) {
	if isolatedResolved(t) == nil {
		return
	}
	ctx := t.Context()
	for _, args := range [][]string{
		{linuxIP, "link", "add", "en017other", "type", "dummy"},
		{linuxIP, "link", "set", "en017other", "up"},
		{"/usr/bin/resolvectl", "dns", "en017other", "192.0.2.99"},
		{"/usr/bin/resolvectl", "domain", "en017other", "~other.example"},
		{"/usr/bin/resolvectl", "default-route", "en017other", "no"},
	} {
		if err := exec.CommandContext(ctx, args[0], args[1:]...).Run(); err != nil {
			t.Fatal("unrelated link fixture", err)
		}
	}
	other, err := net.InterfaceByName("en017other")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := readResolvedLink(ctx, other.Index)
	if err != nil {
		t.Fatal(err)
	}
	var primary, private, nested dnsTestLog
	primary.serve(t, "127.0.0.11", [4]byte{192, 0, 2, 1})
	stop := private.serve(t, "127.0.0.12", [4]byte{192, 0, 2, 2})
	nested.serve(t, "127.0.0.13", [4]byte{192, 0, 2, 3})

	cfg := &coordinator.DNSConfig{OverrideLocalDNS: true, SearchDomains: []string{"corp.example"}, Nameservers: []coordinator.DNSNameserver{
		{ID: "default", Scope: "primary", Address: "127.0.0.11"},
		{ID: "corp", Scope: "split", Address: "127.0.0.12", SplitDomains: []string{"corp.example"}},
		{ID: "dev", Scope: "split", Address: "127.0.0.13", SplitDomains: []string{"dev.corp.example"}},
	}}
	fixture := newCandidateFixture(t)
	fixture.resolverDNS = true
	fixture.snapshot.Peers = nil
	fixture.snapshot.Network.DNSConfig = cfg
	fixture.verify(t, nil)
	session, budget := linuxTestSession(t, fixture)
	ctx = connectivity.WithOperationBudget(ctx, budget)
	if err := session.Start(ctx, fixture.verified); err != nil {
		t.Fatal("signed map install", err)
	}
	if err := session.CheckReady(ctx); err != nil {
		t.Fatal("initial readiness", err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	checkResult := func(st clientv0.DnsApplyState, failure clientv0.DnsApplyFailure, previous uint64) {
		t.Helper()
		r := session.ConnectionLocal().DNS
		if r == nil || r.State != st || r.Failure != failure || r.TargetMapRevision != fixture.snapshot.Revision.Network || r.AccountId != fixture.snapshot.Network.AccountID || r.NetworkId != fixture.snapshot.Network.ID || r.NodeId != fixture.snapshot.Node.ID || (r.LastAppliedMapRevision != nil) != (previous != 0) || r.GetLastAppliedMapRevision() != previous {
			t.Fatalf("DNS result %v, expected state %v failure %v previous %v", r, st, failure, previous)
		}
	}
	checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_APPLIED, 0, fixture.snapshot.Revision.Network)
	link := session.link
	apply := func(next *coordinator.DNSConfig) error {
		previous := fixture.verified.Cursor()
		fixture.snapshot.Revision.Network++
		fixture.snapshot.Network.Revision = fixture.snapshot.Revision.Network
		fixture.snapshot.Network.DNSConfig = next
		fixture.verify(t, &previous)
		epoch := session.BlockSource()
		return session.Refresh(WithSourceEpoch(ctx, epoch), fixture.verified)
	}
	query := func(name string) (string, error) {
		timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		b, err := exec.CommandContext(timeout, "/usr/bin/resolvectl", "query", "--cache=no", "-4", name).CombinedOutput()
		return string(b), err
	}
	for _, tc := range []struct{ name, answer string }{{"db.corp.example", "192.0.2.2"}, {"db.dev.corp.example", "192.0.2.3"}, {"notcorp.example", "192.0.2.1"}, {"short", "192.0.2.2"}} {
		if got, err := query(tc.name); err != nil || !strings.Contains(got, tc.answer) {
			t.Fatalf("%s expected %s: %v %s", tc.name, tc.answer, err, got)
		}
	}
	out, err := exec.CommandContext(ctx, "getent", "ahostsv4", "libc.corp.example").Output()
	if err != nil || !strings.Contains(string(out), "192.0.2.2") || !private.received("libc.corp.example.") {
		t.Fatal("libc resolver did not use private server", err, string(out))
	}
	if !private.received("short.corp.example.") {
		t.Fatal("search expansion not observed")
	}
	// Serve an assigned zone on this node even with a matching upstream route.
	cfg.Zones = []coordinator.DNSZone{{Name: "dev.corp.example.", Version: 1, Records: []coordinator.DNSRecord{{Name: "api.dev.corp.example.", Type: "A", Value: "192.0.2.41", TTL: 1}}}}
	if err := apply(cfg); err != nil {
		t.Fatal(err)
	}
	cached := func() (string, error) {
		b, err := exec.CommandContext(ctx, "/usr/bin/resolvectl", "query", "-4", "api.dev.corp.example").CombinedOutput()
		return string(b), err
	}
	if got, err := cached(); err != nil || !strings.Contains(got, "192.0.2.41") {
		t.Fatal("authoritative answer", err, got)
	}
	if _, err := query("missing.dev.corp.example"); err == nil {
		t.Fatal("absent local record resolved")
	}
	cfg.Zones[0].Version++
	cfg.Zones[0].Records[0].Value = "192.0.2.42"
	cfg.Zones[0].Records = append(cfg.Zones[0].Records, coordinator.DNSRecord{
		Name: "keep.dev.corp.example.", Type: "A", Value: "192.0.2.43", TTL: 1,
	})
	if err := apply(cfg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if got, err := cached(); err != nil || !strings.Contains(got, "192.0.2.42") {
		t.Fatal("record after TTL", err, got)
	}
	// The system resolver must continue to serve the remaining record locally.
	cfg.Zones[0].Version++
	cfg.Zones[0].Records = cfg.Zones[0].Records[1:]
	if err := apply(cfg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if got, err := cached(); err == nil {
		t.Fatal("deleted record resolved alongside retained record", got)
	}
	// Query the OS stub configured in this namespace to distinguish NXDOMAIN,
	// a type without data, and a temporary failure after the map update.
	for _, tc := range []struct {
		name string
		kind dnsmessage.Type
		code dnsmessage.RCode
	}{
		{"api.dev.corp.example.", dnsmessage.TypeA, dnsmessage.RCodeNameError},
		{"keep.dev.corp.example.", dnsmessage.TypeAAAA, dnsmessage.RCodeSuccess},
	} {
		q := dnsmessage.Question{Name: dnsmessage.MustNewName(tc.name), Type: tc.kind, Class: dnsmessage.ClassINET}
		msg := dnsmessage.Message{Header: dnsmessage.Header{ID: 1707, RecursionDesired: true}, Questions: []dnsmessage.Question{q}}
		packet, err := msg.Pack()
		if err != nil {
			t.Fatal(err)
		}
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "udp4", "127.0.0.53:53")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(packet); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		var answer dnsmessage.Message
		if err := answer.Unpack(buf[:n]); err != nil || !answer.Response || answer.ID != msg.ID || answer.RCode != tc.code || len(answer.Answers) != 0 || len(answer.Questions) != 1 || answer.Questions[0] != q {
			t.Fatalf("OS stub response for %v: %+v (%v)", q, answer, err)
		}
	}
	out, err = exec.CommandContext(ctx, "getent", "ahostsv4", "keep.dev.corp.example.").Output()
	if err != nil || !strings.Contains(string(out), "192.0.2.43") {
		t.Fatal("libc lookup of retained record", err, string(out))
	}
	// Deleting the last record keeps the zone authoritative until withdrawal.
	cfg.Zones[0].Version++
	cfg.Zones[0].Records = nil
	if err := apply(cfg); err != nil {
		t.Fatal("remove record from assigned zone", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if got, err := cached(); err == nil {
		t.Fatal("deleted record still resolved after TTL", got)
	}
	checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_APPLIED, 0, fixture.snapshot.Revision.Network)
	for _, log := range []*dnsTestLog{&primary, &private, &nested} {
		if log.received("api.dev.corp.example.") || log.received("missing.dev.corp.example.") || log.received("keep.dev.corp.example.") {
			t.Fatal("local zone escaped to a nameserver")
		}
	}
	cfg.Zones = nil
	if err := apply(cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := query("api.dev.corp.example"); err != nil || !strings.Contains(got, "192.0.2.3") || !nested.received("api.dev.corp.example.") {
		t.Fatal("withdrawn zone still answered", err, got)
	}
	// A live resolver socket does not authorize lookups while the source is blocked.
	epoch := session.BlockSource()
	if err := session.Offline(WithSourceEpoch(ctx, epoch)); err != nil {
		t.Fatal(err)
	}
	if _, err := query("offline.corp.example"); err == nil || private.received("offline.corp.example.") || primary.received("offline.corp.example.") {
		t.Fatal("unconfirmed assignment forwarded a question", err)
	}
	if err := apply(cfg); err != nil {
		t.Fatal("restore signed source", err)
	}
	// Replace the nested assignment through a new signed map on the same link.
	before := session.dns.proxy.addr
	cfg.Nameservers[2].Address = "127.0.0.11"
	if err := apply(cfg); err != nil {
		t.Fatal("replace signed assignment", err)
	}
	if link != session.link || before == session.dns.proxy.addr {
		t.Fatal("unexpected link or resolver socket after replacement")
	}
	if got, err := query("updated.dev.corp.example"); err != nil || !strings.Contains(got, "192.0.2.1") || !primary.received("updated.dev.corp.example.") || nested.received("updated.dev.corp.example.") {
		t.Fatal("updated route not applied", err, got)
	}
	stop()
	if _, err := query("down.corp.example"); err == nil {
		t.Fatal("private query unexpectedly succeeded")
	}
	if got, err := query("external.example"); err != nil || !strings.Contains(got, "192.0.2.1") {
		t.Fatal("external DNS after private failure", err, got)
	}
	if primary.received("down.corp.example.") || nested.received("down.corp.example.") {
		t.Fatal("private query escaped its zone")
	}
	if err := apply(nil); err != nil {
		t.Fatal("withdraw", err)
	}
	checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN, 0, 0)
	if current, err := readResolvedLink(ctx, link.index); err != nil || current != session.dns.previous {
		t.Fatal("link DNS after withdrawal", current, err)
	}
	if err := apply(cfg); err != nil {
		t.Fatal("reinstall", err)
	}
	if err := exec.Command("/usr/bin/resolvectl", "domain", "en017other", "~x.corp.example").Run(); err != nil {
		t.Fatal(err)
	}
	if err := checkResolvedDomains(ctx, link.index, session.dns.domains); !errors.Is(err, ErrIPConflict) {
		t.Fatal("competing private route accepted", err)
	}
	if err := exec.Command("/usr/bin/resolvectl", "domain", "en017other", "~other.example").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/usr/bin/resolvectl", "domain", link.name, "~admin.example").Run(); err != nil {
		t.Fatal(err)
	}
	previousSuccess := fixture.snapshot.Revision.Network
	if withdrawConflict {
		if err := apply(nil); !errors.Is(err, ErrIPConflict) {
			t.Fatal("external edit overwritten", err)
		}
		checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED, clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_CLEANUP, previousSuccess)
	} else {
		if err := apply(cfg); !errors.Is(err, ErrIPConflict) {
			t.Fatal("changed resolver accepted for new revision", err)
		}
		checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED, clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY, previousSuccess)
	}
	edited, err := readResolvedLink(ctx, link.index)
	if err != nil || !strings.Contains(edited.domains, "admin.example") {
		t.Fatal("admin domain not preserved", err)
	}
	if err := session.Stop(ctx); err != nil {
		t.Fatal("disconnect", err)
	}
	checkResult(clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN, 0, 0)
	if _, err := net.InterfaceByName(link.name); err == nil {
		t.Fatal("owned interface survived disconnect")
	}
	if after, err := readResolvedLink(ctx, other.Index); err != nil || after != baseline {
		t.Fatal("unrelated interface changed", after, err)
	}

}
func TestDNSUDPReplyUsesAdvertisedSize(t *testing.T) {
	q := dnsmessage.Question{Name: dnsmessage.MustNewName("large.corp.example."), Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET}
	question := dnsmessage.Message{Header: dnsmessage.Header{ID: 99}, Questions: []dnsmessage.Question{q}}
	answer := dnsmessage.Message{Header: dnsmessage.Header{ID: 99, Response: true}, Questions: question.Questions, Answers: []dnsmessage.Resource{
		{Header: dnsmessage.ResourceHeader{Name: q.Name, Class: q.Class}, Body: &dnsmessage.TXTResource{TXT: []string{strings.Repeat("a", 240), strings.Repeat("b", 240), strings.Repeat("c", 240)}}},
	}}
	wire, err := answer.Pack()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		limit     int
		truncated bool
	}{{512, true}, {1232, false}} {
		if tc.limit > 512 {
			question.Additionals = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("."), Class: dnsmessage.Class(tc.limit)}, Body: &dnsmessage.OPTResource{}}}
		}
		raw, err := question.Pack()
		if err != nil {
			t.Fatal(err)
		}
		reply := dnsUDPReply(raw, wire)
		var got dnsmessage.Message
		if err := got.Unpack(reply); err != nil || got.ID != 99 || len(got.Questions) != 1 || got.Truncated != tc.truncated || len(reply) > tc.limit || (len(got.Answers) == 0) != tc.truncated {
			t.Fatal("wrong stub UDP response", tc, err)
		}
	}
}

func TestResolvedStubRejectsAlternateNameservers(t *testing.T) {
	for conf, want := range map[string]bool{
		"search corp.example\nnameserver 127.0.0.53 # stub\n": true,
		"nameserver 127.0.0.53\nnameserver 192.0.2.1\n":       false,
		"# nameserver 127.0.0.53\n":                           false,
		"# empty\nnameserver":                                 false,
	} {
		if got := usesResolvedStub([]byte(conf)); got != want {
			t.Errorf("got %v, want %v for %q", got, want, conf)
		}
	}
}

func TestLinuxDNSRefreshRechecksGeneration(t *testing.T) {
	resolver := isolatedResolved(t)
	if resolver == nil {
		return
	}
	f := newCandidateFixture(t)
	f.resolverDNS = true
	f.snapshot.Peers = nil
	cfg := &coordinator.DNSConfig{OverrideLocalDNS: true, Nameservers: []coordinator.DNSNameserver{{ID: "primary", Address: "127.0.0.11", Scope: "primary"}}}
	f.snapshot.Network.DNSConfig = cfg
	f.verify(t, nil)
	s, budget := linuxTestSession(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = connectivity.WithOperationBudget(ctx, budget)
	if err := s.Start(ctx, f.verified); err != nil {
		t.Fatal(err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	before := f.verified.Cursor()
	socket := s.dns.proxy.addr
	if err := s.Refresh(ctx, f.verified); !errors.Is(err, connectivity.ErrReplay) || s.dns.proxy.addr != socket {
		t.Fatal("replay replaced DNS", err)
	}
	cfg.Nameservers[0].Address = "127.0.0.12"
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.verify(t, &before)
	if err := resolver.Signal(unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer resolver.Signal(unix.SIGCONT)
	done := make(chan error, 1)
	go func() { done <- s.Refresh(ctx, f.verified) }()
	for {
		r := s.ConnectionLocal().DNS
		if r.GetState() == clientv0.DnsApplyState_DNS_APPLY_STATE_PENDING && r.GetTargetMapRevision() == f.snapshot.Revision.Network && r.GetLastAppliedMapRevision() == before.Revision.Network {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("refresh finished before resolver released: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	view, err := s.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = s.store.Update(ctx, view.Revision, func(next *state.State) error {
		profile := next.Profiles[s.profile]
		_, err := profile.Connection.AcceptIntent(state.IntentDisconnected, time.Now().UTC())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.Signal(unix.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, state.ErrStale) {
		t.Fatal("superseded generation acknowledged DNS", err)
	}
	result := s.ConnectionLocal().DNS
	if result.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED || result.GetLastAppliedMapRevision() != before.Revision.Network || s.lease.Permissions().NewFlows {
		t.Fatal("stale completion advertised success", result)
	}
}
