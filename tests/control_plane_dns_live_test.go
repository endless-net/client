package tests

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	wg "github.com/endless-net/client-api/clientapi/wireguard"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
	"golang.org/x/net/dns/dnsmessage"
)

// HC-017/HC-025: the operating-system resolver selects the running Client's
// DNS configuration, including a map received while connection intent is off.
func TestControlPlaneNativeSystemDNS(t *testing.T) {
	requireControlScenario(t)
	binary := os.Getenv("ENDLESSNET_PACKET_PROBE")
	if !filepath.IsAbs(binary) {
		t.Fatal("system DNS requires the packetprobe binary")
	}
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("native-system-dns", "198.18.97.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, join, "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool { return nativeDNSMapApplied(v, "", 0) })
	id := status.NodeID
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	setPeer := func(hostname, address string) {
		t.Helper()
		previous := status.MapRevision
		if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
			m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
			m.Peers = []api.Peer{{ID: "system-dns-peer", Hostname: hostname, PublicKey: public, AllowedIPs: []string{address + "/32"}}}
		}); err != nil {
			t.Fatal(err)
		}
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool { return nativeDNSMapApplied(v, id, previous) })
	}
	setPeer("system-peer-one", "198.18.97.20")
	assertSystemDNSAddress(t, binary, n.Interface, clientDNSListenerAddress(status), "system-peer-one.scenario.endlessnet", "198.18.97.20")
	assertSystemDNSNameAbsent(t, binary, clientDNSListenerAddress(status), "absent-one.scenario.endlessnet")

	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	previous := status.MapRevision
	if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
		m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
		m.Peers = []api.Peer{{ID: "system-dns-peer", Hostname: "system-peer-two", PublicKey: public, AllowedIPs: []string{"198.18.97.21/32"}}}
	}); err != nil {
		t.Fatal(err)
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, id, previous) && !v.UserDisconnected && v.DesiredState == ipc.DesiredConnected
	})
	assertSystemDNSAddress(t, binary, n.Interface, clientDNSListenerAddress(status), "system-peer-two.scenario.endlessnet", "198.18.97.21")
	assertSystemDNSNameAbsent(t, binary, clientDNSListenerAddress(status), "absent-two.scenario.endlessnet")
}

