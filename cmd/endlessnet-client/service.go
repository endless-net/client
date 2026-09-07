package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientapiv2 "github.com/endless-net/client-api/clientapi/v2"
)

func cmdService(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("service command requires render-systemd, render-macos, render-windows, enroll, status, events, connect, server-identity, trust-server, disconnect, logout, local-forget, networks, select-network, diagnostics, diagnostics-bundle, or logs-recent")
	}
	switch args[0] {
	case "enroll":
		fs := flag.NewFlagSet("service enroll", flag.ExitOnError)
		ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
		serverURL := fs.String("server", "", "EndlessNet server URL")
		joinToken := fs.String("join-token", "", "one-time node join token")
		joinTokenFile := fs.String("join-token-file", "", "read one-time node join token from this file, or '-' for stdin")
		mode := fs.String("mode", "", "Windows enrollment mode: workstation, server, or subnet-router")
		hostname := fs.String("hostname", "", "hostname to register for this device; defaults to the OS hostname")
		idempotencyKey := fs.String("idempotency-key", "", "registration retry idempotency key")
		timeoutValue := fs.String("timeout", "2m", "maximum time to wait for service enrollment")
		jsonOutput := fs.Bool("json", false, "write service enrollment response as JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
		if err != nil || timeout <= 0 {
			return fmt.Errorf("timeout must be a positive duration")
		}
		effectiveJoinToken, err := secretFlagValue("join-token", *joinToken, *joinTokenFile)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		payload, err := serviceEnrollViaIPCWithRetry(ctx, newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket), effectiveJoinToken, *serverURL, *mode, *hostname, *idempotencyKey)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(payload)
		}
		state := strings.TrimSpace(string(payload.State))
		if state == "" || state == "<nil>" {
			state = "unknown"
		}
		fmt.Printf("service enrollment state: %s\n", state)
		if approvalURL := strings.TrimSpace(payload.ApprovalURL); approvalURL != "" {
			fmt.Printf("Open this URL to approve the device:\n%s\n", approvalURL)
		}
		return nil
	case "status":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodGet, ipc.PathStatus, nil, &ipc.StatusResponse{})
	case "events":
		return cmdServiceIPCEvents(args[0], args[1:])
	case "connect":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodPost, ipc.PathConnect, ipc.ConnectRequest{}, &ipc.ConnectResponse{})
	case "server-identity":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodGet, ipc.PathServerIdentity, nil, &ipc.ServerIdentityResponse{})
	case "trust-server":
		fs := flag.NewFlagSet("service trust-server", flag.ExitOnError)
		ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
		timeoutValue := fs.String("timeout", "2m", "maximum time to wait for trust recovery and connection")
		confirmedKeyID := fs.String("confirmed-key-id", "", "server signing key ID confirmed by the operator")
		confirmedControlOrigin := fs.String("confirmed-control-origin", "", "control-plane origin confirmed by the operator")
		yes := fs.Bool("yes", false, "confirm replacement of the pinned server signing identity")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*yes {
			return errors.New("trust-server requires --yes after verifying the announced key ID")
		}
		timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var payload ipc.TrustServerResponse
		if err := newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket).Request(ctx, http.MethodPost, ipc.PathTrustServer, ipc.TrustServerRequest{
			ConfirmedControlOrigin: strings.TrimSpace(*confirmedControlOrigin),
			ConfirmedKeyID:         strings.TrimSpace(*confirmedKeyID),
		}, &payload); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(payload)
	case "disconnect":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodPost, ipc.PathDisconnect, ipc.DisconnectRequest{}, &ipc.DisconnectResponse{})
	case "logout":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodPost, ipc.PathLogout, ipc.LogoutRequest{}, &ipc.LogoutResponse{})
	case "local-forget":
		fs := flag.NewFlagSet("service local-forget", flag.ExitOnError)
		ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
		timeoutValue := fs.String("timeout", "2m", "maximum time to wait for local enrollment cleanup")
		confirmed := fs.Bool("confirm-local-forget", false, "confirm local cleanup when remote credential revocation is unconfirmed")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*confirmed {
			return errors.New("local-forget requires --confirm-local-forget")
		}
		timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var payload ipc.LocalForgetResponse
		if err := newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket).Request(ctx, http.MethodPost, ipc.PathLocalForget, ipc.LocalForgetRequest{Confirmed: true}, &payload); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(payload)
	case "networks":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodGet, ipc.PathNetworks, nil, &ipc.NetworksResponse{})
	case "select-network":
		fs := flag.NewFlagSet("service select-network", flag.ExitOnError)
		ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
		timeoutValue := fs.String("timeout", "30s", "maximum time to wait for service IPC")
		networkID := fs.String("network-id", "", "network ID or name to select")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*networkID) == "" {
			remaining := fs.Args()
			if len(remaining) > 0 {
				*networkID = remaining[0]
			}
		}
		if strings.TrimSpace(*networkID) == "" {
			return fmt.Errorf("network-id is required")
		}
		timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var payload ipc.SelectNetworkResponse
		if err := newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket).Request(ctx, http.MethodPost, ipc.PathSelectNetwork, ipc.SelectNetworkRequest{NetworkID: strings.TrimSpace(*networkID)}, &payload); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(payload)
	case "diagnostics":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodGet, ipc.PathDiagnostics, nil, &ipc.DiagnosticsResponse{})
	case "diagnostics-bundle":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodPost, ipc.PathDiagnosticsBundle, ipc.DiagnosticsBundleRequest{}, &ipc.DiagnosticsBundleResponse{})
	case "logs-recent":
		return cmdServiceIPCRequest(args[0], args[1:], http.MethodGet, ipc.PathRecentLogs, nil, &ipc.RecentLogsResponse{})
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

func cmdServiceIPCRequest(command string, args []string, method, path string, request, response any) error {
	fs := flag.NewFlagSet("service "+command, flag.ExitOnError)
	ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
	timeoutValue := fs.String("timeout", "30s", "maximum time to wait for service IPC")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", command)
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if response == nil {
		return errors.New("service IPC response DTO is required")
	}
	if err := newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket).Request(ctx, method, path, request, response); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(response)
}

