package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"
)

type LaunchdServiceOptions struct {
	Label              string
	BinaryPath         string
	ConfigPath         string
	StatePath          string
	IPCSocketPath      string
	DiagnosticsDir     string
	Interval           time.Duration
	Timeout            time.Duration
	STUNTimeout        time.Duration
	ListenPort         int
	ReconnectMaxDelay  time.Duration
	ReconnectJitter    float64
	WireGuardInterface string
	ProbeRTT           bool
	Debug              bool
	DebugLogDir        string
	RunAtLoad          bool
	KeepAlive          bool
	StartService       bool
	StandardOutPath    string
	StandardErrorPath  string
}

type LaunchdServiceArtifacts struct {
	PlistFile           string `json:"plist_file"`
	InstallScriptFile   string `json:"install_script_file"`
	UninstallScriptFile string `json:"uninstall_script_file"`
	Plist               string `json:"plist"`
	InstallScript       string `json:"install_script"`
	UninstallScript     string `json:"uninstall_script"`
}

func DefaultLaunchdServiceOptions() LaunchdServiceOptions {
	return LaunchdServiceOptions{
		Label:             "ru.endlessnet.client",
		BinaryPath:        "/Library/EndlessNet/endlessnet-client",
		ConfigPath:        "/Library/Application Support/EndlessNet/client.json",
		StatePath:         "/var/run/endlessnet/agent-state.json",
		IPCSocketPath:     ipc.DefaultDarwinSocket,
		DiagnosticsDir:    "/Library/Logs/EndlessNet/Diagnostics",
		Interval:          30 * time.Second,
		Timeout:           10 * time.Second,
		STUNTimeout:       2 * time.Second,
		ReconnectMaxDelay: 5 * time.Minute,
		ReconnectJitter:   0.2,
		Debug:             true,
		DebugLogDir:       "/Library/Logs/EndlessNet",
		RunAtLoad:         true,
		KeepAlive:         true,
		StartService:      true,
		StandardOutPath:   "/Library/Logs/EndlessNet/endlessnet-client.out.log",
		StandardErrorPath: "/Library/Logs/EndlessNet/endlessnet-client.err.log",
	}
}

func RenderLaunchdServiceArtifacts(opts LaunchdServiceOptions) (LaunchdServiceArtifacts, error) {
	opts = normalizeLaunchdServiceOptions(opts)
	if err := validateLaunchdServiceOptions(opts); err != nil {
		return LaunchdServiceArtifacts{}, err
	}
	return LaunchdServiceArtifacts{
		PlistFile:           opts.Label + ".plist",
		InstallScriptFile:   opts.Label + "-install.sh",
		UninstallScriptFile: opts.Label + "-uninstall.sh",
		Plist:               renderLaunchdPlist(opts),
		InstallScript:       renderLaunchdInstallScript(opts),
		UninstallScript:     renderLaunchdUninstallScript(opts),
	}, nil
}

func WriteLaunchdServiceArtifacts(outputDir string, opts LaunchdServiceOptions) (LaunchdServiceArtifacts, error) {
	if strings.TrimSpace(outputDir) == "" {
		return LaunchdServiceArtifacts{}, errors.New("output directory is required")
	}
	artifacts, err := RenderLaunchdServiceArtifacts(opts)
	if err != nil {
		return artifacts, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return artifacts, err
	}
	plistPath := filepath.Join(outputDir, artifacts.PlistFile)
	installPath := filepath.Join(outputDir, artifacts.InstallScriptFile)
	uninstallPath := filepath.Join(outputDir, artifacts.UninstallScriptFile)
	if err := WriteFileAtomic(plistPath, []byte(artifacts.Plist), 0o644); err != nil {
		return artifacts, err
	}
	if err := WriteFileAtomic(installPath, []byte(artifacts.InstallScript), 0o755); err != nil {
		return artifacts, err
	}
	if err := WriteFileAtomic(uninstallPath, []byte(artifacts.UninstallScript), 0o755); err != nil {
		return artifacts, err
	}
	artifacts.PlistFile = plistPath
	artifacts.InstallScriptFile = installPath
	artifacts.UninstallScriptFile = uninstallPath
	return artifacts, nil
}

