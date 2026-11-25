package dataplane

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
)

func TestLinuxOwnedLinkAndFilterCleanup(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id, plan := linuxPolicyFixture()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(directory)
	receipt := state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}
	if err := ledger.Prepare(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	deny, err := plan.transaction(id, true, linuxDeny, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := linuxNFTApply(ctx, deny); err != nil {
		t.Fatal(err)
	}
	link, err := openLinuxLink(ctx, id.tunnel, 1420)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = link.device.Close() }()
	if err := link.configure(ctx, plan.address, plan.protected[1:]); err != nil {
		t.Fatal(err)
	}
	if err := CleanLinuxNetwork(ctx, ledger); err != ErrAdapterOwnership {
		t.Fatalf("retired filter before the link was closed: %v", err)
	}
	route, err := exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "exact", "100.96.0.2/32").Output()
	if err != nil || !strings.Contains(string(route), "dev "+id.tunnel) {
		t.Fatalf("owned /32 route absent: %v", err)
	}
	if err := link.device.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.InterfaceByName(id.tunnel); err == nil {
		t.Fatal("nonpersistent TUN survived close")
	}
	route, err = exec.CommandContext(ctx, linuxIP, "-4", "route", "show", "exact", "100.96.0.2/32").Output()
	if err != nil || len(route) != 0 {
		t.Fatalf("owned route survived link destruction: %v", err)
	}
	if err := CleanLinuxNetwork(ctx, ledger); err != nil {
		t.Fatal(err)
	}
	if handle, err := linuxNFTLookup(ctx, receipt); err != nil || handle != 0 {
		t.Fatalf("owned nft table survived cleanup: %v", err)
	}
	if _, err := ledger.Read(ctx); err != state.ErrMissing {
		t.Fatalf("effects receipt survived cleanup: %v", err)
	}
}

func TestLinuxCrashRecoveryPreservesForeignEffects(t *testing.T) {
	if !linuxNetworkTest(t) {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id, plan := linuxPolicyFixture()
	if ready := os.Getenv("EN_TEST_LINUX_CRASH_READY"); ready != "" {
		link, err := openLinuxLink(ctx, id.tunnel, 1420)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = link.device.Close() }()
		if err := link.configure(ctx, plan.address, plan.protected[1:]); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ready, nil, 0600); err != nil {
			t.Fatal(err)
		}
		<-ctx.Done()
		return
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(dir)
	receipt := state.LinuxNetworkReceipt{Table: id.table, Owner: id.owner}
	if err := ledger.Prepare(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	guards, err := plan.transaction(id, true, linuxOnline, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := linuxNFTApply(ctx, guards+"create table inet unrelated\n"); err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "link", "add", "foreign", "type", "dummy")
	runLinuxTestCommand(t, ctx, linuxIP, "address", "add", "192.0.2.5/24", "dev", "foreign")
	runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "foreign", "up")
	peer := plan.protected[1].String() + "/32"
	runLinuxTestCommand(t, ctx, linuxIP, "route", "add", peer, "dev", "foreign")
	conflicting, err := openLinuxLink(ctx, id.tunnel, 1420)
	if err != nil {
		t.Fatal(err)
	}
	configureErr := conflicting.configure(ctx, plan.address, plan.protected[1:])
	if err := conflicting.device.Close(); err != nil {
		t.Fatal(err)
	}
	if configureErr != ErrIPConflict {
		t.Fatalf("foreign route collision accepted: %v", configureErr)
	}
	runLinuxTestCommand(t, ctx, linuxIP, "route", "del", peer, "dev", "foreign")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "link-ready")
	child := exec.CommandContext(ctx, executable, "-test.run=^TestLinuxCrashRecoveryPreservesForeignEffects$")
	child.Env = append(os.Environ(), "EN_TEST_LINUX_CRASH_READY="+ready)
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if !waitLinuxTestFile(ctx, ready) {
		t.Fatal("child did not acquire the TUN")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("child should have died without cleanup")
	}
	runLinuxTestCommand(t, ctx, linuxIP, "route", "add", peer, "dev", "foreign")
	addr := net.JoinHostPort(plan.protected[1].String(), "4100")
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := connection.Write([]byte("fixture")); err == nil {
		t.Fatal("protected overlay destination could transmit on a foreign route before recovery")
	}
	if err := CleanLinuxNetwork(ctx, ledger); err != nil {
		t.Fatal(err)
	}
	runLinuxTestCommand(t, ctx, linuxNFT, "list", "table", "inet", "unrelated")
	if _, err := connection.Write([]byte("fixture")); err != nil {
		t.Fatalf("foreign route or traffic was changed during cleanup: %v", err)
	}
}

// Run native effect tests under a newly-created namespace; the parent host
// never receives the fixture's routes or firewall changes.
func linuxNetworkTest(t *testing.T) bool {
	t.Helper()
	if testing.Short() || os.Geteuid() != 0 {
		t.Skip("requires root, iproute2 and nftables in an isolated Linux network namespace")
	}
	if os.Getenv("EN_TEST_LINUX_NETWORK_NAMESPACE") == t.Name() {
		return true
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "unshare", "--net", "--", executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$")
	cmd.Env = append(os.Environ(), "EN_TEST_LINUX_NETWORK_NAMESPACE="+t.Name())
	if result, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated native test: %v\n%s", err, result)
	}
	return false
}
