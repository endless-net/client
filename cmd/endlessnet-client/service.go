package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/client"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func cmdService(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("service command requires render-systemd, render-macos, render-windows, enroll, status, runtime-info, support-info, events, operation, profiles, create-profile, select-profile, rename-profile, remove-profile, connect, server-identity, trust-server, disconnect, logout, local-forget, networks, select-network, exit-nodes, exit-node, select-exit-node, clear-exit-node, set-resource-enabled, resources, peers, preferences, set-preferences, reset-preferences, managed-settings, session, renew-session, diagnostics, diagnostics-bundle, or logs-recent")
	}
	switch args[0] {
	case "update-info":
		return cmdServiceRPCQuery(args[0], args[1:], os.Stdout)
	case "export-diagnostics-bundle":
		return cmdServiceRPCBundleExport(args[1:], os.Stdout)
	case "status", "runtime-info", "support-info", "events", "operation", "profiles", "server-identity", "networks", "peers", "preferences", "managed-settings", "session", "diagnostics", "logs-recent", "exit-nodes", "exit-node", "resources":
		return cmdServiceRPCQuery(args[0], args[1:], os.Stdout)
	case "set-preferences", "reset-preferences", "connect", "disconnect", "logout", "create-profile", "select-profile", "rename-profile", "remove-profile", "local-forget", "enroll", "trust-server", "select-network", "diagnostics-bundle", "renew-session", "select-exit-node", "clear-exit-node", "set-resource-enabled":
		return cmdServiceRPCMutation(args[0], args[1:], os.Stdout)
	case "render-systemd":
		defaults := client.DefaultSystemdServiceOptions()
		fs := flag.NewFlagSet("service render-systemd", flag.ExitOnError)
		outputDir := fs.String("output-dir", "", "directory to write the generated service files")
		serviceName := fs.String("name", defaults.ServiceName, "systemd service name without .service")
		binaryPath := fs.String("binary", defaults.BinaryPath, "absolute endlessnet-client binary path on the target host")
		configPath := fs.String("config", defaults.ConfigPath, "client config path on the target host")
		statePath := fs.String("state", defaults.StatePath, "agent state JSON path written by the agent")
		ipcSocket := fs.String("ipc-socket", defaults.IPCSocketPath, "Unix domain socket used for local service IPC")
		intervalValue := fs.String("interval", defaults.Interval.String(), "agent sync interval")
		timeoutValue := fs.String("timeout", defaults.Timeout.String(), "control-plane and relay probe timeout")
		stunTimeoutValue := fs.String("stun-timeout", defaults.STUNTimeout.String(), "per-STUN endpoint timeout")
		listenPort := fs.Int("listen-port", defaults.ListenPort, "preferred wireguard-go UDP port; 0 selects one automatically")
		reconnectMaxDelayValue := fs.String("reconnect-max-delay", defaults.ReconnectMaxDelay.String(), "maximum delay after consecutive control-plane sync failures")
		reconnectJitter := fs.Float64("reconnect-jitter", defaults.ReconnectJitter, "fractional jitter applied to reconnect backoff; 0 disables jitter")
		wgInterface := fs.String("wg-interface", defaults.WGInterface, "wireguard-go interface name inspected by the agent")
		probeRTT := fs.Bool("probe-rtt", defaults.ProbeRTT, "include direct path RTT probes in ExecStart")
		user := fs.String("user", defaults.User, "system user for the service")
		group := fs.String("group", defaults.Group, "system group for the service")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		interval, err := time.ParseDuration(strings.TrimSpace(*intervalValue))
		if err != nil || interval <= 0 {
			return fmt.Errorf("interval must be a positive duration")
		}
		timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("timeout must be a positive duration")
		}
		stunTimeout, err := time.ParseDuration(strings.TrimSpace(*stunTimeoutValue))
		if err != nil || stunTimeout <= 0 {
			return fmt.Errorf("stun-timeout must be a positive duration")
		}
		reconnectMaxDelay, err := time.ParseDuration(strings.TrimSpace(*reconnectMaxDelayValue))
		if err != nil || reconnectMaxDelay <= 0 {
			return fmt.Errorf("reconnect-max-delay must be a positive duration")
		}
		if *reconnectJitter < 0 || *reconnectJitter > 1 {
			return fmt.Errorf("reconnect-jitter must be between 0 and 1")
		}
		artifacts, err := client.WriteSystemdServiceArtifacts(*outputDir, client.SystemdServiceOptions{
			ServiceName:       *serviceName,
			BinaryPath:        *binaryPath,
			ConfigPath:        *configPath,
			StatePath:         *statePath,
			IPCSocketPath:     *ipcSocket,
			Interval:          interval,
			Timeout:           timeout,
			STUNTimeout:       stunTimeout,
			ListenPort:        *listenPort,
			ReconnectMaxDelay: reconnectMaxDelay,
			ReconnectJitter:   *reconnectJitter,
			WGInterface:       *wgInterface,
			ProbeRTT:          *probeRTT,
			User:              *user,
			Group:             *group,
		})
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", artifacts.ServiceFile)
		fmt.Printf("wrote %s\n", artifacts.TmpfilesFile)
		return nil
	case "render-macos":
		defaults := client.DefaultLaunchdServiceOptions()
		fs := flag.NewFlagSet("service "+args[0], flag.ExitOnError)
		outputDir := fs.String("output-dir", "", "directory to write the generated launchd artifacts")
		label := fs.String("label", defaults.Label, "launchd service label")
		binaryPath := fs.String("binary", defaults.BinaryPath, "absolute endlessnet-client path on the target host")
		configPath := fs.String("config", defaults.ConfigPath, "client config path on the target host")
		statePath := fs.String("state", defaults.StatePath, "agent state JSON path written by the agent")
		ipcSocket := fs.String("ipc-socket", defaults.IPCSocketPath, "Unix domain socket used for local service IPC")
		diagnosticsDir := fs.String("diagnostics-dir", defaults.DiagnosticsDir, "diagnostics bundle directory on the target host")
		intervalValue := fs.String("interval", defaults.Interval.String(), "agent sync interval")
		timeoutValue := fs.String("timeout", defaults.Timeout.String(), "control-plane and relay probe timeout")
		stunTimeoutValue := fs.String("stun-timeout", defaults.STUNTimeout.String(), "per-STUN endpoint timeout")
		listenPort := fs.Int("listen-port", defaults.ListenPort, "preferred wireguard-go UDP port; 0 selects one automatically")
		reconnectMaxDelayValue := fs.String("reconnect-max-delay", defaults.ReconnectMaxDelay.String(), "maximum delay after consecutive control-plane sync failures")
		reconnectJitter := fs.Float64("reconnect-jitter", defaults.ReconnectJitter, "fractional jitter applied to reconnect backoff; 0 disables jitter")
		wgInterface := fs.String("wg-interface", defaults.WireGuardInterface, "live WireGuard interface to inspect for direct path decisions")
		probeRTT := fs.Bool("probe-rtt", defaults.ProbeRTT, "probe direct path RTT with ping; requires wg-interface")
		debugService := fs.Bool("debug", defaults.Debug, "enable maximum client debug logging")
		debugLogDir := fs.String("debug-log-dir", defaults.DebugLogDir, "debug log directory on the target host")
		runAtLoad := fs.Bool("run-at-load", defaults.RunAtLoad, "set launchd RunAtLoad")
		keepAlive := fs.Bool("keep-alive", defaults.KeepAlive, "set launchd KeepAlive")
		startService := fs.Bool("start", defaults.StartService, "start the service after install or upgrade")
		stdoutPath := fs.String("stdout", defaults.StandardOutPath, "launchd StandardOutPath")
		stderrPath := fs.String("stderr", defaults.StandardErrorPath, "launchd StandardErrorPath")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		interval, err := time.ParseDuration(strings.TrimSpace(*intervalValue))
		if err != nil || interval <= 0 {
			return fmt.Errorf("interval must be a positive duration")
		}
		timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("timeout must be a positive duration")
		}
		stunTimeout, err := time.ParseDuration(strings.TrimSpace(*stunTimeoutValue))
		if err != nil || stunTimeout <= 0 {
			return fmt.Errorf("stun-timeout must be a positive duration")
		}
		reconnectMaxDelay, err := time.ParseDuration(strings.TrimSpace(*reconnectMaxDelayValue))
		if err != nil || reconnectMaxDelay <= 0 {
			return fmt.Errorf("reconnect-max-delay must be a positive duration")
		}
		if *reconnectJitter < 0 || *reconnectJitter > 1 {
			return fmt.Errorf("reconnect-jitter must be between 0 and 1")
		}
		artifacts, err := client.WriteLaunchdServiceArtifacts(*outputDir, client.LaunchdServiceOptions{
			Label:              *label,
			BinaryPath:         *binaryPath,
			ConfigPath:         *configPath,
			StatePath:          *statePath,
			IPCSocketPath:      *ipcSocket,
			DiagnosticsDir:     *diagnosticsDir,
			Interval:           interval,
			Timeout:            timeout,
			STUNTimeout:        stunTimeout,
			ListenPort:         *listenPort,
			ReconnectMaxDelay:  reconnectMaxDelay,
			ReconnectJitter:    *reconnectJitter,
			WireGuardInterface: *wgInterface,
			ProbeRTT:           *probeRTT,
			Debug:              *debugService,
			DebugLogDir:        *debugLogDir,
			RunAtLoad:          *runAtLoad,
			KeepAlive:          *keepAlive,
			StartService:       *startService,
			StandardOutPath:    *stdoutPath,
			StandardErrorPath:  *stderrPath,
		})
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", artifacts.PlistFile)
		fmt.Printf("wrote %s\n", artifacts.InstallScriptFile)
		fmt.Printf("wrote %s\n", artifacts.UninstallScriptFile)
		return nil
	case "render-windows":
		defaults := client.DefaultWindowsServiceOptions()
		fs := flag.NewFlagSet("service render-windows", flag.ExitOnError)
		outputDir := fs.String("output-dir", "", "directory to write the generated service scripts")
		serviceName := fs.String("name", defaults.ServiceName, "Windows service name")
		displayName := fs.String("display-name", defaults.DisplayName, "Windows service display name")
		description := fs.String("description", defaults.Description, "Windows service description")
		binaryPath := fs.String("binary", defaults.BinaryPath, "absolute endlessnet-client.exe path on the target host")
		configPath := fs.String("config", defaults.ConfigPath, "client config path on the target host")
		statePath := fs.String("state", defaults.StatePath, "agent state JSON path written by the agent")
		diagnosticsDir := fs.String("diagnostics-dir", defaults.DiagnosticsDir, "diagnostics bundle directory on the target host")
		ipcPipe := fs.String("ipc-pipe", defaults.IPCPipe, "Windows named pipe used for local service IPC")
		eventLogSource := fs.String("event-log-source", defaults.EventLogSource, "Windows Event Log source used by the service")
		debugService := fs.Bool("debug", defaults.Debug, "enable maximum Windows client debug logging")
		debugLogDir := fs.String("debug-log-dir", defaults.DebugLogDir, "debug log directory on the target host; supports ~")
		intervalValue := fs.String("interval", defaults.Interval.String(), "agent sync interval")
		timeoutValue := fs.String("timeout", defaults.Timeout.String(), "control-plane and relay probe timeout")
		stunTimeoutValue := fs.String("stun-timeout", defaults.STUNTimeout.String(), "per-STUN endpoint timeout")
		listenPort := fs.Int("listen-port", defaults.ListenPort, "preferred wireguard-go UDP port; 0 selects one automatically")
		reconnectMaxDelayValue := fs.String("reconnect-max-delay", defaults.ReconnectMaxDelay.String(), "maximum delay after consecutive control-plane sync failures")
		reconnectJitter := fs.Float64("reconnect-jitter", defaults.ReconnectJitter, "fractional jitter applied to reconnect backoff; 0 disables jitter")
		startService := fs.Bool("start", defaults.StartService, "start the service after install or upgrade")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		interval, err := time.ParseDuration(strings.TrimSpace(*intervalValue))
		if err != nil || interval <= 0 {
			return fmt.Errorf("interval must be a positive duration")
		}
		timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("timeout must be a positive duration")
		}
		stunTimeout, err := time.ParseDuration(strings.TrimSpace(*stunTimeoutValue))
		if err != nil || stunTimeout <= 0 {
			return fmt.Errorf("stun-timeout must be a positive duration")
		}
		reconnectMaxDelay, err := time.ParseDuration(strings.TrimSpace(*reconnectMaxDelayValue))
		if err != nil || reconnectMaxDelay <= 0 {
			return fmt.Errorf("reconnect-max-delay must be a positive duration")
		}
		if *reconnectJitter < 0 || *reconnectJitter > 1 {
			return fmt.Errorf("reconnect-jitter must be between 0 and 1")
		}
		artifacts, err := client.WriteWindowsServiceArtifacts(*outputDir, client.WindowsServiceOptions{
			ServiceName:       *serviceName,
			DisplayName:       *displayName,
			Description:       *description,
			BinaryPath:        *binaryPath,
			ConfigPath:        *configPath,
			StatePath:         *statePath,
			DiagnosticsDir:    *diagnosticsDir,
			IPCPipe:           *ipcPipe,
			EventLogSource:    *eventLogSource,
			Debug:             *debugService,
			DebugLogDir:       *debugLogDir,
			Interval:          interval,
			Timeout:           timeout,
			STUNTimeout:       stunTimeout,
			ListenPort:        *listenPort,
			ReconnectMaxDelay: reconnectMaxDelay,
			ReconnectJitter:   *reconnectJitter,
			StartService:      *startService,
		})
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", artifacts.InstallScriptFile)
		fmt.Printf("wrote %s\n", artifacts.UninstallScriptFile)
		return nil
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func serviceIPCTransportFlags(fs *flag.FlagSet) (*string, *string) {
	ipcPipe := fs.String("ipc-pipe", "", "Windows named pipe used for local service IPC")
	ipcSocket := fs.String("ipc-socket", "", "Unix domain socket used for local service IPC")
	return ipcPipe, ipcSocket
}

