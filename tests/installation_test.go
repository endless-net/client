package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	native "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testcontrol"
)

// TestInstalledClient uses real service managers on disposable CI machines only.
// It never reads the service's private configuration or identity files.
func TestInstalledClient(t *testing.T) {
	if testing.Short() || os.Getenv("ENDLESSNET_INSTALL_TEST") != "1" {
		t.Skip("requires explicit opt-in on a disposable privileged CI runner")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("installation tests require a GitHub-hosted disposable runner")
	}
	source := requiredPath(t, "ENDLESSNET_TEST_BINARY")
	upgradeSource := requiredPath(t, "ENDLESSNET_TEST_UPGRADE_BINARY")
	initialVersion := strings.TrimSpace(os.Getenv("ENDLESSNET_TEST_INITIAL_VERSION"))
	upgradeVersion := strings.TrimSpace(os.Getenv("ENDLESSNET_TEST_UPGRADE_VERSION"))
	if initialVersion == "" || upgradeVersion == "" || initialVersion == upgradeVersion {
		t.Fatal("installation test requires distinct initial and upgrade versions")
	}
	installSource := source
	artifacts := t.TempDir()
	var binary, configPath string
	var start, stop, reinstall, upgrade, restart, uninstall, removeState func(*testing.T)
	var removed func() bool
	switch runtime.GOOS {
	case "linux":
		binary = "/opt/endlessnet/bin/endlessnet-client"
		configPath = "/var/lib/endlessnet/client.json"
		absent(t, binary, "/var/lib/endlessnet", "/lib/systemd/system/endlessnet-client.service")
		currentDeb := requiredPath(t, "ENDLESSNET_TEST_DEB")
		upgradeDeb := requiredPath(t, "ENDLESSNET_TEST_UPGRADE_DEB")
		reinstall = func(t *testing.T) { command(t, "dpkg", "--install", currentDeb) }
		upgrade = func(t *testing.T) {
			currentDeb = upgradeDeb
			command(t, "dpkg", "--install", currentDeb)
		}
		start = func(t *testing.T) { command(t, "systemctl", "start", "endlessnet-client") }
		stop = func(t *testing.T) { command(t, "systemctl", "stop", "endlessnet-client") }
		uninstall = func(t *testing.T) { command(t, "dpkg", "--remove", "endlessnet-client") }
		removeState = func(t *testing.T) { command(t, "dpkg", "--purge", "endlessnet-client") }
		t.Cleanup(func() { uninstall(t) })
		command(t, "dpkg", "--install", currentDeb)
		command(t, "systemctl", "start", "endlessnet-client")
		command(t, "systemctl", "is-active", "--quiet", "endlessnet-client")
		if got := strings.TrimSpace(string(command(t, "/usr/bin/endlessnet", "version"))); !strings.HasPrefix(got, "endlessnet-client ") {
			t.Fatal("installed CLI alias did not return the product version")
		}
		restart = func(t *testing.T) { command(t, "systemctl", "restart", "endlessnet-client") }
		removed = func() bool {
			_, err := os.Stat(binary)
			_, serviceErr := run("systemctl", "is-active", "--quiet", "endlessnet-client")
			_, unitErr := os.Stat("/lib/systemd/system/endlessnet-client.service")
			return os.IsNotExist(err) && os.IsNotExist(unitErr) && serviceErr != nil
		}
	case "darwin":
		binary = "/Library/EndlessNet/endlessnet-client"
		configPath = "/Library/Application Support/EndlessNet/client.json"
		absent(t, binary, "/Library/Application Support/EndlessNet", "/Library/LaunchDaemons/ru.endlessnet.client.plist")
		command(t, "install", "-d", "-m", "0755", filepath.Dir(binary))
		command(t, "install", "-m", "0755", source, binary)
		command(t, binary, "service", "render-macos", "--output-dir", artifacts, "--debug=false")
		reinstall = func(t *testing.T) { command(t, "sh", filepath.Join(artifacts, "ru.endlessnet.client-install.sh")) }
		upgrade = func(t *testing.T) {
			stop(t)
			installSource = upgradeSource
			copyPublicFile(t, installSource, binary)
			reinstall(t)
		}
		start = reinstall
		stop = func(t *testing.T) { command(t, "launchctl", "bootout", "system/ru.endlessnet.client") }
		uninstall = func(t *testing.T) { command(t, "sh", filepath.Join(artifacts, "ru.endlessnet.client-uninstall.sh")) }
		removeState = func(t *testing.T) {
			command(t, "sh", filepath.Join(artifacts, "ru.endlessnet.client-uninstall.sh"), "--remove-state")
		}
		t.Cleanup(func() { uninstall(t) })
		command(t, "sh", filepath.Join(artifacts, "ru.endlessnet.client-install.sh"))
		restart = func(t *testing.T) { command(t, "launchctl", "kickstart", "-k", "system/ru.endlessnet.client") }
		removed = func() bool {
			_, err := run("launchctl", "print", "system/ru.endlessnet.client")
			_, plistErr := os.Stat("/Library/LaunchDaemons/ru.endlessnet.client.plist")
			return err != nil && os.IsNotExist(plistErr)
		}
	case "windows":
		binary = `C:\Program Files\EndlessNet\endlessnet-client.exe`
		configPath = `C:\ProgramData\EndlessNet\client.json`
		absent(t, filepath.Dir(binary), `C:\ProgramData\EndlessNet`)
		command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "if (Get-Service endlessnet-client -ErrorAction SilentlyContinue) { exit 1 }")
		copyPublicFile(t, source, binary)
		copyPublicFile(t, requiredPath(t, "ENDLESSNET_TEST_WINTUN"), filepath.Join(filepath.Dir(binary), "wintun.dll"))
		command(t, binary, "service", "render-windows", "--output-dir", artifacts, "--debug=false")
		reinstall = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(artifacts, "endlessnet-client-install.ps1"))
		}
		upgrade = func(t *testing.T) {
			stop(t)
			installSource = upgradeSource
			copyPublicFile(t, installSource, binary)
			reinstall(t)
		}
		start = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; Start-Service endlessnet-client; (Get-Service endlessnet-client).WaitForStatus('Running', '00:00:30')")
		}
		stop = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; Stop-Service endlessnet-client; (Get-Service endlessnet-client).WaitForStatus('Stopped', '00:00:30')")
		}
		uninstall = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(artifacts, "endlessnet-client-uninstall.ps1"))
		}
		removeState = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(artifacts, "endlessnet-client-uninstall.ps1"), "-RemoveState")
		}
		t.Cleanup(func() { uninstall(t) })
		// This fresh installer only emits service-manager and public file errors;
		// unlike client/IPC output it contains no enrolled identity or credentials.
		output, err := run("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(artifacts, "endlessnet-client-install.ps1"))
		if err != nil {
			t.Fatalf("Windows installer failed: %v\n%s", err, output)
		}
		restart = func(t *testing.T) {
			command(t, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; Restart-Service endlessnet-client; (Get-Service endlessnet-client).WaitForStatus('Running', '00:00:30')")
		}
		removed = func() bool {
			_, err := run("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "if (Get-Service endlessnet-client -ErrorAction SilentlyContinue) { exit 1 }")
			return err == nil
		}
	default:
		t.Fatalf("unsupported installation platform: %s", runtime.GOOS)
	}

	if !t.Run("fresh-install", func(t *testing.T) {
		info := waitInstalledNativeRuntime(t, binary)
		status := waitInstalledNativeUnenrolled(t, binary)
		if status.GetStoredState().GetNodeCredentialPresent() || status.NodeId != "" || status.PeerCount != 0 || status.GetNetwork().GetId() != "" || status.ActiveProfileId != "" {
			t.Fatal("fresh service must need enrollment and have no node credentials, node or peers")
		}
		if got := string(command(t, binary, "version")); !strings.HasPrefix(got, "endlessnet-client ") {
			t.Fatal("installed executable did not return its version")
		}
		platform := map[string]native.Platform{"windows": native.Platform_PLATFORM_WINDOWS, "linux": native.Platform_PLATFORM_LINUX, "darwin": native.Platform_PLATFORM_MACOS}[runtime.GOOS]
		if info.GetBuild().GetPlatform() != platform || info.GetBuild().GetArchitecture() != runtime.GOARCH {
			t.Fatal("native runtime must identify the installed platform")
		}
		// Fresh installation has no profile. Do not make owner/profile-scoped
		// catalog or diagnostics calls and pretend their empty data is a report.
		if output, err := run(binary, "service", "diagnostics", "--timeout", "2s"); err == nil || len(output) == 0 {
			t.Fatal("profile-less native diagnostics did not fail explicitly")
		}
	}) {
		t.FailNow()
	}
	if !t.Run("profile-less-disconnect-rejected", func(t *testing.T) {
		before := waitInstalledNativeUnenrolled(t, binary)
		output, err := run(binary, "service", "disconnect", "--timeout", "2s", "--request-id", "1f510000-0000-4000-8000-000000000001",
			"--expected-instance-id", before.GetMetadata().GetInstanceId(), "--expected-revision", strconv.FormatUint(before.GetMetadata().GetRevision(), 10))
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "--profile-id") {
			t.Fatal("profile-less disconnect did not report its required profile (output withheld)")
		}
		restart(t)
		status := waitInstalledNativeUnenrolled(t, binary)
		if status.ActiveProfileId != "" || status.NodeId != "" || status.GetStoredState().GetNodeCredentialPresent() || status.GetStoredState().GetCachedMapPresent() ||
			status.GetIntent().GetDesiredState() != before.GetIntent().GetDesiredState() || status.UserDisconnected != before.UserDisconnected {
			t.Fatal("rejected profile-less mutation or restart created enrollment or changed intent")
		}
	}) {
		t.FailNow()
	}
	// Keep the contract peer alive through the later OS uninstall operation.
	s := testcontrol.New(t)
	if !t.Run("enrolled-reinstall", func(t *testing.T) {
		repair := func(t *testing.T) {
			// Linux restores files from the same Debian package. The macOS and
			// Windows service installers consume an already-staged core artifact.
			if runtime.GOOS != "linux" {
				copyPublicFile(t, installSource, binary)
			}
			reinstall(t)
			// The fixture explicitly stopped the service before removing the
			// binary. Debian preserves stopped intent across package reinstalls.
			if runtime.GOOS == "linux" {
				start(t)
			}
		}
		exerciseInstalledReinstall(t, s, binary, configPath, initialVersion, upgradeVersion, start, stop, reinstall, upgrade, repair)
	}) {
		t.FailNow()
	}
	t.Run("uninstall", func(t *testing.T) {
		status := waitInstalledNativeCondition(t, binary, func(v *native.Status) bool { return v.NodeId != "" && v.ActiveProfileId != "" })
		diagnostics := &native.GetDiagnosticsResponse{}
		awaitInstalledNative(t, binary, "diagnostics", diagnostics, func() bool {
			d := diagnostics.GetDiagnostics()
			return d.GetStatus().GetNodeId() == status.NodeId && d.GetStatus().GetActiveProfileId() == status.ActiveProfileId && d.GetStatus().GetMapRevision() >= status.MapRevision &&
				d.GetTunnel().GetOk() && d.GetTunnel().GetFailure() == nil && strings.TrimSpace(d.GetTunnel().GetInterfaceName()) != ""
		}, "--profile-id", status.ActiveProfileId)
		interfaceName := diagnostics.Diagnostics.Tunnel.InterfaceName
		uninstall(t)
		deadline := time.Now().Add(30 * time.Second)
		for !removed() || interfaceExists(interfaceName) {
			if time.Now().After(deadline) {
				t.Fatal("uninstaller left the service, package files or network interface installed")
			}
			time.Sleep(time.Second)
		}
		if _, err := os.Stat(configPath); err != nil {
			t.Fatal("ordinary uninstall did not retain enrolled client state")
		}
		if runtime.GOOS != "linux" {
			if _, err := run(binary, "service", "status", "--timeout", "2s"); err == nil {
				t.Fatal("service still answers IPC after uninstall")
			}
		}
		removeState(t)
		if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
			t.Fatal("explicit state removal retained the client state directory")
		}

		// HC-063: reinstalling after explicit state removal must begin with no
		// identity, and a new enrollment must create a different node. The old
		// provider-side node is deliberately not treated as locally removable.
		reinstall(t)
		if runtime.GOOS == "linux" {
			start(t)
		}
		fresh := waitInstalledNativeUnenrolled(t, binary)
		if fresh.ActiveProfileId != "" || !nativeEnrollmentAbsent(fresh) {
			t.Fatal("reinstalled client retained identity after explicit state removal")
		}
		replacementNetwork, replacementJoin, err := s.AddNetwork("reset-client", "198.18.96.0/24")
		if err != nil {
			t.Fatal(err)
		}
		trust, err := json.Marshal(s.Trust())
		if err != nil {
			t.Fatal(err)
		}
		trustFile := filepath.Join(t.TempDir(), "reset-public-trust.json")
		if err := os.WriteFile(trustFile, trust, 0o600); err != nil {
			t.Fatal(err)
		}
		stop(t)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		cmd := exec.CommandContext(ctx, binary, "up", "--config", configPath, "--server", s.URL(), "--network", replacementNetwork.Name, "--join-token-file", "-", "--hostname", "reset-node", "--map-signing-trust-file", trustFile, "--route-table", "auto")
		cmd.Stdin = strings.NewReader(replacementJoin)
		err = cmd.Run()
		cancel()
		if err != nil {
			t.Fatal("reenrollment after explicit state removal failed (output withheld)")
		}
		start(t)
		replacement := waitInstalledNativeCondition(t, binary, func(v *native.Status) bool {
			return v.NodeId != "" && v.ActiveProfileId != "" && v.GetNetwork().GetId() == replacementNetwork.ID && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapValid() && v.ConnectionPhase == native.ConnectionPhase_CONNECTION_PHASE_CONNECTED
		})
		if replacement.NodeId == status.NodeId {
			t.Fatal("identity reset reused the removed local node identity")
		}
	})
}

func interfaceExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}

func requiredPath(t *testing.T, key string) string {
	t.Helper()
	path := os.Getenv(key)
	info, err := os.Stat(path)
	if !filepath.IsAbs(path) || err != nil || info.IsDir() {
		t.Fatalf("%s must name an existing absolute file", key)
	}
	return path
}

func absent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refusing to modify a non-fresh installation: %s", path)
		}
	}
}

func copyPublicFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if strings.EqualFold(filepath.Base(name), "powershell.exe") {
		// pwsh's module paths can shadow Windows PowerShell's built-in
		// Security module. Let the child initialize its own default paths.
		cmd.Env = []string{}
		for _, value := range os.Environ() {
			key, _, _ := strings.Cut(value, "=")
			if !strings.EqualFold(key, "PSModulePath") {
				cmd.Env = append(cmd.Env, value)
			}
		}
	}
	return cmd.CombinedOutput()
}

func command(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	output, err := run(name, args...)
	if err != nil {
		// Never dump IPC responses, service state, or generated identities to CI logs.
		t.Fatalf("%s %v failed: %v", filepath.Base(name), args, err)
	}
	return output
}
