package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ipc "github.com/endless-net/client/ipc/v2"
)

type WindowsServiceOptions struct {
	ServiceName       string
	DisplayName       string
	Description       string
	BinaryPath        string
	ConfigPath        string
	StatePath         string
	DiagnosticsDir    string
	IPCPipe           string
	EventLogSource    string
	Interval          time.Duration
	Timeout           time.Duration
	STUNTimeout       time.Duration
	ListenPort        int
	ReconnectMaxDelay time.Duration
	ReconnectJitter   float64
	StartService      bool
	Debug             bool
	DebugLogDir       string
}

type WindowsServiceArtifacts struct {
	InstallScriptFile   string `json:"install_script_file"`
	UninstallScriptFile string `json:"uninstall_script_file"`
	InstallScript       string `json:"install_script"`
	UninstallScript     string `json:"uninstall_script"`
}

func DefaultWindowsServiceOptions() WindowsServiceOptions {
	return WindowsServiceOptions{
		ServiceName:       "endlessnet-client",
		DisplayName:       "EndlessNet Client",
		Description:       "EndlessNet client agent",
		BinaryPath:        `C:\Program Files\EndlessNet\endlessnet-client.exe`,
		ConfigPath:        `C:\ProgramData\EndlessNet\client.json`,
		StatePath:         `C:\ProgramData\EndlessNet\agent-state.json`,
		DiagnosticsDir:    `C:\ProgramData\EndlessNet\Diagnostics`,
		IPCPipe:           ipc.DefaultWindowsPipe,
		EventLogSource:    DefaultWindowsEventLogSource,
		Interval:          30 * time.Second,
		Timeout:           10 * time.Second,
		STUNTimeout:       2 * time.Second,
		ReconnectMaxDelay: 5 * time.Minute,
		ReconnectJitter:   0.2,
		StartService:      true,
		Debug:             true,
		DebugLogDir:       DefaultDebugLogDir,
	}
}

func RenderWindowsServiceArtifacts(opts WindowsServiceOptions) (WindowsServiceArtifacts, error) {
	opts = normalizeWindowsServiceOptions(opts)
	if err := validateWindowsServiceOptions(opts); err != nil {
		return WindowsServiceArtifacts{}, err
	}
	return WindowsServiceArtifacts{
		InstallScriptFile:   opts.ServiceName + "-install.ps1",
		UninstallScriptFile: opts.ServiceName + "-uninstall.ps1",
		InstallScript:       renderWindowsServiceInstallScript(opts),
		UninstallScript:     renderWindowsServiceUninstallScript(opts),
	}, nil
}

func WriteWindowsServiceArtifacts(outputDir string, opts WindowsServiceOptions) (WindowsServiceArtifacts, error) {
	if strings.TrimSpace(outputDir) == "" {
		return WindowsServiceArtifacts{}, errors.New("output directory is required")
	}
	artifacts, err := RenderWindowsServiceArtifacts(opts)
	if err != nil {
		return artifacts, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return artifacts, err
	}
	installPath := filepath.Join(outputDir, artifacts.InstallScriptFile)
	uninstallPath := filepath.Join(outputDir, artifacts.UninstallScriptFile)
	if err := WriteFileAtomic(installPath, []byte(artifacts.InstallScript), 0o644); err != nil {
		return artifacts, err
	}
	if err := WriteFileAtomic(uninstallPath, []byte(artifacts.UninstallScript), 0o644); err != nil {
		return artifacts, err
	}
	artifacts.InstallScriptFile = installPath
	artifacts.UninstallScriptFile = uninstallPath
	return artifacts, nil
}

func normalizeWindowsServiceOptions(opts WindowsServiceOptions) WindowsServiceOptions {
	defaults := DefaultWindowsServiceOptions()
	if strings.TrimSpace(opts.ServiceName) == "" {
		opts.ServiceName = defaults.ServiceName
	}
	if strings.TrimSpace(opts.DisplayName) == "" {
		opts.DisplayName = defaults.DisplayName
	}
	if strings.TrimSpace(opts.Description) == "" {
		opts.Description = defaults.Description
	}
	if strings.TrimSpace(opts.BinaryPath) == "" {
		opts.BinaryPath = defaults.BinaryPath
	}
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = defaults.ConfigPath
	}
	if strings.TrimSpace(opts.StatePath) == "" {
		opts.StatePath = defaults.StatePath
	}
	if strings.TrimSpace(opts.DiagnosticsDir) == "" {
		opts.DiagnosticsDir = defaults.DiagnosticsDir
	}
	if strings.TrimSpace(opts.IPCPipe) == "" {
		opts.IPCPipe = defaults.IPCPipe
	}
	if strings.TrimSpace(opts.EventLogSource) == "" {
		opts.EventLogSource = defaults.EventLogSource
	}
	if opts.Interval <= 0 {
		opts.Interval = defaults.Interval
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaults.Timeout
	}
	if opts.STUNTimeout <= 0 {
		opts.STUNTimeout = defaults.STUNTimeout
	}
	if opts.ReconnectMaxDelay <= 0 {
		opts.ReconnectMaxDelay = defaults.ReconnectMaxDelay
	}
	if strings.TrimSpace(opts.DebugLogDir) == "" {
		opts.DebugLogDir = defaults.DebugLogDir
	}
	return opts
}