func parsePositiveServiceIPCTimeout(value string) (time.Duration, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("timeout must be a positive duration")
	}
	return timeout, nil
}

type agentIterationOptions struct {
	ConfigPath     string
	StateOutput    string
	ListenPort     int
	Timeout        time.Duration
	STUNTimeout    time.Duration
	RelayTLSConfig *tls.Config
	Offline        bool
	MaxCacheAge    time.Duration
	WireGuard      agentWireGuard
	WGInterface    string
	ProbeRTT       bool
	FromRevision   uint64
}

type agentIPCOptions struct {
	Offline        bool
	Pipe           string
	UnixSocket     string
	ConfigPath     string
	StateOutput    string
	ConfigStore    *client.ConfigStore
	OperationMu    *sync.Mutex
	DiagnosticsDir string
	RecentLogs     *recentLogBuffer
	ListenPort     int
	WGInterface    string
	Timeout        time.Duration
	WireGuard      agentWireGuard
	ExitRuntime    *client.NativeExitRuntime
	SyncWake       chan struct{}
	ObserveRoutes  func(context.Context, string, []string) []client.WireGuardRouteInspection
}

func requestAgentSync(opts agentIPCOptions) {
	if opts.SyncWake == nil {
		return
	}
	select {
	case opts.SyncWake <- struct{}{}:
	default:
	}
}