func cmdServiceIPCEvents(command string, args []string) error {
	fs := flag.NewFlagSet("service "+command, flag.ExitOnError)
	ipcPipe, ipcSocket := serviceIPCTransportFlags(fs)
	timeoutValue := fs.String("timeout", "30s", "maximum time to listen for service IPC events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", command)
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	encoder := json.NewEncoder(os.Stdout)
	err = newServiceIPCClientForLocalTransport(*ipcPipe, *ipcSocket).Stream(ctx, http.MethodGet, ipc.PathEvents, nil, func(event ipc.Event) error {
		return encoder.Encode(event)
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func serviceIPCTransportFlags(fs *flag.FlagSet) (*string, *string) {
	ipcPipe := fs.String("ipc-pipe", "", "Windows named pipe used for local service IPC")
	ipcSocket := fs.String("ipc-socket", "", "Unix domain socket used for local service IPC")
	return ipcPipe, ipcSocket
}

func newServiceIPCClientForLocalTransport(ipcPipe, ipcSocket string) *ipc.Client {
	if strings.TrimSpace(ipcSocket) != "" {
		return newServiceIPCClient(ipcSocket)
	}
	if strings.TrimSpace(ipcPipe) != "" {
		return newServiceIPCClient(ipcPipe)
	}
	if runtime.GOOS == "windows" {
		return newServiceIPCClient(ipc.DefaultWindowsPipe)
	}
	return newServiceIPCClient("")
}

func parsePositiveServiceIPCTimeout(value string) (time.Duration, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("timeout must be a positive duration")
	}
	return timeout, nil
}

func serviceEnrollViaIPC(ctx context.Context, ipcClient *ipc.Client, token, serverURL, mode, hostname, idempotencyKey string) (ipc.EnrollResponse, error) {
	req := ipc.EnrollRequest{
		EnrollToken:    strings.TrimSpace(token),
		Server:         strings.TrimSpace(serverURL),
		Mode:           strings.TrimSpace(mode),
		Hostname:       strings.TrimSpace(hostname),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	}
	var payload ipc.EnrollResponse
	if err := ipcClient.Request(ctx, http.MethodPost, ipc.PathEnroll, req, &payload); err != nil {
		return ipc.EnrollResponse{}, err
	}
	return payload, nil
}

func serviceEnrollViaIPCWithRetry(ctx context.Context, ipcClient *ipc.Client, token, serverURL, mode, hostname, idempotencyKey string) (ipc.EnrollResponse, error) {
	var lastErr error
	for {
		payload, err := serviceEnrollViaIPC(ctx, ipcClient, token, serverURL, mode, hostname, idempotencyKey)
		if err == nil {
			return payload, nil
		}
		var ipcErr ipc.Error
		if errors.As(err, &ipcErr) {
			return ipc.EnrollResponse{}, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return ipc.EnrollResponse{}, fmt.Errorf("service enrollment IPC unavailable before timeout: %w", lastErr)
			}
			return ipc.EnrollResponse{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
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
	Pipe             string
	UnixSocket       string
	ConfigPath       string
	StateOutput      string
	ConfigStore      *client.ConfigStore
	OperationMu      *sync.Mutex
	DiagnosticsDir   string
	DiagnosticsStore *diagnosticsStore
	RecentLogs       *recentLogBuffer
	ListenPort       int
	WGInterface      string
	Timeout          time.Duration
	WireGuard        agentWireGuard
	SyncForConnect   func() error
	SyncWake         chan struct{}
	Now              func() time.Time
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
		log.Printf("invalidate stale agent state after successful connection: %v", err)
	}
}

type agentWireGuard interface {
	Configure(context.Context, client.Config, clientapi.RegisterNodeResponse) (client.WireGuardApplyResult, error)
	Down(context.Context) (client.WireGuardApplyResult, error)
	LastEndpointDiscovery() client.WireGuardEngineEndpointDiscovery
	STUNSnapshot(context.Context, clientapi.RegisterNodeResponse, time.Duration) client.AgentSTUNSnapshot
	Inspection() client.WireGuardInspection
	PathStatus() []client.PeerPathStatus
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

func claimAgentServiceIPCOwner(ctx context.Context, opts agentIPCOptions) (bool, error) {
	if _, ok := client.ServiceIPCPeerFromContext(ctx); !ok {
		return false, nil
	}
	store, err := agentServiceIPCConfigStore(opts)
	if err != nil {
		return false, serviceIPCConfigError(err)
	}
	claimed, err := client.ClaimLocalServiceIPCOwner(ctx, store)
	if err != nil {
		var ipcErr ipc.Error
		if errors.As(err, &ipcErr) {
			return false, ipcErr
		}
		return false, ipc.NewError(http.StatusInternalServerError, "local_owner_update_failed", err)
	}
	return claimed, nil
}

func releaseAgentServiceIPCOwnerClaim(ctx context.Context, opts agentIPCOptions) error {
	store, err := agentServiceIPCConfigStore(opts)
	if err != nil {
		return serviceIPCConfigError(err)
	}
	if err := client.ReleaseLocalServiceIPCOwnerClaim(ctx, store); err != nil {
		return ipc.NewError(http.StatusInternalServerError, "local_owner_update_failed", err)
	}
	return nil
}

func agentIPCEnrollmentServer(opts agentIPCOptions, requested string) (string, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested, nil
	}
	store, err := agentServiceIPCConfigStore(opts)
	if err != nil {
		return "", serviceIPCConfigError(err)
	}
	if configured := firstControlPlaneURL(store.Read()); configured != "" {
		return configured, nil
	}
	return defaultPublicServerURL, nil
}

func serviceIPCMetadata() ipc.Metadata {
	return ipc.Metadata{
		IPCProtocol:      ipc.Protocol,
		IPCVersion:       ipc.Version,
		IPCMinSupported:  ipc.MinSupportedVersion,
		ServiceVersion:   version,
		ServiceCommit:    commit,
		ServiceBuildDate: buildDate,
	}
}

func wireGuardApplyResult(result client.WireGuardApplyResult) ipc.WireGuardApplyResult {
	return ipc.WireGuardApplyResult{
		OK: result.OK, Method: result.Method, Interface: result.Interface, Changed: result.Changed,
		Skipped: result.Skipped, Reason: result.Reason, DownError: result.DownError,
		UpError: result.UpError, SyncError: result.SyncError, RouteError: result.RouteError,
	}
}

func wireGuardInspection(inspection client.WireGuardInspection) ipc.WireGuardInspection {
	peers := make([]ipc.WireGuardPeerInspection, 0, len(inspection.Peers))
	for _, peer := range inspection.Peers {
		peers = append(peers, ipc.WireGuardPeerInspection{
			PublicKey: peer.PublicKey, Endpoint: peer.Endpoint, AllowedIPs: append([]string(nil), peer.AllowedIPs...),
			LatestHandshakeUnix: peer.LatestHandshakeUnix, TransferRXBytes: peer.TransferRXBytes,
			TransferTXBytes: peer.TransferTXBytes, PersistentKeepaliveSeconds: peer.PersistentKeepaliveSeconds,
		})
	}
	routes := make([]ipc.WireGuardRouteInspection, 0, len(inspection.Routes))
	for _, route := range inspection.Routes {
		routes = append(routes, ipc.WireGuardRouteInspection{
			Target: route.Target, Interface: route.Interface, UsesInterface: route.UsesInterface, Error: route.Error,
		})
	}
	return ipc.WireGuardInspection{
		OK: inspection.OK, Interface: inspection.Interface, MTU: inspection.MTU,
		ListenPort: inspection.ListenPort, PeerCount: inspection.PeerCount,
		Peers: peers, Routes: routes, Error: inspection.Error,
	}
}

func networkInterfaceStatuses(statuses []client.NetworkInterfaceStatus) []ipc.NetworkInterfaceStatus {
	out := make([]ipc.NetworkInterfaceStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, ipc.NetworkInterfaceStatus{
			Name: status.Name, Index: status.Index, MTU: status.MTU,
			Flags: append([]string(nil), status.Flags...), AddressCount: status.AddressCount,
			Addresses: append([]string(nil), status.Addresses...), Prefixes: append([]string(nil), status.Prefixes...),
			Error: status.Error,
		})
	}
	return out
}

func overlayCIDRConflicts(conflicts []client.OverlayCIDRConflict) []ipc.OverlayCIDRConflict {
	out := make([]ipc.OverlayCIDRConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		out = append(out, ipc.OverlayCIDRConflict{
			OverlayCIDR: conflict.OverlayCIDR, LocalPrefix: conflict.LocalPrefix,
			Interface: conflict.Interface, AddressFamily: conflict.AddressFamily, Reason: conflict.Reason,
		})
	}
	return out
}

func pathCandidateStatus(status client.PathCandidateStatus) ipc.PathCandidateStatus {
	return ipc.PathCandidateStatus{
		Type: status.Type, Tier: status.Tier, Priority: status.Priority, State: status.State,
		Endpoint: status.Endpoint, RelayID: status.RelayID, Protocol: status.Protocol, RTTMS: status.RTTMS,
		CheckedAt: status.CheckedAt, LastReachableAt: status.LastReachableAt,
		ConsecutiveFailures: status.ConsecutiveFailures, Reason: status.Reason,
	}
}

func peerPathStatuses(statuses []client.PeerPathStatus) []ipc.PeerPathStatus {
	out := make([]ipc.PeerPathStatus, 0, len(statuses))
	for _, status := range statuses {
		candidates := make([]ipc.PathCandidateStatus, 0, len(status.Candidates))
		for _, candidate := range status.Candidates {
			candidates = append(candidates, pathCandidateStatus(candidate))
		}
		out = append(out, ipc.PeerPathStatus{
			PeerID: status.PeerID, Hostname: status.Hostname, Direct: pathCandidateStatus(status.Direct),
			Candidates: candidates, Relay: pathCandidateStatus(status.Relay), SelectedPath: status.SelectedPath,
			SelectedEndpoint: status.SelectedEndpoint, LastTransitionAt: status.LastTransitionAt,
			SelectionReason: status.SelectionReason,
		})
	}
	return out
}

func serviceIPCEvent(eventType ipc.EventType, sequence int) ipc.Event {
	return ipc.Event{
		Metadata:    serviceIPCMetadata(),
		EventType:   eventType,
		Sequence:    sequence,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func serviceIPCErrorEvent(sequence int, err error) ipc.Event {
	code := ipc.ErrorRequestFailed
	var ipcErr ipc.Error
	if errors.As(err, &ipcErr) {
		code = ipcErr.Code
	}
	event := serviceIPCEvent(ipc.EventTypeError, sequence)
	event.ErrorCode = code
	event.Error = redactDiagnosticsStringForJSON(err.Error())
	return event
}

func applyAgentConnectionIntentStatus(payload *ipc.StatusResponse, intent client.ConnectionIntent) {
	switch payload.ControlState {
	case ipc.ControlStateCacheInvalid, ipc.ControlStateError:
		return
	}
	payload.DesiredState = ipc.DesiredDisconnected
	payload.UserDisconnected = true
	payload.ConnectionIntent = &ipc.ConnectionIntentStatus{
		DesiredState: ipc.DesiredDisconnected,
		Reason:       intent.Reason,
		UpdatedAt:    intent.UpdatedAt,
	}
	payload.ControlState = ipc.ControlStateDisconnected
	if payload.Agent != nil {
		payload.Agent.ConnectionPaused = true
	}
}

func startAgentIPC(ctx context.Context, opts agentIPCOptions) (func(), error) {
	if opts.DiagnosticsStore == nil && strings.TrimSpace(opts.DiagnosticsDir) != "" {
		opts.DiagnosticsStore = newDiagnosticsStore(opts.DiagnosticsDir)
	}
	if opts.DiagnosticsStore != nil {
		if err := opts.DiagnosticsStore.Prune(); err != nil {
			return nil, fmt.Errorf("prune diagnostics bundle store: %w", err)
		}
	}
	stops := []func(){}
	if pipe := strings.TrimSpace(opts.Pipe); pipe != "" {
		listener, err := client.ListenWindowsServicePipe(pipe)
		if err != nil {
			return nil, err
		}
		stop, err := startAgentIPCListener(ctx, listener, client.WindowsServiceIPCConnContext, opts)
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
		stops = append(stops, stop)
	}
	if socket := strings.TrimSpace(opts.UnixSocket); socket != "" {
		// The Windows stub always returns an error, while Linux and macOS use the
		// real listener implementation selected by build tags.
		listener, err := client.ListenUnixServiceSocket(socket) //nolint:staticcheck // Linux and macOS return dynamic errors.
		if err != nil {                                         //nolint:staticcheck // Real implementations can fail dynamically.
			for _, stop := range stops {
				stop()
			}
			return nil, err
		}
		stop, err := startAgentIPCListener(ctx, listener, client.UnixServiceIPCConnContext, opts)
		if err != nil {
			_ = listener.Close()
			for _, stop := range stops {
				stop()
			}
			return nil, err
		}
		stops = append(stops, stop)
	}
	if len(stops) == 0 {
		return func() {}, nil
	}
	return func() {
		for _, stop := range stops {
			stop()
		}
	}, nil
}

func startAgentIPCListener(ctx context.Context, listener net.Listener, connContext func(context.Context, net.Conn) context.Context, opts agentIPCOptions) (func(), error) {
	if listener == nil {
		return nil, errors.New("service IPC listener is nil")
	}
	handler := client.NewServiceIPCHandler(agentIPCHandlers(opts))
	server := client.NewLocalHTTPServer(handler)
	server.ConnContext = connContext
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("service ipc stopped: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	return func() {
		_ = server.Close()
		<-done
	}, nil
}

func connectAgentTunnel(ctx context.Context, opts agentIPCOptions) (ipc.ConnectResponse, error) {
	cfg, err := client.LoadConfig(opts.ConfigPath)
	if err != nil {
		return ipc.ConnectResponse{}, serviceIPCConfigError(err)
	}
	if strings.EqualFold(strings.TrimSpace(cfg.NodeApprovalState), clientapi.NodeApprovalPending) {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusConflict, "approval_required", errors.New("node enrollment is pending approval"))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.NodeApprovalState), clientapi.NodeApprovalRejected) {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusForbidden, "approval_rejected", errors.New("node enrollment was rejected"))
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" || strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusConflict, "node_identity_missing", errors.New("node identity is missing; enroll this device first"))
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusConflict, "device_fingerprint_mismatch", err)
	}
	if opts.WireGuard == nil {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusInternalServerError, "wireguard_unavailable", errors.New("wireguard-go engine is not initialized"))
	}
	networkMap, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusConflict, cliErrorNetworkMapUnavailable, err)
	}
	result, err := opts.WireGuard.Configure(ctx, cfg, networkMap)
	if err != nil {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusInternalServerError, "connect_failed", err)
	}
	if !result.OK {
		return ipc.ConnectResponse{}, ipc.NewError(http.StatusInternalServerError, "connect_failed", errors.New("wireguard-go configuration did not report success"))
	}
	return ipc.ConnectResponse{
		Metadata:     serviceIPCMetadata(),
		State:        ipc.StateConnected,
		DesiredState: ipc.DesiredConnected,
		NodeID:       cfg.NodeID,
		NetworkID:    cfg.NetworkID,
		MapRevision:  cfg.MapRevision,
		WireGuard:    wireGuardApplyResult(result),
	}, nil
}

