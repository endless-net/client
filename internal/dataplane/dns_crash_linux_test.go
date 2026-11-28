package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type crashDNSReady struct {
	Link     string
	Index    int
	Endpoint string
}

func crashDNSFixture(t *testing.T, at time.Time) *candidateFixture {
	f := candidateFixtureAt(t, transport.AmneziaWG, at)
	f.resolverDNS = true
	f.snapshot.Peers = nil
	f.snapshot.Network.DNSConfig = &coordinator.DNSConfig{Nameservers: []coordinator.DNSNameserver{{ID: "private", Scope: "split", Address: "127.0.0.12", SplitDomains: []string{"corp.example"}}}}
	return f
}

// The child is SIGKILLed with a live TUN, resolver sockets and nft table.
// Only the protected store and effects ledger are shared with its successor.
func TestLinuxDNSAfterProcessCrash(t *testing.T) {
	if directory := os.Getenv("EN_TEST_DNS_CRASH_CHILD"); directory != "" {
		for _, ns := range []string{"net", "mnt"} {
			own, e1 := os.Readlink("/proc/self/ns/" + ns)
			init, e2 := os.Readlink("/proc/1/ns/" + ns)
			if e1 != nil || e2 != nil || own == init || os.Getenv("EN_TEST_RESOLVED_NAMESPACE") != "1" {
				t.Fatal("requires isolated resolver")
			}
		}
		stamp, err := strconv.ParseInt(os.Getenv("EN_TEST_DNS_CRASH_STAMP"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		store, err := state.OpenLinux(t.Context(), directory, false, 1<<20, 2<<20)
		if err != nil {
			t.Fatal(err)
		}
		view, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		f := crashDNSFixture(t, time.Unix(0, stamp).UTC())
		f.profile = view.Profiles[view.ActiveProfileID]
		key, err := ecdh.X25519().NewPrivateKey(f.profile.Keys.WireGuardPrivate)
		if err != nil {
			t.Fatal(err)
		}
		f.snapshot.Node.PublicKey = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
		f.verify(t, nil)
		s, budget := linuxTestSessionWithStore(t, f, store, directory)
		ctx := connectivity.WithOperationBudget(t.Context(), budget)
		if err := s.Start(ctx, f.verified); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckReady(ctx); err != nil {
			t.Fatal(err)
		}
		if err := budget.Complete(); err != nil {
			t.Fatal(err)
		}
		pipe := os.NewFile(3, "applied")
		if err := json.NewEncoder(pipe).Encode(crashDNSReady{s.link.name, s.link.index, s.dns.proxy.addr.String()}); err != nil {
			t.Fatal(err)
		}
		_ = pipe.Close()
		<-t.Context().Done()
		return
	}
	resolver := isolatedResolved(t)
	if resolver == nil {
		return
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	at := time.Now().UTC()
	f := crashDNSFixture(t, at)
	seed, _ := hintStoreFixture(t, f)
	initial, err := seed.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.OpenLinux(ctx, directory, true, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(ctx, 1, func(v *state.State) error {
		v.OwnerSID = initial.OwnerSID
		v.ActiveProfileID = initial.ActiveProfileID
		v.Profiles = initial.Profiles
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var original, replacement dnsTestLog
	original.serve(t, "127.0.0.12", [4]byte{192, 0, 2, 12})
	replacement.serve(t, "127.0.0.13", [4]byte{192, 0, 2, 13})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestLinuxDNSAfterProcessCrash$")
	cmd.Env = append(os.Environ(), "EN_TEST_DNS_CRASH_CHILD="+directory, "EN_TEST_DNS_CRASH_STAMP="+strconv.FormatInt(at.UnixNano(), 10))
	cmd.ExtraFiles = []*os.File{writer}
	log := new(bytes.Buffer)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	_ = writer.Close()
	var ready crashDNSReady
	if err := json.NewDecoder(reader).Decode(&ready); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child startup: %v %s", err, log)
	}
	query := func(name, answer string) {
		t.Helper()
		raw, err := exec.CommandContext(ctx, "/usr/bin/resolvectl", "query", "--cache=no", "-4", name).CombinedOutput()
		if err != nil || !strings.Contains(string(raw), answer) {
			t.Fatalf("resolver query %q: %v %s", name, err, raw)
		}
	}
	query("before.corp.example", "192.0.2.12")
	if err := resolver.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resolver.Signal(syscall.SIGCONT) }()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exitErr *exec.ExitError
	if !errors.As(cmd.Wait(), &exitErr) || exitErr.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatal("fixture did not crash from SIGKILL")
	}
	if _, err := net.InterfaceByName(ready.Link); err == nil {
		t.Fatal("dead owner's tunnel survived")
	}
	if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", ready.Endpoint); err == nil {
		_ = conn.Close()
		t.Fatal("dead owner's DNS endpoint accepts connections")
	}
	ledger := state.NewLinuxNetworkLedger(directory)
	receipt, err := ledger.Read(ctx)
	if err != nil || receipt.Table != ready.Link {
		t.Fatal("durable native receipt lost", err)
	}
	if handle, err := linuxNFTLookup(ctx, receipt); err != nil || handle == 0 {
		t.Fatal("missing crash residue", err)
	}
	if receipt.DNSIndex != ready.Index || receipt.DNSNewPort == 0 {
		t.Fatal("missing resolver receipt", receipt)
	}
	timeout, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	err = CleanLinuxNetwork(timeout, ledger)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("restart retired effects without checking resolved", err)
	}
	if retained, err := ledger.Read(ctx); err != nil || retained != receipt {
		t.Fatal("lost retryable cleanup receipt", err)
	}
	if err := resolver.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	// A different interface may acquire the same index between attempts.
	const foreign = "en017other"
	for _, args := range [][]string{
		{linuxIP, "link", "add", "name", foreign, "index", strconv.Itoa(ready.Index), "type", "dummy"},
		{linuxIP, "link", "set", foreign, "up"},
		{"/usr/bin/resolvectl", "domain", foreign, "~foreign.example"},
		{"/usr/bin/resolvectl", "dns", foreign, ready.Endpoint},
	} {
		if raw, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("foreign link fixture: %v %s", err, raw)
		}
	}
	if err := CleanLinuxNetwork(ctx, ledger); !errors.Is(err, ErrIPConflict) {
		t.Fatal("recorded port on reused interface silently retired", err)
	}
	if err := exec.CommandContext(ctx, "/usr/bin/resolvectl", "dns", foreign, "192.0.2.9").Run(); err != nil {
		t.Fatal(err)
	}
	otherDNS, err := readResolvedLink(ctx, ready.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err := CleanLinuxNetwork(ctx, ledger); err != nil {
		t.Fatal(err)
	}
	if handle, err := linuxNFTLookup(ctx, receipt); err != nil || handle != 0 {
		t.Fatal("orphan table after restart", err)
	}
	if _, err := ledger.Read(ctx); !errors.Is(err, state.ErrMissing) {
		t.Fatal("orphan receipt after cleanup", err)
	}
	if got, err := readResolvedLink(ctx, ready.Index); err != nil || got != otherDNS {
		t.Fatal("recovery changed foreign DNS", got, err)
	}
	reopened, err := state.OpenLinux(ctx, directory, false, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.profile = saved.Profiles[saved.ActiveProfileID]
	cursor := f.profile.Connection.MapCursor
	if cursor == nil || cursor.Revision != f.snapshot.Revision {
		t.Fatal("cursor lost on restart")
	}
	if err := f.verifyResult(t, cursor); !errors.Is(err, connectivity.ErrReplay) {
		t.Fatal("restored verifier accepted original signed DNS assignment", err)
	}
	session, budget := linuxTestSessionWithStore(t, f, reopened, directory)
	if session.ConnectionLocal().DNS != nil || session.dns.proxy != nil {
		t.Fatal("stored cursor claimed DNS was applied")
	}
	f.clock.advance(time.Second, time.Second)
	f.snapshot.Revision.Network++
	f.snapshot.Network.Revision = f.snapshot.Revision.Network
	f.snapshot.Network.DNSConfig.Nameservers[0].Address = "127.0.0.13"
	f.verify(t, cursor)
	ctx = connectivity.WithOperationBudget(ctx, budget)
	if err := session.Restore(ctx, f.verified); err != nil {
		t.Fatal(err)
	}
	if err := session.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	query("after.corp.example", "192.0.2.13")
	if original.received("after.corp.example.") || !replacement.received("after.corp.example.") {
		t.Fatal("restored old DNS assignment")
	}
}