func normalizeLaunchdServiceOptions(opts LaunchdServiceOptions) LaunchdServiceOptions {
	defaults := DefaultLaunchdServiceOptions()
	if strings.TrimSpace(opts.Label) == "" {
		opts.Label = defaults.Label
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
	if strings.TrimSpace(opts.IPCSocketPath) == "" {
		opts.IPCSocketPath = defaults.IPCSocketPath
	}
	if strings.TrimSpace(opts.DiagnosticsDir) == "" {
		opts.DiagnosticsDir = defaults.DiagnosticsDir
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
	if strings.TrimSpace(opts.WireGuardInterface) == "" {
		opts.WireGuardInterface = "endlessnet"
	}
	if strings.TrimSpace(opts.DebugLogDir) == "" {
		opts.DebugLogDir = defaults.DebugLogDir
	}
	if strings.TrimSpace(opts.StandardOutPath) == "" {
		opts.StandardOutPath = defaults.StandardOutPath
	}
	if strings.TrimSpace(opts.StandardErrorPath) == "" {
		opts.StandardErrorPath = defaults.StandardErrorPath
	}
	return opts
}

func validateLaunchdServiceOptions(opts LaunchdServiceOptions) error {
	for name, value := range map[string]string{
		"label":                 opts.Label,
		"binary path":           opts.BinaryPath,
		"config path":           opts.ConfigPath,
		"state path":            opts.StatePath,
		"ipc socket path":       opts.IPCSocketPath,
		"diagnostics directory": opts.DiagnosticsDir,
		"wireguard interface":   opts.WireGuardInterface,
		"debug log directory":   opts.DebugLogDir,
		"standard output path":  opts.StandardOutPath,
		"standard error path":   opts.StandardErrorPath,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must not contain newlines", name)
		}
	}
	if !safeLaunchdLabel(opts.Label) {
		return fmt.Errorf("launchd label %q is not a launchd-safe reverse-DNS label", opts.Label)
	}
	if opts.ListenPort < 0 || opts.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 0 and 65535")
	}
	if opts.ReconnectJitter < 0 || opts.ReconnectJitter > 1 {
		return fmt.Errorf("reconnect jitter must be between 0 and 1")
	}
	return nil
}

func renderLaunchdPlist(opts LaunchdServiceOptions) string {
	args := launchdAgentArguments(opts)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")
	writeLaunchdString(&b, "Label", opts.Label)
	b.WriteString("  <key>ProgramArguments</key>\n")
	b.WriteString("  <array>\n")
	for _, arg := range args {
		fmt.Fprintf(&b, "    <string>%s</string>\n", xmlEscape(arg))
	}
	b.WriteString("  </array>\n")
	writeLaunchdBool(&b, "RunAtLoad", opts.RunAtLoad)
	writeLaunchdBool(&b, "KeepAlive", opts.KeepAlive)
	writeLaunchdString(&b, "ProcessType", "Background")
	writeLaunchdString(&b, "WorkingDirectory", filepath.Dir(opts.BinaryPath))
	writeLaunchdString(&b, "StandardOutPath", opts.StandardOutPath)
	writeLaunchdString(&b, "StandardErrorPath", opts.StandardErrorPath)
	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")
	return b.String()
}

func launchdAgentArguments(opts LaunchdServiceOptions) []string {
	args := []string{
		opts.BinaryPath,
		"agent",
		"--config", opts.ConfigPath,
		"--state-output", opts.StatePath,
		"--diagnostics-dir", opts.DiagnosticsDir,
		"--ipc-socket", opts.IPCSocketPath,
		"--interval", opts.Interval.String(),
		"--timeout", opts.Timeout.String(),
		"--stun-timeout", opts.STUNTimeout.String(),
		"--reconnect-max-delay", opts.ReconnectMaxDelay.String(),
		"--reconnect-jitter", strconv.FormatFloat(opts.ReconnectJitter, 'f', -1, 64),
	}
	if opts.Debug {
		args = append(args, "--debug", "--debug-log-dir", opts.DebugLogDir)
	}
	if opts.ListenPort > 0 {
		args = append(args, "--listen-port", strconv.Itoa(opts.ListenPort))
	}
	if strings.TrimSpace(opts.WireGuardInterface) != "" {
		args = append(args, "--wg-interface", opts.WireGuardInterface)
	}
	if opts.ProbeRTT {
		args = append(args, "--probe-rtt")
	}
	return args
}

func renderLaunchdInstallScript(opts LaunchdServiceOptions) string {
	plistTarget := "/Library/LaunchDaemons/" + opts.Label + ".plist"
	dirs := launchdInstallDirectories(opts)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -eu\n\n")
	b.WriteString("if [ \"$(id -u)\" != \"0\" ]; then\n")
	b.WriteString("  echo \"EndlessNet launchd install must be run as root\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")
	fmt.Fprintf(&b, "Label=%s\n", quoteShellSingle(opts.Label))
	fmt.Fprintf(&b, "Binary=%s\n", quoteShellSingle(opts.BinaryPath))
	fmt.Fprintf(&b, "PlistTarget=%s\n", quoteShellSingle(plistTarget))
	b.WriteString("ScriptDir=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\n")
	fmt.Fprintf(&b, "PlistSource=\"$ScriptDir/%s\"\n\n", opts.Label+".plist")
	b.WriteString("if [ ! -x \"$Binary\" ]; then\n")
	b.WriteString("  echo \"EndlessNet binary is not executable: $Binary\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n")
	b.WriteString("if [ ! -f \"$PlistSource\" ]; then\n")
	b.WriteString("  echo \"LaunchDaemon plist not found: $PlistSource\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")
	for _, dir := range dirs {
		fmt.Fprintf(&b, "install -d -m %s %s\n", dir.mode, quoteShellSingle(dir.path))
	}
	b.WriteString("\n")
	b.WriteString("cp \"$PlistSource\" \"$PlistTarget\"\n")
	b.WriteString("chown root:wheel \"$PlistTarget\"\n")
	b.WriteString("chmod 0644 \"$PlistTarget\"\n")
	b.WriteString("launchctl bootout system \"$PlistTarget\" >/dev/null 2>&1 || true\n")
	b.WriteString("launchctl bootstrap system \"$PlistTarget\"\n")
	b.WriteString("launchctl enable \"system/$Label\" >/dev/null 2>&1 || true\n")
	if opts.StartService {
		b.WriteString("launchctl kickstart -k \"system/$Label\"\n")
	}
	return b.String()
}

