package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ipc "github.com/endless-net/client/ipc/v1"
)

type SystemdServiceOptions struct {
	ServiceName       string
	Description       string
	BinaryPath        string
	ConfigPath        string
	StatePath         string
	IPCSocketPath     string
	Interval          time.Duration
	Timeout           time.Duration
	STUNTimeout       time.Duration
	ListenPort        int
	ReconnectMaxDelay time.Duration
	ReconnectJitter   float64
	WGInterface       string
	ProbeRTT          bool
	User              string
	Group             string
	RestartDelay      time.Duration
	TimeoutStop       time.Duration
}

type SystemdServiceArtifacts struct {
	ServiceFile    string `json:"service_file"`
	TmpfilesFile   string `json:"tmpfiles_file"`
	ServiceUnit    string `json:"service_unit"`
	TmpfilesConfig string `json:"tmpfiles_config"`
}

func DefaultSystemdServiceOptions() SystemdServiceOptions {
	return SystemdServiceOptions{
		ServiceName:       "endlessnet-client",
		Description:       "EndlessNet client agent",
		BinaryPath:        "/opt/endlessnet/bin/endlessnet-client",
		ConfigPath:        DefaultLinuxServiceConfigPath,
		StatePath:         "/run/endlessnet/agent-state.json",
		IPCSocketPath:     ipc.DefaultUnixSocket,
		Interval:          30 * time.Second,
		Timeout:           10 * time.Second,
		STUNTimeout:       2 * time.Second,
		ReconnectMaxDelay: 5 * time.Minute,
		ReconnectJitter:   0.2,
		User:              "root",
		Group:             "root",
		RestartDelay:      5 * time.Second,
		TimeoutStop:       15 * time.Second,
	}
}

func RenderSystemdServiceArtifacts(opts SystemdServiceOptions) (SystemdServiceArtifacts, error) {
	opts = normalizeSystemdServiceOptions(opts)
	if err := validateSystemdServiceOptions(opts); err != nil {
		return SystemdServiceArtifacts{}, err
	}
	serviceUnit := renderSystemdUnit(opts)
	tmpfiles := renderSystemdTmpfiles(opts)
	return SystemdServiceArtifacts{
		ServiceFile:    opts.ServiceName + ".service",
		TmpfilesFile:   opts.ServiceName + ".tmpfiles.conf",
		ServiceUnit:    serviceUnit,
		TmpfilesConfig: tmpfiles,
	}, nil
}

func WriteSystemdServiceArtifacts(outputDir string, opts SystemdServiceOptions) (SystemdServiceArtifacts, error) {
	if strings.TrimSpace(outputDir) == "" {
		return SystemdServiceArtifacts{}, errors.New("output directory is required")
	}
	artifacts, err := RenderSystemdServiceArtifacts(opts)
	if err != nil {
		return artifacts, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return artifacts, err
	}
	servicePath := filepath.Join(outputDir, artifacts.ServiceFile)
	tmpfilesPath := filepath.Join(outputDir, artifacts.TmpfilesFile)
	if err := WriteFileAtomic(servicePath, []byte(artifacts.ServiceUnit), 0o644); err != nil {
		return artifacts, err
	}
	if err := WriteFileAtomic(tmpfilesPath, []byte(artifacts.TmpfilesConfig), 0o644); err != nil {
		return artifacts, err
	}
	artifacts.ServiceFile = servicePath
	artifacts.TmpfilesFile = tmpfilesPath
	return artifacts, nil
}