func invalidateAgentSnapshot(opts agentIPCOptions) {
	statePath := strings.TrimSpace(opts.StateOutput)
	if statePath == "" {
		return
	}
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		log.Print("failed to invalidate stale agent state after tunnel transition")
	}
}

type agentWireGuard interface {
	RestoreExitProtection(context.Context, client.Config) error
	ControlPlaneHTTPClient(client.Config) (*http.Client, error)
	Configure(context.Context, client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error)
	Down(context.Context) (client.WireGuardApplyResult, error)
	LastEndpointDiscovery() client.WireGuardEngineEndpointDiscovery
	STUNSnapshot(context.Context, clientapi.RegisterNodeResponse, time.Duration) client.AgentSTUNSnapshot
	Inspection() client.WireGuardInspection
	TryInspection() (client.WireGuardInspection, bool)
	TryMapInspection(string, string, uint64, uint64) (client.WireGuardInspection, bool)
	PathStatus() []client.PeerPathStatus
	TryPathStatus(string, string, uint64, uint64) ([]client.PeerPathStatus, bool)
	TryResourceEnforcement(client.Config, time.Time) bool
	RelayStatus() (client.RelayDataplaneBridgeStatus, bool, error)
}

func agentConnectionIntentStore(opts agentIPCOptions) client.ConnectionIntentStore {
	store := opts.ConfigStore
	if store == nil && strings.TrimSpace(opts.ConfigPath) != "" {
		opened, err := client.OpenConfigStore(opts.ConfigPath)
		if err == nil {
			store = opened
		}
	}
	return client.NewConnectionIntentStore(store)
}

