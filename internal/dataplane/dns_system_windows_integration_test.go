//go:build windows && integration

package dataplane

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

// Uses machine DNS policy and a real Wintun adapter only on an explicitly
// selected, elevated, disposable Windows guest with the pinned DLL installed.
func TestWindowsSystemDNSAssignment(t *testing.T) {
	if os.Getenv("ENDLESSNET_TEST_WINDOWS_SYSTEM_DNS") != "1" {
		t.Skip("requires isolated Windows system DNS fixture")
	}
	f, store, _, initial, _, _, _, _, clock := wiredSessionFixture(t)
	f.cancel()
	config := f.config
	config.ApplyTimeout = 20 * time.Second
	// Keep the signed map and state fixture. All OS handles and resolver
	// operations in this session come from the production constructor.
	s, err := NewWindowsSession(t.Context(), store, clock, f.profileID, f.generation, config, f.resources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	id := uuid.NewString()[:8]
	short := "db" + id
	zone := "en" + id + ".invalid"
	fqdn := short + "." + zone + "."
	m := nextSessionMap(t, s, initial, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		snapshot.Peers = nil
		snapshot.Network.DNSConfig = &coordinator.DNSConfig{SearchDomains: []string{zone}, Zones: []coordinator.DNSZone{{Name: zone + ".", Version: 1, Records: []coordinator.DNSRecord{{Name: fqdn, Type: "A", Value: "192.0.2.41", TTL: 1}}}}}
	})
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		// The name is generated above using only letters, hex digits and dots.
		script := "$ErrorActionPreference='Stop'; try { (Resolve-DnsName -DnsOnly -NoHostsFile -QuickTimeout -Name '" + name + "' -Type A).IPAddress } catch { if ($_.FullyQualifiedErrorId -like 'DNS_ERROR_RCODE_NAME_ERROR,*') { exit 3 }; throw }"
		raw, err := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		return strings.TrimSpace(string(raw)), err
	}
	check := func(want string) {
		t.Helper()
		for _, name := range []string{fqdn, short} {
			if got, err := lookup(name); err != nil || got != want {
				t.Fatalf("%s: got %q (%v), want %s", name, got, err, want)
			}
		}
	}
	check("192.0.2.41")
	next := nextSessionMap(t, s, m, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		z := &snapshot.Network.DNSConfig.Zones[0]
		z.Version++
		z.Records[0].Value = "192.0.2.42"
		z.Records = append(z.Records, coordinator.DNSRecord{Name: "keep." + zone + ".", Type: "A", Value: "192.0.2.43", TTL: 1})
	})
	if err := s.Refresh(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second) // allow the previous one-second answer to expire
	check("192.0.2.42")
	expectAbsent := func(name string) {
		t.Helper()
		got, err := lookup(name)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Fatalf("expected system NXDOMAIN for %s: %q (%v)", name, got, err)
		}
	}
	trimmed := nextSessionMap(t, s, next, clock, func(snapshot *coordinator.NetworkMapSnapshot) {
		z := &snapshot.Network.DNSConfig.Zones[0]
		z.Version++
		z.Records = z.Records[1:]
	})
	if err := s.Refresh(t.Context(), trimmed); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	expectAbsent(fqdn)
	keep := "keep." + zone + "."
	if got, err := lookup(keep); err != nil || got != "192.0.2.43" {
		t.Fatalf("remaining record: %q (%v)", got, err)
	}
	withdraw := nextSessionMap(t, s, trimmed, clock, func(snapshot *coordinator.NetworkMapSnapshot) { snapshot.Network.DNSConfig = nil })
	if err := s.Refresh(t.Context(), withdraw); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	expectAbsent(keep)
}