func validateWindowsServiceOptions(opts WindowsServiceOptions) error {
	for name, value := range map[string]string{
		"service name":          opts.ServiceName,
		"display name":          opts.DisplayName,
		"description":           opts.Description,
		"binary path":           opts.BinaryPath,
		"config path":           opts.ConfigPath,
		"state path":            opts.StatePath,
		"diagnostics directory": opts.DiagnosticsDir,
		"ipc pipe":              opts.IPCPipe,
		"event log source":      opts.EventLogSource,
		"debug log directory":   opts.DebugLogDir,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must not contain newlines", name)
		}
	}
	if strings.ContainsAny(opts.ServiceName, `/\:"'`) || strings.ContainsAny(opts.ServiceName, " \t") {
		return fmt.Errorf("service name %q is not a service-safe name", opts.ServiceName)
	}
	if opts.ListenPort < 0 || opts.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 0 and 65535")
	}
	if opts.ReconnectJitter < 0 || opts.ReconnectJitter > 1 {
		return fmt.Errorf("reconnect jitter must be between 0 and 1")
	}
	return nil
}

func renderWindowsServiceInstallScript(opts WindowsServiceOptions) string {
	args := []string{
		"agent",
		"--windows-service",
		"--config", opts.ConfigPath,
		"--state-output", opts.StatePath,
		"--diagnostics-dir", opts.DiagnosticsDir,
		"--interval", opts.Interval.String(),
		"--timeout", opts.Timeout.String(),
		"--stun-timeout", opts.STUNTimeout.String(),
		"--reconnect-max-delay", opts.ReconnectMaxDelay.String(),
		"--reconnect-jitter", strconv.FormatFloat(opts.ReconnectJitter, 'f', -1, 64),
		"--ipc-pipe", opts.IPCPipe,
		"--event-log-source", opts.EventLogSource,
	}
	if opts.Debug {
		args = append(args, "--debug", "--debug-log-dir", opts.DebugLogDir)
	}
	if opts.ListenPort > 0 {
		args = append(args, "--listen-port", strconv.Itoa(opts.ListenPort))
	}

	var b strings.Builder
	b.WriteString("# EndlessNet client Windows service installer\n")
	b.WriteString("#Requires -RunAsAdministrator\n")
	b.WriteString("$ErrorActionPreference = 'Stop'\n\n")
	fmt.Fprintf(&b, "$ServiceName = %s\n", quotePowerShellSingle(opts.ServiceName))
	fmt.Fprintf(&b, "$DisplayName = %s\n", quotePowerShellSingle(opts.DisplayName))
	fmt.Fprintf(&b, "$Description = %s\n", quotePowerShellSingle(opts.Description))
	fmt.Fprintf(&b, "$Binary = %s\n", quotePowerShellSingle(opts.BinaryPath))
	fmt.Fprintf(&b, "$Config = %s\n", quotePowerShellSingle(opts.ConfigPath))
	fmt.Fprintf(&b, "$State = %s\n", quotePowerShellSingle(opts.StatePath))
	fmt.Fprintf(&b, "$Diagnostics = %s\n", quotePowerShellSingle(opts.DiagnosticsDir))
	fmt.Fprintf(&b, "$EventLogSource = %s\n", quotePowerShellSingle(opts.EventLogSource))
	fmt.Fprintf(&b, "$Debug = $%t\n", opts.Debug)
	fmt.Fprintf(&b, "$DebugLogDir = %s\n", quotePowerShellSingle(opts.DebugLogDir))
	fmt.Fprintf(&b, "$StartService = $%t\n\n", opts.StartService)
	b.WriteString("foreach ($dir in @(\n")
	b.WriteString("    (Split-Path -Parent $Binary),\n")
	b.WriteString("    (Split-Path -Parent $Config),\n")
	b.WriteString("    (Split-Path -Parent $State),\n")
	b.WriteString("    $Diagnostics\n")
	b.WriteString(")) {\n")
	b.WriteString("    if ($dir) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }\n")
	b.WriteString("}\n\n")
	b.WriteString("$Wintun = Join-Path (Split-Path -Parent $Binary) 'wintun.dll'\n")
	b.WriteString("if (-not (Test-Path -LiteralPath $Wintun)) { throw \"wintun.dll must be installed beside endlessnet-client.exe\" }\n")
	b.WriteString("$WintunSignature = Get-AuthenticodeSignature -LiteralPath $Wintun\n")
	b.WriteString("if ($WintunSignature.Status -ne 'Valid') { throw \"wintun.dll Authenticode signature is not valid: $($WintunSignature.Status)\" }\n\n")
	b.WriteString("$StateRoot = Split-Path -Parent $Config\n")
	b.WriteString("icacls.exe $StateRoot /setowner '*S-1-5-18' | Out-Null\n")
	b.WriteString("icacls.exe $StateRoot /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' | Out-Null\n")
	b.WriteString("icacls.exe $Diagnostics /setowner '*S-1-5-18' | Out-Null\n")
	b.WriteString("icacls.exe $Diagnostics /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' | Out-Null\n\n")
	b.WriteString("if (-not [System.Diagnostics.EventLog]::SourceExists($EventLogSource)) {\n")
	b.WriteString("    New-EventLog -LogName Application -Source $EventLogSource\n")
	b.WriteString("}\n\n")
	b.WriteString("$agentArgs = @(\n")
	for i, arg := range args {
		suffix := ","
		if i == len(args)-1 {
			suffix = ""
		}
		fmt.Fprintf(&b, "    %s%s\n", quotePowerShellSingle(arg), suffix)
	}
	b.WriteString(")\n\n")
	b.WriteString("$quotedArgs = $agentArgs | ForEach-Object { '\"' + ($_ -replace '\"', '\\\"') + '\"' }\n")
	b.WriteString("$binaryPathName = '\"' + $Binary + '\" ' + ($quotedArgs -join ' ')\n")
	b.WriteString("$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue\n")
	b.WriteString("if ($service) {\n")
	b.WriteString("    if ($service.Status -ne 'Stopped') {\n")
	b.WriteString("        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue\n")
	b.WriteString("        $service.WaitForStatus('Stopped', '00:00:30')\n")
	b.WriteString("    }\n")
	b.WriteString("    sc.exe config $ServiceName binPath= $binaryPathName start= auto DisplayName= $DisplayName | Out-Null\n")
	b.WriteString("    Set-ItemProperty -Path \"HKLM:\\SYSTEM\\CurrentControlSet\\Services\\$ServiceName\" -Name Description -Value $Description\n")
	b.WriteString("} else {\n")
	b.WriteString("    New-Service -Name $ServiceName -BinaryPathName $binaryPathName -DisplayName $DisplayName -Description $Description -StartupType Automatic | Out-Null\n")
	b.WriteString("}\n")
	b.WriteString("sc.exe failure $ServiceName reset= 60 actions= restart/5000/restart/5000/\"\"/5000 | Out-Null\n")
	b.WriteString("if ($StartService) {\n")
	b.WriteString("    Start-Service -Name $ServiceName\n")
	b.WriteString("}\n")
	return b.String()
}