func enrollmentStatusAfterConnect(ctx context.Context, opts agentIPCOptions, connected ipc.ConnectResponse) ipc.StatusResponse {
	status, err := agentIPCStatus(ctx, opts)
	if err == nil {
		return status
	}
	return ipc.StatusResponse{
		Metadata:        serviceIPCMetadata(),
		State:           ipc.StateError,
		ControlState:    ipc.ControlStateError,
		DesiredState:    connected.DesiredState,
		NodeID:          connected.NodeID,
		NetworkID:       connected.NetworkID,
		MapRevision:     connected.MapRevision,
		LocalStateError: err.Error(),
	}
}

func downAgentWireGuard(ctx context.Context, opts agentIPCOptions) (client.WireGuardApplyResult, error) {
	if opts.WireGuard == nil {
		return client.WireGuardApplyResult{Method: "wireguard-go"}, errors.New("wireguard-go engine is not initialized")
	}
	return opts.WireGuard.Down(ctx)
}

const serverMapSigningTrustChangedError = "server map signing trust changed"

func agentFailureIsServerIdentityChange(statePath string) bool {
	if strings.TrimSpace(statePath) == "" {
		return false
	}
	snapshot, err := client.LoadAgentSnapshot(statePath)
	return err == nil && strings.Contains(strings.ToLower(snapshot.LastError), serverMapSigningTrustChangedError)
}