func normalizeSystemdServiceOptions(opts SystemdServiceOptions) SystemdServiceOptions {
	defaults := DefaultSystemdServiceOptions()
	if strings.TrimSpace(opts.ServiceName) == "" {
		opts.ServiceName = defaults.ServiceName
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
	if strings.TrimSpace(opts.IPCSocketPath) == "" {
		opts.IPCSocketPath = defaults.IPCSocketPath
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
	if strings.TrimSpace(opts.WGInterface) == "" {
		opts.WGInterface = "endlessnet"
	}
	if strings.TrimSpace(opts.User) == "" {
		opts.User = defaults.User
	}
	if strings.TrimSpace(opts.Group) == "" {
		opts.Group = defaults.Group
	}
	if opts.RestartDelay <= 0 {
		opts.RestartDelay = defaults.RestartDelay
	}
	if opts.TimeoutStop <= 0 {
		opts.TimeoutStop = defaults.TimeoutStop
	}
	return opts
}

func validateSystemdServiceOptions(opts SystemdServiceOptions) error {
	for name, value := range map[string]string{
		"service name":        opts.ServiceName,
		"binary path":         opts.BinaryPath,
		"config path":         opts.ConfigPath,
		"state path":          opts.StatePath,
		"ipc socket path":     opts.IPCSocketPath,
		"wireguard interface": opts.WGInterface,
		"user":                opts.User,
		"group":               opts.Group,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must not contain newlines", name)
		}
	}
	if opts.ListenPort < 0 || opts.ListenPort > 65535 {
		return fmt.Errorf("listen port must be between 0 and 65535")
	}
	if strings.ContainsAny(opts.ServiceName, `/\`) || strings.Contains(opts.ServiceName, " ") {
		return fmt.Errorf("service name %q is not a unit-safe name", opts.ServiceName)
	}
	if opts.ReconnectJitter < 0 || opts.ReconnectJitter > 1 {
		return fmt.Errorf("reconnect jitter must be between 0 and 1")
	}
	return nil
}

func renderSystemdUnit(opts SystemdServiceOptions) string {
	args := []string{
		opts.BinaryPath,
		"agent",
		"--config", opts.ConfigPath,
		"--state-output", opts.StatePath,
		"--ipc-socket", opts.IPCSocketPath,
		"--interval", opts.Interval.String(),
		"--timeout", opts.Timeout.String(),
		"--stun-timeout", opts.STUNTimeout.String(),
		"--reconnect-max-delay", opts.ReconnectMaxDelay.String(),
		"--reconnect-jitter", strconv.FormatFloat(opts.ReconnectJitter, 'f', -1, 64),
	}
	if opts.ListenPort > 0 {
		args = append(args, "--listen-port", strconv.Itoa(opts.ListenPort))
	}
	if strings.TrimSpace(opts.WGInterface) != "" {
		args = append(args, "--wg-interface", opts.WGInterface)
	}
	if opts.ProbeRTT {
		args = append(args, "--probe-rtt")
	}
	execStart := quoteSystemdCommand(args)
	readWritePaths := uniqueSystemdPaths([]string{
		filepath.ToSlash(filepath.Dir(opts.ConfigPath)),
		filepath.ToSlash(filepath.Dir(opts.StatePath)),
		filepath.ToSlash(filepath.Dir(opts.IPCSocketPath)),
	})
	return fmt.Sprintf(`[Unit]
Description=%s
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=%s
Group=%s
RuntimeDirectory=endlessnet
RuntimeDirectoryMode=0750
StateDirectory=endlessnet
StateDirectoryMode=0700
ConfigurationDirectory=endlessnet
ConfigurationDirectoryMode=0700
Environment=ENDLESSNET_INSTALLATION_STATE_DIR=/var/lib/endlessnet
ExecStart=%s
Restart=always
RestartSec=%s
KillSignal=SIGINT
TimeoutStopSec=%s
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
ReadWritePaths=%s
LockPersonality=true
RestrictRealtime=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`, opts.Description, opts.User, opts.Group, execStart, opts.RestartDelay.String(), opts.TimeoutStop.String(), strings.Join(readWritePaths, " "))
}

func renderSystemdTmpfiles(opts SystemdServiceOptions) string {
	paths := uniqueSystemdPaths([]string{
		filepath.ToSlash(filepath.Dir(opts.ConfigPath)),
		filepath.ToSlash(filepath.Dir(opts.StatePath)),
		filepath.ToSlash(filepath.Dir(opts.IPCSocketPath)),
	})
	var b strings.Builder
	b.WriteString("# EndlessNet client agent directories\n")
	for _, path := range paths {
		mode := "0750"
		if strings.HasPrefix(path, "/etc/endlessnet") || strings.HasPrefix(path, "/var/lib/endlessnet") {
			mode = "0700"
		}
		fmt.Fprintf(&b, "d %s %s %s %s -\n", path, mode, opts.User, opts.Group)
	}
	return b.String()
}

func uniqueSystemdPaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func quoteSystemdCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		quoted = append(quoted, strconv.Quote(arg))
	}
	return strings.Join(quoted, " ")
}