func agentServiceIPCConfigStore(opts agentIPCOptions) (*client.ConfigStore, error) {
	if opts.ConfigStore != nil {
		return opts.ConfigStore, nil
	}
	return client.OpenConfigStore(opts.ConfigPath)
}

func downAgentWireGuard(ctx context.Context, opts agentIPCOptions) (client.WireGuardApplyResult, error) {
	if opts.WireGuard == nil {
		return client.WireGuardApplyResult{Method: "wireguard-go"}, errors.New("wireguard-go engine is not initialized")
	}
	return opts.WireGuard.Down(ctx)
}

const serverMapSigningTrustChangedError = "server map signing trust changed"

func loadAgentSnapshotIfAvailable(path string) *client.AgentSnapshot {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	loaded, err := client.LoadAgentSnapshot(path)
	if err != nil {
		return nil
	}
	return &loaded
}

type recentLogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type recentLogBuffer struct {
	mu      sync.Mutex
	limit   int
	entries []recentLogEntry
}

func newRecentLogBuffer(limit int) *recentLogBuffer {
	if limit <= 0 {
		limit = 100
	}
	return &recentLogBuffer{limit: limit}
}

func (b *recentLogBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimSpace(client.RedactServiceLogMessage(line))
		if line == "" {
			continue
		}
		b.add(recentLogEntry{Timestamp: now, Message: line})
	}
	return len(p), nil
}

func (b *recentLogBuffer) add(entry recentLogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, entry)
	if overflow := len(b.entries) - b.limit; overflow > 0 {
		copy(b.entries, b.entries[overflow:])
		b.entries = b.entries[:b.limit]
	}
}

func (b *recentLogBuffer) Recent(limit int) []recentLogEntry {
	if b == nil {
		return nil
	}
	if limit <= 0 || limit > b.limit {
		limit = b.limit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	start := len(b.entries) - limit
	if start < 0 {
		start = 0
	}
	out := make([]recentLogEntry, len(b.entries[start:]))
	copy(out, b.entries[start:])
	return out
}

func recentLogEntries(logs *recentLogBuffer, limit int) []recentLogEntry {
	if logs == nil {
		return []recentLogEntry{}
	}
	return logs.Recent(limit)
}