func syncAgentForConnect(opts agentIPCOptions) error {
	if opts.SyncForConnect != nil {
		return opts.SyncForConnect()
	}
	if cfg, err := client.LoadConfig(opts.ConfigPath); err != nil {
		return err
	} else if cfg.EnrollmentRecovery != nil {
		if opts.WireGuard != nil {
			if _, err := downAgentWireGuard(context.Background(), opts); err != nil {
				return err
			}
		}
		progress, attemptErr := continueEnrollmentRecovery(context.Background(), opts.ConfigPath)
		if progress.Completed {
			return nil
		}
		if attemptErr != nil {
			return fmt.Errorf("recovery %s", firstNonEmpty(progress.ErrorCode, recoveryErrorProtocol))
		}
		return fmt.Errorf("recovery %s", firstNonEmpty(progress.ErrorCode, recoveryErrorProtocol))
	}
	args := []string{"--config", opts.ConfigPath}
	return cmdUp(args)
}

func forgetAgentEnrollmentLocally(ctx context.Context, opts agentIPCOptions) error {
	if opts.WireGuard != nil {
		if _, err := downAgentWireGuard(ctx, opts); err != nil {
			return fmt.Errorf("tear down WireGuard tunnel before local forget: %w", err)
		}
	}
	store, err := agentServiceIPCConfigStore(opts)
	if err != nil {
		return err
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	if err := store.Update(func(cfg *client.Config) error {
		return client.ApplyLocalLogoutCleanup(cfg, now)
	}); err != nil {
		return err
	}
	if statePath := strings.TrimSpace(opts.StateOutput); statePath != "" {
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale agent state: %w", err)
		}
	}
	return nil
}

func inspectServerIdentity(configPath string) (ipc.ServerIdentityResponse, clientapi.SigningTrustBundle, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return ipc.ServerIdentityResponse{}, clientapi.SigningTrustBundle{}, err
	}
	if err := validateMapSigningEnrollmentURLs(cfg); err != nil {
		return ipc.ServerIdentityResponse{}, clientapi.SigningTrustBundle{}, err
	}
	trusted, err := client.SigningTrustBundle(cfg)
	if err != nil {
		return ipc.ServerIdentityResponse{}, clientapi.SigningTrustBundle{}, err
	}
	serverKey, err := apiFromConfig(cfg).ServerKey()
	if err != nil {
		return ipc.ServerIdentityResponse{}, clientapi.SigningTrustBundle{}, fmt.Errorf("fetch server signing trust bundle: %w", err)
	}
	announced := serverKey.TrustBundle
	if err := announced.Validate(); err != nil {
		return ipc.ServerIdentityResponse{}, clientapi.SigningTrustBundle{}, fmt.Errorf("invalid server signing trust bundle: %w", err)
	}
	return ipc.ServerIdentityResponse{
		Metadata:       serviceIPCMetadata(),
		ControlOrigin:  firstControlPlaneURL(cfg),
		TrustedKeyID:   trusted.ActiveKeyID,
		AnnouncedKeyID: announced.ActiveKeyID,
		Changed:        trusted.ActiveKeyID != announced.ActiveKeyID,
	}, announced, nil
}

