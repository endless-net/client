package client

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRenderWindowsServiceArtifacts(t *testing.T) {
	opts := DefaultWindowsServiceOptions()
	opts.Interval = 7 * time.Second
	opts.Timeout = 3 * time.Second
	opts.STUNTimeout = time.Second
	opts.ListenPort = 51820
	opts.ReconnectMaxDelay = 42 * time.Second
	opts.ReconnectJitter = 0.3

	artifacts, err := RenderWindowsServiceArtifacts(opts)
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.InstallScriptFile != "endlessnet-client-install.ps1" {
		t.Fatalf("install script file = %q", artifacts.InstallScriptFile)
	}
	if artifacts.UninstallScriptFile != "endlessnet-client-uninstall.ps1" {
		t.Fatalf("uninstall script file = %q", artifacts.UninstallScriptFile)
	}
	for _, want := range []string{
		"New-Service -Name $ServiceName",
		"--windows-service",
		`C:\Program Files\EndlessNet\endlessnet-client.exe`,
		`C:\ProgramData\EndlessNet\client.json`,
		`C:\ProgramData\EndlessNet\agent-state.json`,
		`C:\ProgramData\EndlessNet\Diagnostics`,
		"'--diagnostics-dir'",
		"'--interval'",
		"'7s'",
		"'--timeout'",
		"'3s'",
		"'--stun-timeout'",
		"'1s'",
		"'--listen-port'",
		"'51820'",
		"'--reconnect-max-delay'",
		"'42s'",
		"'--reconnect-jitter'",
		"'0.3'",
		"'--ipc-pipe'",
		`\\.\pipe\endlessnet-service`,
		"'--event-log-source'",
		"'EndlessNet Client'",
		"'--debug'",
		"'--debug-log-dir'",
		`~\.endlessnet\logs`,
		"New-EventLog -LogName Application -Source $EventLogSource",
		"icacls.exe $StateRoot /inheritance:r",
		"icacls.exe $StateRoot /setowner '*S-1-5-18'",
		"icacls.exe $Diagnostics /setowner '*S-1-5-18'",
		"'*S-1-5-18:(OI)(CI)(F)'",
		"'*S-1-5-32-544:(OI)(CI)(F)'",
		"sc.exe config $ServiceName",
		"Set-ItemProperty -Path \"HKLM:\\SYSTEM\\CurrentControlSet\\Services\\$ServiceName\"",
		"sc.exe failure $ServiceName",
		"restart/5000/restart/5000",
		"Start-Service -Name $ServiceName",
	} {
		if !strings.Contains(artifacts.InstallScript, want) {
			t.Fatalf("install script missing %q:\n%s", want, artifacts.InstallScript)
		}
	}
	if strings.Contains(artifacts.InstallScript, "--output") || strings.Contains(artifacts.InstallScript, "endlessnet.conf") {
		t.Fatalf("install script contains removed rendered-config plumbing:\n%s", artifacts.InstallScript)
	}
	for _, want := range []string{
		"Get-Service -Name $ServiceName",
		"Stop-Service -Name $ServiceName",
		"sc.exe delete $ServiceName",
		"param([switch]$RemoveState)",
		"Remove-EventLog -Source $EventLogSource",
		"if ($RemoveState)",
	} {
		if !strings.Contains(artifacts.UninstallScript, want) {
			t.Fatalf("uninstall script missing %q:\n%s", want, artifacts.UninstallScript)
		}
	}
	for _, script := range []string{artifacts.InstallScript, artifacts.UninstallScript} {
		if strings.Contains(script, "HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run") || strings.Contains(script, "Software\\Classes\\endlessnet") {
			t.Fatalf("service artifact unexpectedly manages a per-user application integration:\n%s", script)
		}
	}
	for _, leak := range []string{"super-secret-token", "private-key-value", "node-credential-value"} {
		if strings.Contains(artifacts.InstallScript, leak) || strings.Contains(artifacts.UninstallScript, leak) {
			t.Fatalf("windows service artifacts leaked %q", leak)
		}
	}
}

func TestRenderWindowsServiceArtifactsRejectsUnsafeValues(t *testing.T) {
	opts := DefaultWindowsServiceOptions()
	opts.ServiceName = "endlessnet client"
	if _, err := RenderWindowsServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "service-safe") {
		t.Fatalf("unsafe service name error = %v, want service-safe", err)
	}

	opts = DefaultWindowsServiceOptions()
	opts.ConfigPath = "C:\\ProgramData\\EndlessNet\\client.json\nWrite-Host token"
	if _, err := RenderWindowsServiceArtifacts(opts); err == nil || !strings.Contains(err.Error(), "newlines") {
		t.Fatalf("newline path error = %v, want newlines", err)
	}
}

func TestDefaultWindowsServiceUsesWireGuardGo(t *testing.T) {
	artifacts, err := RenderWindowsServiceArtifacts(DefaultWindowsServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(artifacts.InstallScript, "'--userspace-wireguard'") || strings.Contains(artifacts.InstallScript, "'--listen-port'") || strings.Contains(artifacts.InstallScript, "'--apply-wireguard'") || strings.Contains(artifacts.InstallScript, "'--wireguard-windows'") {
		t.Fatalf("default Windows service contains an obsolete WireGuard backend selector:\n%s", artifacts.InstallScript)
	}
	for _, want := range []string{"wintun.dll must be installed beside endlessnet-client.exe", "Get-AuthenticodeSignature -LiteralPath $Wintun"} {
		if !strings.Contains(artifacts.InstallScript, want) {
			t.Fatalf("default Windows service is missing Wintun validation %q:\n%s", want, artifacts.InstallScript)
		}
	}
}

func TestWriteWindowsServiceArtifacts(t *testing.T) {
	outputDir := t.TempDir()
	artifacts, err := WriteWindowsServiceArtifacts(outputDir, DefaultWindowsServiceOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{artifacts.InstallScriptFile, artifacts.UninstallScriptFile} {
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