// HC-025: the running native agent applies DNS changes from signed maps.
// Queries target its DNS listener; this does not prove OS resolver selection.
func TestControlPlaneNativeDNSMapUpdates(t *testing.T) {
	requireControlScenario(t)
	s := testcontrol.New(t)
	network, join, err := s.AddNetwork("native-dns", "198.18.96.0/24")
	if err != nil {
		t.Fatal(err)
	}
	n := testclient.New(t, s)
	n.Enroll(s, network.Name, join, "--route-table", "auto")
	n.Start()
	defer n.Stop()
	status := n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, "", 0)
	})
	id := status.NodeID
	key, err := wg.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wg.PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []struct{ hostname, ipv4, ipv6 string }{
		{"live-peer", "198.18.96.20", "fd96::20"},
		{"live-peer", "198.18.96.21", "fd96::21"},
		{"renamed-peer", "198.18.96.21", "fd96::21"},
		{},
		{"live-peer", "198.18.96.20", "fd96::20"},
	} {
		if err := s.UpdateMap(id, func(m *api.NetworkMapSnapshot) {
			m.Network.IPv6CIDR = "fd96::/64"
			m.Node.AssignedIPv6 = "fd96::1"
			m.Network.DNSConfig = &api.DNSConfig{MagicDNSEnabled: true, Suffix: "scenario.endlessnet", OverrideLocalDNS: false}
			m.Peers = nil
			if phase.hostname != "" {
				m.Peers = []api.Peer{{ID: "live-dns-peer", Hostname: phase.hostname, PublicKey: public, AllowedIPs: []string{phase.ipv4 + "/32", phase.ipv6 + "/128"}}}
			}
		}); err != nil {
			t.Fatal(err)
		}
		previous := status.MapRevision
		status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
			return nativeDNSMapApplied(v, id, previous)
		})
		var diagnostic ipc.DiagnosticsResponse
		n.Service("diagnostics", &diagnostic)
		d := diagnostic.Diagnostics
		if d.Status.NodeID != id || d.Status.MapRevision != status.MapRevision || !d.Status.CachedMapValid ||
			d.DNSSummary == nil || !d.DNSSummary.ConfigPresent || !d.DNSSummary.MagicDNSEnabled ||
			d.DNSSummary.SearchDomain != "scenario.endlessnet" || d.DNSSummary.RecordCount != len(d.DNSSummary.Records) {
			t.Fatal("public DNS diagnostics did not describe the applied signed map")
		}
		peerRecords := 0
		for _, record := range d.DNSSummary.Records {
			if record.NodeID != "live-dns-peer" {
				continue
			}
			peerRecords++
			if record.Hostname != phase.hostname || record.IPv4 != phase.ipv4 || record.IPv6 != phase.ipv6 ||
				strings.TrimSuffix(record.FQDN, ".") != phase.hostname+".scenario.endlessnet" {
				t.Fatal("public DNS diagnostics retained an obsolete peer name or address")
			}
		}
		wantPeerRecords := 0
		if phase.hostname != "" {
			wantPeerRecords = 1
		}
		if peerRecords != wantPeerRecords {
			t.Fatal("public DNS diagnostics omitted, duplicated or retained a withdrawn peer")
		}
		listener := clientDNSListenerAddress(status)
		for _, transport := range []string{"udp", "tcp"} {
			for _, hostname := range []string{"live-peer", "renamed-peer"} {
				code, ipv4, ipv6 := dnsmessage.RCodeNameError, "", ""
				if hostname == phase.hostname {
					code, ipv4, ipv6 = dnsmessage.RCodeSuccess, phase.ipv4, phase.ipv6
				}
				name := hostname + ".scenario.endlessnet."
				assertDNSWire(t, transport, listener, name, code, ipv4)
				assertDNSWireType(t, transport, listener, name, dnsmessage.TypeAAAA, code, ipv6)
			}
		}
	}
	var disconnected ipc.DisconnectResponse
	n.Service("disconnect", &disconnected)
	n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return v.NodeID == id && v.UserDisconnected && v.DesiredState == ipc.DesiredDisconnected
	})
	for _, transport := range []string{"udp", "tcp"} {
		assertDNSListenerUnavailable(t, transport, clientDNSListenerAddress(status))
	}
	var connected ipc.ConnectResponse
	n.Service("connect", &connected)
	status = n.AwaitStatus(func(v ipc.StatusResponse) bool {
		return nativeDNSMapApplied(v, id, 0) && v.NodeCredentialPresent &&
			!v.UserDisconnected && v.DesiredState == ipc.DesiredConnected &&
			v.State != ipc.StateDegraded
	})
	listener := clientDNSListenerAddress(status)
	for _, transport := range []string{"udp", "tcp"} {
		assertDNSWire(t, transport, listener, "live-peer.scenario.endlessnet.", dnsmessage.RCodeSuccess, "198.18.96.20")
		assertDNSWireType(t, transport, listener, "live-peer.scenario.endlessnet.", dnsmessage.TypeAAAA, dnsmessage.RCodeSuccess, "fd96::20")
		assertDNSWire(t, transport, listener, "renamed-peer.scenario.endlessnet.", dnsmessage.RCodeNameError, "")
	}
}

func clientDNSListenerAddress(status ipc.StatusResponse) string {
	if runtime.GOOS == "linux" {
		return net.JoinHostPort(status.OverlayIP, "53")
	}
	return "127.0.0.1:53"
}