func agentIPCHandlers(opts agentIPCOptions) client.ServiceIPCHandlers {
	store := opts.DiagnosticsStore
	if store == nil && strings.TrimSpace(opts.DiagnosticsDir) != "" {
		store = newDiagnosticsStore(opts.DiagnosticsDir)
	}
	return client.ServiceIPCHandlers{
		Authorize: func(r *http.Request, endpoint client.ServiceIPCEndpoint) error {
			configStore, err := agentServiceIPCConfigStore(opts)
			if err != nil {
				return serviceIPCConfigError(err)
			}
			return client.AuthorizeLocalServiceIPCForConfig(r, endpoint, configStore.Read())
		},
		MutationLock: opts.OperationMu,
		Status: func(ctx context.Context, req ipc.StatusRequest) (ipc.StatusResponse, error) {
			return agentIPCStatus(ctx, opts)
		},
		Events: func(ctx context.Context, req ipc.EventsRequest, writer client.ServiceIPCEventWriter) error {
			return streamAgentIPCEvents(ctx, opts, writer)
		},
		Enroll: func(ctx context.Context, req ipc.EnrollRequest) (ipc.EnrollResponse, error) {
			ownerClaimed, err := claimAgentServiceIPCOwner(ctx, opts)
			if err != nil {
				return ipc.EnrollResponse{}, err
			}
			token := strings.TrimSpace(req.EnrollToken)
			serverURL, serverErr := agentIPCEnrollmentServer(opts, req.Server)
			if serverErr != nil {
				return ipc.EnrollResponse{}, serverErr
			}
			args := []string{"--config", opts.ConfigPath}
			if strings.TrimSpace(token) != "" {
				args = append(args, "--join-token", token)
			} else {
				args = append(args, "--approval-timeout", "0")
			}
			args = append(args, "--server", serverURL)
			if mode := strings.TrimSpace(req.Mode); mode != "" {
				args = append(args, "--tag", "mode:"+mode)
			}
			if hostname := strings.TrimSpace(req.Hostname); hostname != "" {
				args = append(args, "--hostname", hostname)
			}
			if idempotencyKey := strings.TrimSpace(req.IdempotencyKey); idempotencyKey != "" {
				args = append(args, "--idempotency-key", idempotencyKey)
			}
			if err := cmdUp(args); err != nil {
				var approvalRequired enrollmentApprovalRequiredError
				if strings.TrimSpace(token) == "" && errors.As(err, &approvalRequired) {
					if intentErr := agentConnectionIntentStore(opts).Clear(); intentErr != nil {
						return ipc.EnrollResponse{}, ipc.NewError(http.StatusInternalServerError, "connection_intent_update_failed", intentErr)
					}
					payload, statusErr := agentIPCStatus(ctx, opts)
					if statusErr != nil {
						return ipc.EnrollResponse{}, statusErr
					}
					response := ipc.EnrollResponse{StatusResponse: payload}
					if requestID := strings.TrimSpace(approvalRequired.RequestID); requestID != "" {
						response.EnrollmentRequestID = requestID
					}
					if approvalURL := strings.TrimSpace(approvalRequired.ApprovalURL); approvalURL != "" {
						response.ApprovalURL = approvalURL
					}
					requestAgentSync(opts)
					return response, nil
				}
				if ownerClaimed {
					if releaseErr := releaseAgentServiceIPCOwnerClaim(ctx, opts); releaseErr != nil {
						return ipc.EnrollResponse{}, releaseErr
					}
				}
				return ipc.EnrollResponse{}, ipc.NewError(http.StatusBadRequest, "enrollment_failed", err)
			}
			if err := agentConnectionIntentStore(opts).Clear(); err != nil {
				return ipc.EnrollResponse{}, ipc.NewError(http.StatusInternalServerError, "connection_intent_update_failed", err)
			}
			if cfg, err := client.LoadConfig(opts.ConfigPath); err == nil && strings.EqualFold(strings.TrimSpace(cfg.NodeApprovalState), clientapi.NodeApprovalPending) {
				payload, err := agentIPCStatus(ctx, opts)
				if err != nil {
					return ipc.EnrollResponse{}, err
				}
				requestAgentSync(opts)
				return ipc.EnrollResponse{StatusResponse: payload}, nil
			}
			connectPayload, err := connectAgentTunnel(ctx, opts)
			if err != nil {
				return ipc.EnrollResponse{}, err
			}
			invalidateAgentSnapshot(opts)
			payload := enrollmentStatusAfterConnect(ctx, opts, connectPayload)
			requestAgentSync(opts)
			apply := connectPayload.WireGuard
			return ipc.EnrollResponse{
				StatusResponse: payload,
				WireGuardApply: &apply,
			}, nil
		},
		Connect: func(ctx context.Context, req ipc.ConnectRequest) (ipc.ConnectResponse, error) {
			if agentFailureIsServerIdentityChange(opts.StateOutput) {
				return ipc.ConnectResponse{}, ipc.NewError(http.StatusConflict, "server_identity_changed", errors.New("server signing identity changed; inspect and explicitly trust the new server identity before connecting"))
			}
			if err := agentConnectionIntentStore(opts).Clear(); err != nil {
				return ipc.ConnectResponse{}, ipc.NewError(http.StatusInternalServerError, "connection_intent_update_failed", err)
			}
			if err := syncAgentForConnect(opts); err != nil {
				if status, statusErr := agentIPCStatus(ctx, opts); statusErr == nil && status.Recovery != nil {
					return connectResponseFromStatus(status), nil
				}
				return ipc.ConnectResponse{}, ipc.NewError(http.StatusBadGateway, "connect_sync_failed", err)
			}
			if cfg, loadErr := client.LoadConfig(opts.ConfigPath); loadErr == nil && strings.TrimSpace(cfg.NodeCredential) == "" {
				status, statusErr := agentIPCStatus(ctx, opts)
				if statusErr != nil {
					return ipc.ConnectResponse{}, statusErr
				}
				return connectResponseFromStatus(status), nil
			}
			payload, err := connectAgentTunnel(ctx, opts)
			if err != nil {
				return ipc.ConnectResponse{}, err
			}
			invalidateAgentSnapshot(opts)
			payload.UserDisconnected = false
			payload.DesiredState = ipc.DesiredConnected
			requestAgentSync(opts)
			return payload, nil
		},
		ServerIdentity: func(ctx context.Context, req ipc.ServerIdentityRequest) (ipc.ServerIdentityResponse, error) {
			payload, _, err := inspectServerIdentity(opts.ConfigPath)
			if err != nil {
				return ipc.ServerIdentityResponse{}, ipc.NewError(http.StatusBadGateway, "server_identity_unavailable", err)
			}
			return payload, nil
		},
		TrustServer: func(ctx context.Context, req ipc.TrustServerRequest) (ipc.TrustServerResponse, error) {
			identity, announced, err := inspectServerIdentity(opts.ConfigPath)
			if err != nil {
				return ipc.TrustServerResponse{}, ipc.NewError(http.StatusBadGateway, "server_identity_unavailable", err)
			}
			confirmedKeyID := strings.TrimSpace(req.ConfirmedKeyID)
			announcedKeyID := strings.TrimSpace(identity.AnnouncedKeyID)
			confirmedOrigin := strings.TrimSpace(req.ConfirmedControlOrigin)
			if confirmedOrigin == "" || confirmedOrigin != strings.TrimSpace(identity.ControlOrigin) || confirmedKeyID == "" || confirmedKeyID != announcedKeyID {
				return ipc.TrustServerResponse{}, ipc.NewError(http.StatusConflict, "server_identity_confirmation_mismatch", errors.New("confirmed server signing key ID does not match the currently announced key"))
			}
			candidateOperationID, err := clientapi.NewCreateIdempotencyKey()
			if err != nil {
				return ipc.TrustServerResponse{}, ipc.NewError(http.StatusInternalServerError, ipc.ErrorRecoveryOperationIDFailed, err)
			}
			candidateIdempotencyID, err := clientapiv2.NewRegistrationIdempotencyID()
			if err != nil {
				return ipc.TrustServerResponse{}, ipc.NewError(http.StatusInternalServerError, ipc.ErrorRecoveryOperationIDFailed, err)
			}
			configStore, err := agentServiceIPCConfigStore(opts)
			if err != nil {
				return ipc.TrustServerResponse{}, serviceIPCConfigError(err)
			}
			operationID := candidateOperationID
			outcome := ipc.RecoveryOutcomeAccepted
			needsRecovery := false
			now := time.Now()
			if opts.Now != nil {
				now = opts.Now()
			}
			if err := configStore.Update(func(current *client.Config) error {
				if strings.TrimSpace(firstControlPlaneURL(*current)) != confirmedOrigin {
					return errors.New("confirmed control origin is no longer current")
				}
				if current.EnrollmentRecovery != nil {
					recovery := current.EnrollmentRecovery
					if recovery.ConfirmedControlOrigin != confirmedOrigin || recovery.ConfirmedKeyID != confirmedKeyID {
						return errors.New("a different recovery operation is already active")
					}
					operationID = recovery.OperationID
					outcome = ipc.RecoveryOutcomeAlreadyApplied
					needsRecovery = true
					return nil
				}
				trusted, trustErr := client.SigningTrustBundle(*current)
				if trustErr == nil && trusted.ActiveKeyID == announced.ActiveKeyID {
					outcome = ipc.RecoveryOutcomeAlreadyApplied
					return nil
				}
				if err := client.ReplaceSigningTrustBundle(current, announced); err != nil {
					return err
				}
				if strings.TrimSpace(current.NodeCredential) == "" {
					return nil
				}
				recovery, err := client.NewEnrollmentRecovery(candidateOperationID, candidateIdempotencyID, confirmedOrigin, confirmedKeyID, now)
				if err != nil {
					return err
				}
				current.EnrollmentRecovery = &recovery
				needsRecovery = true
				return nil
			}); err != nil {
				return ipc.TrustServerResponse{}, ipc.NewError(http.StatusInternalServerError, "server_identity_update_failed", err)
			}
			progress := recoveryAttemptResult{Completed: true}
			if needsRecovery {
				if opts.WireGuard != nil {
					if _, downErr := downAgentWireGuard(ctx, opts); downErr != nil {
						return ipc.TrustServerResponse{}, ipc.NewError(http.StatusInternalServerError, "server_identity_recovery_failed", downErr)
					}
				}
				progress, err = continueEnrollmentRecovery(ctx, opts.ConfigPath)
				if err != nil && progress.OperationID == "" {
					return ipc.TrustServerResponse{}, ipc.NewError(http.StatusInternalServerError, "server_identity_recovery_failed", err)
				}
			}
			if progress.Completed && !progress.Terminal {
				if cfg := configStore.Read(); strings.TrimSpace(cfg.NodeCredential) != "" {
					if _, connectErr := connectAgentTunnel(ctx, opts); connectErr != nil {
						return ipc.TrustServerResponse{}, connectErr
					}
					invalidateAgentSnapshot(opts)
				}
			}
			if progress.Terminal {
				invalidateAgentSnapshot(opts)
			}
			status, err := agentIPCStatus(ctx, opts)
			if err != nil {
				return ipc.TrustServerResponse{}, err
			}
			requestAgentSync(opts)
			return ipc.TrustServerResponse{
				Metadata:     serviceIPCMetadata(),
				OperationID:  operationID,
				Operation:    ipc.RecoveryOperationTrustServerIdentity,
				Outcome:      outcome,
				State:        status.State,
				ControlState: status.ControlState,
				TrustedKeyID: announcedKeyID,
			}, nil
		},
		Disconnect: func(ctx context.Context, req ipc.DisconnectRequest) (ipc.DisconnectResponse, error) {
			if err := agentConnectionIntentStore(opts).SetDisconnected("user_disconnect"); err != nil {
				return ipc.DisconnectResponse{}, ipc.NewError(http.StatusInternalServerError, "connection_intent_update_failed", err)
			}
			markNodeOfflineConfigBestEffort(opts.ConfigPath)
			result, err := downAgentWireGuard(ctx, opts)
			if err != nil {
				return ipc.DisconnectResponse{}, ipc.NewError(http.StatusInternalServerError, "disconnect_failed", err)
			}
			return ipc.DisconnectResponse{
				Metadata:         serviceIPCMetadata(),
				State:            ipc.StateDisconnected,
				DesiredState:     ipc.DesiredDisconnected,
				UserDisconnected: true,
				WireGuard:        wireGuardApplyResult(result),
			}, nil
		},
		Logout: func(ctx context.Context, req ipc.LogoutRequest) (ipc.LogoutResponse, error) {
			if _, err := downAgentWireGuard(ctx, opts); err != nil {
				return ipc.LogoutResponse{}, ipc.NewError(http.StatusInternalServerError, "logout_failed", err)
			}
			args := []string{"--config", opts.ConfigPath}
			if strings.TrimSpace(opts.StateOutput) != "" {
				args = append(args, "--state-output", opts.StateOutput)
			}
			if err := cmdLogout(args); err != nil {
				var remoteErr remoteCleanupError
				if errors.As(err, &remoteErr) {
					return ipc.LogoutResponse{}, ipc.NewErrorWithRequestID(http.StatusConflict, ipc.ErrorRemoteCleanupRequired, remoteErr.RequestID, err)
				}
				return ipc.LogoutResponse{}, ipc.NewError(http.StatusInternalServerError, "logout_failed", err)
			}
			return ipc.LogoutResponse{
				Metadata:     serviceIPCMetadata(),
				State:        ipc.StateNeedsEnrollment,
				ControlState: ipc.ControlStateNotRegistered,
				Outcome:      ipc.LogoutOutcomeRemoteCleanupConfirmed,
			}, nil
		},
		LocalForget: func(ctx context.Context, req ipc.LocalForgetRequest) (ipc.LocalForgetResponse, error) {
			if !req.Confirmed {
				return ipc.LocalForgetResponse{}, ipc.NewError(http.StatusBadRequest, ipc.ErrorLocalForgetConfirmationRequired, errors.New("explicit local-forget confirmation is required"))
			}
			if err := forgetAgentEnrollmentLocally(ctx, opts); err != nil {
				return ipc.LocalForgetResponse{}, ipc.NewError(http.StatusInternalServerError, ipc.ErrorLocalForgetFailed, err)
			}
			return ipc.LocalForgetResponse{
				Metadata:     serviceIPCMetadata(),
				State:        ipc.StateNeedsEnrollment,
				ControlState: ipc.ControlStateNotRegistered,
				Outcome:      ipc.LogoutOutcomeRemoteCleanupUnconfirmed,
			}, nil
		},
		Networks: func(ctx context.Context, req ipc.NetworksRequest) (ipc.NetworksResponse, error) {
			cfg, err := client.LoadConfig(opts.ConfigPath)
			if err != nil {
				return ipc.NetworksResponse{}, serviceIPCConfigError(err)
			}
			if cfg.CachedMap == nil {
				return ipc.NetworksResponse{Metadata: serviceIPCMetadata(), Networks: []clientapi.Network{}}, nil
			}
			networkMap, err := verifiedCachedNetworkMap(&cfg)
			if err != nil {
				return ipc.NetworksResponse{}, ipc.NewError(http.StatusInternalServerError, cliErrorNetworkMapUnavailable, err)
			}
			return ipc.NetworksResponse{
				Metadata:          serviceIPCMetadata(),
				Networks:          []clientapi.Network{networkMap.Network},
				SelectedNetworkID: networkMap.Network.ID,
			}, nil
		},
		SelectNetwork: func(ctx context.Context, req ipc.SelectNetworkRequest) (ipc.SelectNetworkResponse, error) {
			return selectAgentNetwork(opts, req)
		},
		Diagnostics: func(ctx context.Context, req ipc.DiagnosticsRequest) (ipc.DiagnosticsResponse, error) {
			payload, err := buildServiceIPCDiagnostics(opts, req.LogLimit)
			if err != nil {
				return ipc.DiagnosticsResponse{}, err
			}
			return ipc.DiagnosticsResponse{Metadata: serviceIPCMetadata(), Diagnostics: payload}, nil
		},
		DiagnosticsBundle: func(ctx context.Context, req ipc.DiagnosticsBundleRequest) (ipc.DiagnosticsBundleResponse, error) {
			payload, err := buildServiceIPCDiagnostics(opts, req.LogLimit)
			if err != nil {
				return ipc.DiagnosticsBundleResponse{}, err
			}
			if store == nil {
				return ipc.DiagnosticsBundleResponse{}, ipc.NewError(http.StatusServiceUnavailable, "diagnostics_bundle_unavailable", errors.New("diagnostics bundle directory is not configured"))
			}
			bundle, err := store.Write(payload)
			if err != nil {
				return ipc.DiagnosticsBundleResponse{}, ipc.NewError(http.StatusInternalServerError, "diagnostics_bundle_failed", err)
			}
			return ipc.DiagnosticsBundleResponse{
				Metadata: serviceIPCMetadata(), Path: bundle.Path,
				CreatedAt: bundle.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: bundle.ExpiresAt.Format(time.RFC3339Nano),
				SizeBytes: bundle.SizeBytes, Reused: bundle.Reused,
			}, nil
		},
		RecentLogs: func(ctx context.Context, req ipc.RecentLogsRequest) (ipc.RecentLogsResponse, error) {
			entries := recentLogEntries(opts.RecentLogs, positiveIntOr(req.Limit, 100))
			logs := make([]ipc.LogEntry, 0, len(entries))
			for _, entry := range entries {
				logs = append(logs, ipc.LogEntry{Timestamp: entry.Timestamp, Message: entry.Message})
			}
			return ipc.RecentLogsResponse{Metadata: serviceIPCMetadata(), Logs: logs}, nil
		},
	}
}