func renderLaunchdUninstallScript(opts LaunchdServiceOptions) string {
	plistTarget := "/Library/LaunchDaemons/" + opts.Label + ".plist"
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -eu\n\n")
	b.WriteString("RemoveState=0\n")
	b.WriteString("case \"${1:-}\" in\n")
	b.WriteString("  --remove-state) RemoveState=1 ;;\n")
	b.WriteString("  \"\") ;;\n")
	b.WriteString("  *) echo \"usage: $0 [--remove-state]\" >&2; exit 2 ;;\n")
	b.WriteString("esac\n\n")
	b.WriteString("if [ \"$(id -u)\" != \"0\" ]; then\n")
	b.WriteString("  echo \"EndlessNet launchd uninstall must be run as root\" >&2\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n\n")
	fmt.Fprintf(&b, "Label=%s\n", quoteShellSingle(opts.Label))
	fmt.Fprintf(&b, "PlistTarget=%s\n", quoteShellSingle(plistTarget))
	fmt.Fprintf(&b, "IPCSocket=%s\n", quoteShellSingle(opts.IPCSocketPath))
	fmt.Fprintf(&b, "ConfigDir=%s\n", quoteShellSingle(filepath.Dir(opts.ConfigPath)))
	fmt.Fprintf(&b, "StateDir=%s\n", quoteShellSingle(filepath.Dir(opts.StatePath)))
	fmt.Fprintf(&b, "DiagnosticsDir=%s\n\n", quoteShellSingle(opts.DiagnosticsDir))
	b.WriteString("launchctl bootout system \"$PlistTarget\" >/dev/null 2>&1 || launchctl bootout \"system/$Label\" >/dev/null 2>&1 || true\n")
	b.WriteString("rm -f \"$PlistTarget\" \"$IPCSocket\"\n")
	b.WriteString("if [ \"$RemoveState\" = \"1\" ]; then\n")
	b.WriteString("  rm -rf \"$ConfigDir\" \"$StateDir\" \"$DiagnosticsDir\"\n")
	b.WriteString("fi\n")
	return b.String()
}

type launchdInstallDirectory struct {
	path string
	mode string
}

func launchdInstallDirectories(opts LaunchdServiceOptions) []launchdInstallDirectory {
	candidates := []launchdInstallDirectory{
		{path: filepath.Dir(opts.BinaryPath), mode: "0755"},
		{path: filepath.Dir(opts.ConfigPath), mode: "0700"},
		{path: filepath.Dir(opts.StatePath), mode: "0750"},
		{path: filepath.Dir(opts.IPCSocketPath), mode: "0750"},
		{path: opts.DiagnosticsDir, mode: "0700"},
		{path: filepath.Dir(opts.StandardOutPath), mode: "0750"},
		{path: filepath.Dir(opts.StandardErrorPath), mode: "0750"},
	}
	if strings.TrimSpace(opts.DebugLogDir) != "" && !strings.HasPrefix(opts.DebugLogDir, "~") {
		candidates = append(candidates, launchdInstallDirectory{path: opts.DebugLogDir, mode: "0750"})
	}
	seen := map[string]bool{}
	out := make([]launchdInstallDirectory, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.path = strings.TrimSpace(filepath.ToSlash(candidate.path))
		if candidate.path == "" || candidate.path == "." || seen[candidate.path] {
			continue
		}
		seen[candidate.path] = true
		out = append(out, candidate)
	}
	return out
}

func writeLaunchdString(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "  <key>%s</key>\n", xmlEscape(key))
	fmt.Fprintf(b, "  <string>%s</string>\n", xmlEscape(value))
}

func writeLaunchdBool(b *strings.Builder, key string, value bool) {
	fmt.Fprintf(b, "  <key>%s</key>\n", xmlEscape(key))
	if value {
		b.WriteString("  <true/>\n")
		return
	}
	b.WriteString("  <false/>\n")
}

func safeLaunchdLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" || strings.HasPrefix(label, ".") || strings.HasSuffix(label, ".") || strings.Contains(label, "..") {
		return false
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}

func quoteShellSingle(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
