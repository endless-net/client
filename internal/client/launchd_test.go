package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRenderLaunchdServiceArtifacts(t *testing.T) {
	opts := DefaultLaunchdServiceOptions()
	opts.Interval = 7 * time.Second
	opts.Timeout = 3 * time.Second
	opts.STUNTimeout = time.Second
	opts.ListenPort = 51820
	opts.ReconnectMaxDelay = 42 * time.Second
	opts.ReconnectJitter = 0.3
	opts.ProbeRTT = true

	artifacts, err := RenderLaunchdServiceArtifacts(opts)
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.PlistFile != "ru.endlessnet.client.plist" {
		t.Fatalf("plist file = %q", artifacts.PlistFile)
	}
	if artifacts.InstallScriptFile != "ru.endlessnet.client-install.sh" || artifacts.UninstallScriptFile != "ru.endlessnet.client-uninstall.sh" {
		t.Fatalf("script files = %q %q", artifacts.InstallScriptFile, artifacts.UninstallScriptFile)
	}
	for _, want := range []string{
		"<key>Label</key>",
		"<string>ru.endlessnet.client</string>",
		"<key>ProgramArguments</key>",
		"<string>/Library/EndlessNet/endlessnet-client</string>",
		"<string>agent</string>",
		"<string>--config</string>",
		"<string>/Library/Application Support/EndlessNet/client.json</string>",
		"<string>--ipc-socket</string>",
		"<string>/var/run/endlessnet/client.sock</string>",
		"<string>--diagnostics-dir</string>",
		"<string>/Library/Logs/EndlessNet/Diagnostics</string>",
		"<string>--interval</string>",
		"<string>7s</string>",
		"<string>--listen-port</string>",
		"<string>51820</string>",
		"<string>--probe-rtt</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"<key>StandardErrorPath</key>",
	} {
		if !strings.Contains(artifacts.Plist, want) {
			t.Fatalf("launchd plist missing %q:\n%s", want, artifacts.Plist)
		}
	}
	for _, want := range []string{
		"launchctl bootstrap system \"$PlistTarget\"",
		"launchctl kickstart -k \"system/$Label\"",
		"chown root:wheel \"$PlistTarget\"",
		"chmod 0644 \"$PlistTarget\"",
		"install -d -m 0700 '/Library/Application Support/EndlessNet'",
		"install -d -m 0750 '/var/run/endlessnet'",
	} {
		if !strings.Contains(artifacts.InstallScript, want) {
			t.Fatalf("launchd install script missing %q:\n%s", want, artifacts.InstallScript)
		}
	}
	for _, want := range []string{
		"launchctl bootout system \"$PlistTarget\"",
		"rm -f \"$PlistTarget\" \"$IPCSocket\"",
		"--remove-state",
	} {
		if !strings.Contains(artifacts.UninstallScript, want) {
			t.Fatalf("launchd uninstall script missing %q:\n%s", want, artifacts.UninstallScript)
		}
	}
}

func TestRenderLaunchdServiceArtifactsRejectsUnsafeValues(t *testing.T) {
	opts := DefaultLaunchdServiceOptions()
	opts.Label = "ru.endlessnet client"
	if _, err := RenderLaunchdServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "launchd-safe") {
		t.Fatalf("unsafe label error = %v, want launchd-safe", err)
	}

	opts = DefaultLaunchdServiceOptions()
	opts.ConfigPath = "/Library/Application Support/EndlessNet/client.json\nProgram=/bin/sh"
	if _, err := RenderLaunchdServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "newlines") {
		t.Fatalf("newline path error = %v, want newlines", err)
	}
}

func TestDefaultLaunchdServiceUsesWireGuardGo(t *testing.T) {
	artifacts, err := RenderLaunchdServiceArtifacts(DefaultLaunchdServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(artifacts.Plist, "<string>--wg-interface</string>") || !strings.Contains(artifacts.Plist, "<string>endlessnet</string>") || strings.Contains(artifacts.Plist, "<string>--listen-port</string>") || strings.Contains(artifacts.Plist, "<string>--apply-wg-quick</string>") || strings.Contains(artifacts.Plist, "<string>--userspace-wireguard</string>") {
		t.Fatalf("default launchd service does not use automatic-port wireguard-go:\n%s", artifacts.Plist)
	}
}

func TestWriteLaunchdServiceArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	artifacts, err := WriteLaunchdServiceArtifacts(outputDir, DefaultLaunchdServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{artifacts.PlistFile, artifacts.InstallScriptFile, artifacts.UninstallScriptFile} {
		if filepath.Dir(path) != outputDir {
			t.Fatalf("artifact path %s is outside %s", path, outputDir)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" {
			want := os.FileMode(0o755)
			if strings.HasSuffix(path, ".plist") {
				want = 0o644
			}
			if info.Mode().Perm() != want {
				t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
			}
		}
	}
}