func buildServiceIPCDiagnostics(opts agentIPCOptions, logLimit int) (ipc.Diagnostics, error) {
	cfg, err := client.LoadConfig(opts.ConfigPath)
	if err != nil {
		return ipc.Diagnostics{}, serviceIPCConfigError(err)
	}
	agentState := loadAgentSnapshotIfAvailable(opts.StateOutput)
	status := serviceIPCStatusForConfig(cfg)
	if agentState != nil {
		attachServiceIPCAgentStatus(&status, *agentState)
	}
	if opts.WireGuard != nil {
		inspection := opts.WireGuard.Inspection()
		converted := wireGuardInspection(inspection)
		status.WireGuard = &converted
	}
	status.State = serviceStateFromControlState(status.ControlState, status.CachedMapError != "")
	status.Metadata = serviceIPCMetadata()
	payload := serviceIPCDiagnosticsPayload(cfg, status, agentState)
	entries := recentLogEntries(opts.RecentLogs, positiveIntOr(logLimit, 100))
	payload.RecentLogs = make([]ipc.LogEntry, 0, len(entries))
	for _, entry := range entries {
		payload.RecentLogs = append(payload.RecentLogs, ipc.LogEntry{Timestamp: entry.Timestamp, Message: entry.Message})
	}
	interfaces := client.LocalInterfaceStatuses()
	payload.Interfaces = networkInterfaceStatuses(interfaces)
	payload.RouteConflicts = overlayCIDRConflicts(serviceIPCDiagnosticsRouteConflicts(cfg, interfaces, opts.WGInterface))
	payload.RouteConflictCount = len(payload.RouteConflicts)
	payload, err = sanitizeServiceIPCDiagnosticsPayload(payload)
	if err != nil {
		return ipc.Diagnostics{}, ipc.NewError(http.StatusInternalServerError, "diagnostics_snapshot_failed", err)
	}
	return payload, nil
}