func renderWindowsServiceUninstallScript(opts WindowsServiceOptions) string {
	var b strings.Builder
	b.WriteString("# EndlessNet client Windows service uninstaller\n")
	b.WriteString("#Requires -RunAsAdministrator\n")
	b.WriteString("param([switch]$RemoveState)\n")
	b.WriteString("$ErrorActionPreference = 'Stop'\n\n")
	fmt.Fprintf(&b, "$ServiceName = %s\n", quotePowerShellSingle(opts.ServiceName))
	fmt.Fprintf(&b, "$Config = %s\n", quotePowerShellSingle(opts.ConfigPath))
	fmt.Fprintf(&b, "$State = %s\n\n", quotePowerShellSingle(opts.StatePath))
	fmt.Fprintf(&b, "$EventLogSource = %s\n\n", quotePowerShellSingle(opts.EventLogSource))
	b.WriteString("$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue\n")
	b.WriteString("if ($service) {\n")
	b.WriteString("    if ($service.Status -ne 'Stopped') {\n")
	b.WriteString("        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue\n")
	b.WriteString("        $service.WaitForStatus('Stopped', '00:00:30')\n")
	b.WriteString("    }\n")
	b.WriteString("    sc.exe delete $ServiceName | Out-Null\n")
	b.WriteString("}\n")
	b.WriteString("if ([System.Diagnostics.EventLog]::SourceExists($EventLogSource)) {\n")
	b.WriteString("    Remove-EventLog -Source $EventLogSource -ErrorAction SilentlyContinue\n")
	b.WriteString("}\n")
	b.WriteString("if ($RemoveState) {\n")
	b.WriteString("    foreach ($path in @((Split-Path -Parent $Config), (Split-Path -Parent $State))) {\n")
	b.WriteString("        if ($path -and (Test-Path -LiteralPath $path)) { Remove-Item -LiteralPath $path -Recurse -Force }\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

func quotePowerShellSingle(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
