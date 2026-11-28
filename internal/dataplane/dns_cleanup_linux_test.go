package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"golang.org/x/sys/unix"
)

func TestLinuxDNSPartialApplyAndCleanupRetry(t *testing.T) {
	resolver := isolatedResolved(t)
	if resolver == nil {
		return
	}
	ctx := t.Context()
	f := crashDNSFixture(t, time.Now().UTC())
	f.verify(t, nil)
	s, budget := linuxTestSession(t, f)
	op := connectivity.WithOperationBudget(ctx, budget)
	if err := s.Start(op, f.verified); err != nil {
		t.Fatal(err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	old := s.dns.proxy.addr
	before := f.verified.Cursor()
	oldState := s.dns.installed

	// In the private mount namespace let the DNS server write reach resolved,
	// then fail the domain write. Leave the external daemon and link live.
	const real = "/run/real-resolvectl"
	const injected = "/run/test-resolvectl"
	if err := os.WriteFile(real, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("/usr/bin/resolvectl", real, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(injected, []byte("#!/bin/sh\nif [ \"$1\" = domain ]; then exit 1; fi\nexec /run/real-resolvectl \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(injected, "/usr/bin/resolvectl", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Network.DNSConfig.Nameservers[0].SplitDomains = []string{"replacement.example"}
	f.verify(t, &before)
	if err := s.Refresh(op, f.verified); err == nil {
		t.Fatal("partial DNS update reported success")
	}
	reported := s.ConnectionLocal().DNS
	if reported.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED || reported.GetFailure() != clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY || reported.GetTargetMapRevision() != f.snapshot.Revision.Network || reported.GetLastAppliedMapRevision() != before.Revision.Network {
		t.Fatal("wrong receipt for partial update", reported)
	}
	if !s.dns.uncertain || s.dns.retiring == nil || s.lease.Permissions().NewFlows {
		t.Fatal("partial update still authorized")
	}
	next := s.dns.proxy.addr
	actual, err := readResolvedLink(ctx, s.link.index)
	if err != nil || old == next || actual.servers != resolverState(next, nil).servers || actual.domains != oldState.domains {
		t.Fatal("did not exercise partial OS update", actual, err)
	}
	if err := resolver.Signal(unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer resolver.Signal(unix.SIGCONT)
	timeout, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	err = s.Stop(timeout)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || s.ConnectionLocal().DNS.GetFailure() != clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_CLEANUP {
		t.Fatal("unconfirmed resolver cleanup acknowledged", err)
	}
	for _, endpoint := range []netip.AddrPort{old, next} {
		l, err := net.Listen("tcp4", endpoint.String())
		if err == nil {
			l.Close()
			t.Fatal("port reusable before resolver confirmed removal", endpoint)
		}
		if !errors.Is(err, unix.EADDRINUSE) {
			t.Fatal(err)
		}
	}
	if err := resolver.Signal(unix.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal("cleanup retry", err)
	}
	if r := s.ConnectionLocal().DNS; r.GetState() != clientv0.DnsApplyState_DNS_APPLY_STATE_WITHDRAWN || r.LastAppliedMapRevision != nil {
		t.Fatal("cleanup not confirmed", r)
	}
	for _, endpoint := range []netip.AddrPort{old, next} {
		l, err := net.Listen("tcp4", endpoint.String())
		if err != nil {
			t.Fatal("port survived confirmed cleanup", err)
		}
		l.Close()
	}
}