func assertDNSListenerUnavailable(t *testing.T, transport, address string) {
	t.Helper()
	conn, err := net.DialTimeout(transport, address, time.Second)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if transport == "tcp" {
		t.Fatal("agent DNS TCP listener remained available after disconnect")
	}
	qname, err := dnsmessage.NewName("live-peer.scenario.endlessnet.")
	if err != nil {
		t.Fatal(err)
	}
	query := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: 0x6143, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: qname, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
	}
	wire, err := query.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(wire); err != nil {
		return
	}
	var reply [512]byte
	if _, err := conn.Read(reply[:]); err == nil {
		t.Fatal("agent DNS UDP listener remained available after disconnect")
	}
}

func assertSystemDNSAddress(t *testing.T, binary, interfaceName, listener, name, expected string) {
	t.Helper()
	// Platform DNS managers can publish a completed configuration command
	// before their application-facing resolver view has converged.
	deadline := time.Now().Add(10 * time.Second)
	var output []byte
	var err error
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		output, err = systemResolverCommand(ctx, binary, name, expected).CombinedOutput()
		cancel()
		if err == nil || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-t.Context().Done():
			t.Fatal("system resolver convergence wait interrupted")
		}
	}
	if err != nil {
		probeExit := -1
		if exit, ok := err.(*exec.ExitError); ok {
			probeExit = exit.ExitCode()
		}
		probeOutcome := "other"
		if runtime.GOOS == "windows" {
			probeOutcome = "native-api"
		}
		if strings.TrimSpace(string(output)) == "application exchange unavailable" {
			probeOutcome = "unavailable"
		}
		directCtx, directCancel := context.WithTimeout(t.Context(), 3*time.Second)
		directOutput, directErr := packetProbeCommand(directCtx, "", binary, "--mode", "resolve", "--address", name, "--dns", listener).CombinedOutput()
		directOK := directErr == nil && strings.TrimSpace(string(directOutput)) == expected
		directCancel()
		if runtime.GOOS == "linux" {
			listenerHost, _, _ := net.SplitHostPort(listener)
			managerCtx, managerCancel := context.WithTimeout(t.Context(), 3*time.Second)
			managerOK := exec.CommandContext(managerCtx, "resolvectl", "query", "--", name).Run() == nil
			managerCancel()
			dnsCtx, dnsCancel := context.WithTimeout(t.Context(), 3*time.Second)
			dnsOutput, dnsErr := exec.CommandContext(dnsCtx, "resolvectl", "dns", interfaceName).CombinedOutput()
			linkDNSOK := dnsErr == nil && strings.Contains(string(dnsOutput), listenerHost)
			dnsCancel()
			domainCtx, domainCancel := context.WithTimeout(t.Context(), 3*time.Second)
			domainOutput, domainErr := exec.CommandContext(domainCtx, "resolvectl", "domain", interfaceName).CombinedOutput()
			linkDomainOK := domainErr == nil && strings.Contains(string(domainOutput), "~scenario.endlessnet")
			domainCancel()
			contents, _ := os.ReadFile("/etc/resolv.conf")
			link, _ := os.Readlink("/etc/resolv.conf")
			stub := strings.Contains(string(contents), "127.0.0.53") || strings.Contains(link, "stub-resolv.conf")
			t.Logf("system resolver diagnostic: probe_exit=%d probe_outcome=%s direct_listener_ok=%t resolvectl_query_ok=%t link_dns_ok=%t link_domain_ok=%t systemd_stub=%t", probeExit, probeOutcome, directOK, managerOK, linkDNSOK, linkDomainOK, stub)
		}
		if runtime.GOOS == "windows" {
			managerCtx, managerCancel := context.WithTimeout(t.Context(), 5*time.Second)
			quotedName := strings.ReplaceAll(name, "'", "''")
			resolveScript := "$r=Resolve-DnsName -Name '" + quotedName + "' -Type A -DnsOnly -NoHostsFile -ErrorAction Stop; if(-not ($r | Where-Object {$_.IPAddress})){exit 2}"
			managerOK := exec.CommandContext(managerCtx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", resolveScript).Run() == nil
			managerCancel()
			ruleCtx, ruleCancel := context.WithTimeout(t.Context(), 5*time.Second)
			ruleScript := "$r=Get-DnsClientNrptRule -ErrorAction SilentlyContinue | Where-Object {$_.DisplayName -like 'EndlessNet-*'}; if(-not $r){exit 2}"
			rulePresent := exec.CommandContext(ruleCtx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", ruleScript).Run() == nil
			ruleCancel()
			t.Logf("system resolver diagnostic: probe_exit=%d probe_outcome=%s direct_listener_ok=%t windows_dns_api_ok=%t nrpt_present=%t", probeExit, probeOutcome, directOK, managerOK, rulePresent)
		}
		t.Fatal("system resolver did not resolve the published Client DNS name")
	}
	if strings.TrimSpace(string(output)) != expected {
		t.Fatal("system resolver returned an address outside the published Client map")
	}
}

func systemResolverCommand(ctx context.Context, binary, name, expected string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return packetProbeCommand(ctx, "", binary, "--mode", "resolve", "--address", name)
	}
	quotedName := strings.ReplaceAll(name, "'", "''")
	quotedExpected := strings.ReplaceAll(expected, "'", "''")
	script := "$r=@(Resolve-DnsName -Name '" + quotedName + "' -Type A -DnsOnly -NoHostsFile -ErrorAction Stop | Where-Object {$_.IPAddress} | ForEach-Object {$_.IPAddress}); if($r.Count -ne 1 -or $r[0] -ne '" + quotedExpected + "'){exit 2}; [Console]::Out.Write($r[0])"
	return exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
}