func streamAgentIPCEvents(ctx context.Context, opts agentIPCOptions, writer client.ServiceIPCEventWriter) error {
	if writer == nil {
		return errors.New("service IPC event writer is required")
	}
	sequence := 1
	if err := writer.Send(serviceIPCEvent(ipc.EventTypeHello, sequence)); err != nil {
		return err
	}
	sequence++
	lastStatus := ""
	sendStatus := func() error {
		payload, err := agentIPCStatus(ctx, opts)
		if err != nil {
			if sendErr := writer.Send(serviceIPCErrorEvent(sequence, err)); sendErr != nil {
				return sendErr
			}
			sequence++
			return nil
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fingerprint := string(raw)
		if fingerprint == lastStatus {
			return nil
		}
		lastStatus = fingerprint
		event := serviceIPCEvent(ipc.EventTypeStatusChanged, sequence)
		event.Status = &payload
		if err := writer.Send(event); err != nil {
			return err
		}
		sequence++
		return nil
	}
	if err := sendStatus(); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := sendStatus(); err != nil {
				return err
			}
		}
	}
}

func selectAgentNetwork(opts agentIPCOptions, req ipc.SelectNetworkRequest) (ipc.SelectNetworkResponse, error) {
	networkRef := firstNonEmpty(req.NetworkID, req.NetworkName)
	if strings.TrimSpace(networkRef) == "" {
		return ipc.SelectNetworkResponse{}, ipc.NewError(http.StatusBadRequest, "network_ref_required", errors.New("network_id or network_name is required"))
	}
	cfg, err := client.LoadConfig(opts.ConfigPath)
	if err != nil {
		return ipc.SelectNetworkResponse{}, serviceIPCConfigError(err)
	}
	if cfg.CachedMap == nil {
		return ipc.SelectNetworkResponse{}, ipc.NewError(http.StatusConflict, "network_selection_requires_enrollment", errors.New("network selection requires an enrolled device"))
	}
	networkMap, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		return ipc.SelectNetworkResponse{}, ipc.NewError(http.StatusConflict, cliErrorNetworkMapUnavailable, err)
	}
	if networkRef != networkMap.Network.ID && !strings.EqualFold(networkRef, networkMap.Network.Name) {
		return ipc.SelectNetworkResponse{}, ipc.NewError(http.StatusConflict, "network_selection_requires_enrollment", errors.New("switching networks requires a new network-scoped enrollment token"))
	}
	return ipc.SelectNetworkResponse{
		Metadata:          serviceIPCMetadata(),
		State:             ipc.StateConnected,
		DesiredState:      ipc.DesiredConnected,
		SelectedNetworkID: networkMap.Network.ID,
		SelectedNetwork:   networkMap.Network,
		NodeID:            networkMap.Node.ID,
		MapRevision:       networkMap.Network.Revision,
	}, nil
}

