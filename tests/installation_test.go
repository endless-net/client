package tests

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testcontrol"
	ipc "github.com/endless-net/client/ipc/v2"
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
		status := waitStatus(t, binary)
		if status.State != ipc.StateNeedsEnrollment || status.NodeCredentialPresent || status.NodeID != "" || status.PeerCount != 0 {
			t.Fatal("fresh service must need enrollment and have no node credentials, node or peers")
		}
		if got := string(command(t, binary, "version")); !strings.HasPrefix(got, "endlessnet-client ") {
			t.Fatal("installed executable did not return its version")
		}
		var networks ipc.NetworksResponse
		request(t, binary, "networks", &networks)
		if len(networks.Networks) != 0 || networks.SelectedNetworkID != "" {
			t.Fatal("fresh service must have no networks or selected network")
		}
		var diagnostics ipc.DiagnosticsResponse
		request(t, binary, "diagnostics", &diagnostics)
		d := diagnostics.Diagnostics
		if d.GeneratedAt == "" || d.Runtime.GOOS != runtime.GOOS || d.Runtime.GOARCH != runtime.GOARCH || d.Config.NodeCredentialPresent {
			t.Fatal("diagnostics must identify this platform and the unenrolled configuration")
		}
	}) {
		t.FailNow()
	}
	if !t.Run("disconnect-survives-service-restart", func(t *testing.T) {
		var response ipc.DisconnectResponse
		request(t, binary, "disconnect", &response)
		if response.DesiredState != ipc.DesiredDisconnected || !response.UserDisconnected {
			t.Fatal("disconnect did not acknowledge the user's intent")
		}
		restart(t)
		status := waitStatus(t, binary)
		if status.DesiredState != ipc.DesiredDisconnected || !status.UserDisconnected || status.NodeCredentialPresent {
			t.Fatal("service restart lost the disconnected intent or acquired credentials")
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
		status := waitStatus(t, binary)
		if status.WireGuard == nil || strings.TrimSpace(status.WireGuard.Interface) == "" {
			t.Fatal("connected installed service did not publish its network interface")
		}
		interfaceName := status.WireGuard.Interface
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
		fresh := waitStatus(t, binary)
		if fresh.State != ipc.StateNeedsEnrollment || fresh.NodeID != "" || fresh.NodeCredentialPresent || fresh.CachedMapPresent {
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
		replacement := waitInstalledCondition(t, binary, "identity reset reenrollment", func(v ipc.StatusResponse) bool {
			return v.NodeID != "" && v.NetworkID == replacementNetwork.ID && v.NodeCredentialPresent && v.CachedMapValid && v.WireGuard != nil && v.WireGuard.OK
		})
		if replacement.NodeID == status.NodeID {
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

func request(t *testing.T, binary, operation string, target any) {
	t.Helper()
	timeout := "5s"
	if operation == "connect" || operation == "disconnect" {
		timeout = "30s"
	}
	output := command(t, binary, "service", operation, "--timeout", timeout)
	if err := json.Unmarshal(output, target); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", operation, err)
	}
}

func waitStatus(t *testing.T, binary string) ipc.StatusResponse {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		output, err := run(binary, "service", "status", "--timeout", "2s")
		if err == nil {
			var status ipc.StatusResponse
			if err := json.Unmarshal(output, &status); err != nil {
				t.Fatalf("status returned invalid JSON: %v", err)
			}
			if status.IPCVersion != ipc.Version {
				t.Fatal("installed service returned an unexpected IPC version")
			}
			return status
		}
		if time.Now().After(deadline) {
			t.Fatal("installed service did not become reachable over local IPC within 45s")
		}
		time.Sleep(time.Second)
	}
}