func assertSystemDNSNameAbsent(t *testing.T, binary, listener, name string) {
	t.Helper()
	directNameNotFound := false
	for range 3 {
		directCtx, directCancel := context.WithTimeout(t.Context(), 3*time.Second)
		directOutput, directErr := packetProbeCommand(directCtx, "", binary, "--mode", "resolve", "--address", name, "--dns", listener).CombinedOutput()
		directCancel()
		directExit, directExitOK := directErr.(*exec.ExitError)
		if directExitOK && directExit.ExitCode() == 3 && strings.TrimSpace(string(directOutput)) == "DNS name not found" {
			directNameNotFound = true
			break
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-t.Context().Done():
			t.Fatal("Client DNS negative convergence wait interrupted")
		}
	}
	if !directNameNotFound {
		t.Fatal("Client DNS listener did not return the name-not-found outcome")
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		quotedName := strings.ReplaceAll(name, "'", "''")
		script := "try { $r=@(Resolve-DnsName -Name '" + quotedName + "' -Type A -DnsOnly -NoHostsFile -ErrorAction Stop | Where-Object {$_.IPAddress}); if($r.Count -eq 0){exit 0}; exit 2 } catch { if($_.FullyQualifiedErrorId -like 'DNS_ERROR_RCODE_NAME_ERROR*'){exit 0}; exit 2 }"
		if err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
			t.Fatal("Windows system resolver did not return the Client DNS name-not-found outcome")
		}
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output, err := packetProbeCommand(ctx, "", binary, "--mode", "resolve", "--address", name).CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 || strings.TrimSpace(string(output)) != "DNS name not found" {
		t.Fatal("system resolver did not return the Client DNS name-not-found outcome")
	}
}

func nativeDNSMapApplied(status ipc.StatusResponse, nodeID string, afterRevision uint64) bool {
	return (nodeID == "" || status.NodeID == nodeID) && status.NodeID != "" &&
		status.CachedMapValid && status.MapRevision > afterRevision &&
		status.Agent != nil && status.Agent.StatePresent &&
		status.Agent.SnapshotState == ipc.AgentSnapshotCurrent &&
		status.Agent.MapRevision == status.MapRevision &&
		status.WireGuard != nil && status.WireGuard.OK && status.State != ipc.StateDegraded
}