func agentIPCStatus(ctx context.Context, opts agentIPCOptions) (ipc.StatusResponse, error) {
	cfg, err := client.LoadConfig(opts.ConfigPath)
	if err != nil {
		return ipc.StatusResponse{}, serviceIPCConfigError(err)
	}
	return agentIPCStatusForConfig(ctx, opts, cfg, loadAgentSnapshotIfAvailable(opts.StateOutput)), nil
}

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

func agentIPCStatusForConfig(ctx context.Context, opts agentIPCOptions, cfg client.Config, agentState *client.AgentSnapshot) ipc.StatusResponse {
	response := serviceIPCStatusForConfig(cfg)
	controlCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	attachServiceIPCControlAvailability(controlCtx, &response, cfg.ControlURLs()...)
	if agentState != nil {
		attachServiceIPCAgentStatus(&response, *agentState)
	}
	if opts.WireGuard != nil {
		inspection := opts.WireGuard.Inspection()
		converted := wireGuardInspection(inspection)
		response.WireGuard = &converted
	}
	response.State = serviceStateFromControlState(response.ControlState, response.CachedMapError != "")
	response.Metadata = serviceIPCMetadata()
	if intent, disconnected, err := agentConnectionIntentStore(opts).Disconnected(); err != nil {
		response.ConnectionIntentError = err.Error()
		response.ControlState = ipc.ControlStateError
		response.State = ipc.StateError
	} else if disconnected {
		applyAgentConnectionIntentStatus(&response, intent)
		response.State = serviceStateFromControlState(response.ControlState, response.CachedMapError != "")
	}
	return response
}

func agentSnapshotMatchesStatus(status ipc.StatusResponse, snapshot client.AgentSnapshot) bool {
	return agentSnapshotIdentityMatchesStatus(status, snapshot) &&
		snapshot.MapRevision == status.MapRevision
}

func agentSnapshotStateForStatus(status ipc.StatusResponse, snapshot client.AgentSnapshot) (ipc.AgentSnapshotState, bool) {
	if agentSnapshotMatchesStatus(status, snapshot) {
		return ipc.AgentSnapshotCurrent, true
	}
	if !agentSnapshotIdentityMatchesStatus(status, snapshot) {
		return ipc.AgentSnapshotAbsent, false
	}
	if snapshot.MapRevision < status.MapRevision {
		return ipc.AgentSnapshotPrevious, true
	}
	return ipc.AgentSnapshotAbsent, false
}

func agentSnapshotIdentityMatchesStatus(status ipc.StatusResponse, snapshot client.AgentSnapshot) bool {
	return strings.TrimSpace(snapshot.NodeID) == strings.TrimSpace(status.NodeID) &&
		strings.TrimSpace(snapshot.NetworkID) == strings.TrimSpace(status.NetworkID)
}

func serviceStateFromControlState(controlState ipc.ControlState, cachedMapInvalid bool) ipc.ServiceState {
	if cachedMapInvalid {
		return ipc.StateError
	}
	switch controlState {
	case ipc.ControlStatePendingApproval:
		return ipc.StateNeedsApproval
	case ipc.ControlStateDegraded, ipc.ControlStateOfflineCache:
		return ipc.StateDegraded
	case ipc.ControlStateServerIdentityChanged:
		return ipc.StateServerIdentityChanged
	case ipc.ControlStateRecovering:
		return ipc.StateRecovering
	case ipc.ControlStateRecoveryBlocked:
		return ipc.StateRecoveryBlocked
	case ipc.ControlStatePolicyBlocked:
		return ipc.StatePolicyBlocked
	case ipc.ControlStateNeedsLogin:
		return ipc.StateNeedsLogin
	case ipc.ControlStateReady, ipc.ControlStateRegistered:
		return ipc.StateConnected
	case ipc.ControlStateCacheInvalid, ipc.ControlStateError:
		return ipc.StateError
	case ipc.ControlStateNotRegistered:
		return ipc.StateNeedsEnrollment
	case ipc.ControlStateDisconnected:
		return ipc.StateDisconnected
	default:
		return ipc.StateDisconnected
	}
}

func positiveIntOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func connectResponseFromStatus(status ipc.StatusResponse) ipc.ConnectResponse {
	return ipc.ConnectResponse{
		Metadata:         status.Metadata,
		State:            status.State,
		ControlState:     status.ControlState,
		DesiredState:     status.DesiredState,
		UserDisconnected: status.UserDisconnected,
		NodeID:           status.NodeID,
		NetworkID:        status.NetworkID,
		MapRevision:      status.MapRevision,
	}
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
