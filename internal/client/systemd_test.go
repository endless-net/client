package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRenderSystemdServiceArtifacts(t *testing.T) {
	opts := DefaultSystemdServiceOptions()
	opts.Interval = 7 * time.Second
	opts.Timeout = 3 * time.Second
	opts.STUNTimeout = time.Second
	opts.ListenPort = 51820
	opts.ReconnectMaxDelay = 42 * time.Second
	opts.ReconnectJitter = 0.3

	artifacts, err := RenderSystemdServiceArtifacts(opts)
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.ServiceFile != "endlessnet-client.service" {
		t.Fatalf("service file = %q", artifacts.ServiceFile)
	}
	if artifacts.TmpfilesFile != "endlessnet-client.tmpfiles.conf" {
		t.Fatalf("tmpfiles file = %q", artifacts.TmpfilesFile)
	}
	for _, want := range []string{
		`ExecStart="/opt/endlessnet/bin/endlessnet-client" "agent"`,
		`"--config" "/var/lib/endlessnet/client.json"`,
		`"--state-output" "/run/endlessnet/agent-state.json"`,
		`"--ipc-socket" "/run/endlessnet/client.sock"`,
		`"--interval" "7s"`,
		`"--timeout" "3s"`,
		`"--stun-timeout" "1s"`,
		`"--listen-port" "51820"`,
		`"--reconnect-max-delay" "42s"`,
		`"--reconnect-jitter" "0.3"`,
		`"--wg-interface" "endlessnet"`,
		"Restart=always",
		"KillSignal=SIGINT",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW",
		"StateDirectory=endlessnet",
		"StateDirectoryMode=0700",
		"Environment=ENDLESSNET_INSTALLATION_STATE_DIR=/var/lib/endlessnet",
		"ReadWritePaths=/var/lib/endlessnet /run/endlessnet",
	} {
		if !strings.Contains(artifacts.ServiceUnit, want) {
			t.Fatalf("service unit missing %q:\n%s", want, artifacts.ServiceUnit)
		}
	}
	for _, want := range []string{
		"d /var/lib/endlessnet 0700 root root -",
		"d /run/endlessnet 0750 root root -",
	} {
		if !strings.Contains(artifacts.TmpfilesConfig, want) {
			t.Fatalf("tmpfiles config missing %q:\n%s", want, artifacts.TmpfilesConfig)
		}
	}
	for _, leak := range []string{"super-secret-token", "private-key-value", "node-credential-value"} {
		if strings.Contains(artifacts.ServiceUnit, leak) || strings.Contains(artifacts.TmpfilesConfig, leak) {
			t.Fatalf("service artifacts leaked %q", leak)
		}
	}
}

func TestRenderSystemdServiceArtifactsRejectsUnsafeNames(t *testing.T) {
	opts := DefaultSystemdServiceOptions()
	opts.ServiceName = "endlessnet client"
	if _, err := RenderSystemdServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "unit-safe") {
		t.Fatalf("unsafe service name error = %v, want unit-safe", err)
	}

	opts = DefaultSystemdServiceOptions()
	opts.ConfigPath = "/var/lib/endlessnet/client.json\nEnvironment=TOKEN"
	if _, err := RenderSystemdServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "newlines") {
		t.Fatalf("newline path error = %v, want newlines", err)
	}
}

func TestDefaultSystemdServiceUsesWireGuardGo(t *testing.T) {
	artifacts, err := RenderSystemdServiceArtifacts(DefaultSystemdServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifacts.ServiceUnit, `"--wg-interface" "endlessnet"`) || strings.Contains(artifacts.ServiceUnit, `"--listen-port"`) || strings.Contains(artifacts.ServiceUnit, `"--apply-wg-quick"`) || strings.Contains(artifacts.ServiceUnit, `"--userspace-wireguard"`) {
		t.Fatalf("default service does not use automatic-port wireguard-go:\n%s", artifacts.ServiceUnit)
	}
}

func TestWriteSystemdServiceArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	artifacts, err := WriteSystemdServiceArtifacts(outputDir, DefaultSystemdServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{artifacts.ServiceFile, artifacts.TmpfilesFile} {
		if filepath.Dir(path) != outputDir {
			t.Fatalf("artifact path %s is outside %s", path, outputDir)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
			t.Fatalf("%s mode = %o, want 644", path, info.Mode().Perm())
		}
	}
}
