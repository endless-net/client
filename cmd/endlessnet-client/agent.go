package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func agentReconnectDelay(baseDelay, maxDelay time.Duration, consecutiveFailures int, jitterRatio, jitterUnit float64) time.Duration {
	if baseDelay <= 0 {
		baseDelay = time.Second
	}
	if maxDelay <= 0 {
		maxDelay = baseDelay
	}
	if maxDelay < baseDelay {
		maxDelay = baseDelay
	}
	if consecutiveFailures < 1 {
		consecutiveFailures = 1
	}
	delay := baseDelay
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
			break
		}
	}
	if jitterRatio <= 0 {
		return delay
	}
	if jitterRatio > 1 {
		jitterRatio = 1
	}
	if jitterUnit < 0 {
		jitterUnit = 0
	}
	if jitterUnit > 1 {
		jitterUnit = 1
	}
	jittered := delay + time.Duration(float64(delay)*jitterRatio*jitterUnit)
	if jittered > maxDelay {
		return maxDelay
	}
	return jittered
}

func waitForAgentSync(ctx context.Context, delay time.Duration, wake <-chan struct{}) (bool, bool) {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return false, false
	case <-timer.C:
		return true, false
	case <-wake:
		return true, true
	}
}

func randomJitterUnit() float64 {
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return 0
	}
	value := binary.BigEndian.Uint64(raw[:]) >> 11
	return float64(value) / (1 << 53)
}

type endpointUpdateState struct {
	PendingEndpoint     string
	PendingSince        time.Time
	UnconfirmedEndpoint string
}

func (s *endpointUpdateState) NextEndpointUpdate(candidate, knownEndpoint string, now time.Time, debounce time.Duration) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		s.MarkEndpointConfirmed()
		return "", false
	}
	if candidate == strings.TrimSpace(knownEndpoint) {
		s.MarkEndpointConfirmed()
		return "", false
	}
	if debounce <= 0 {
		return candidate, true
	}
	if candidate != s.PendingEndpoint {
		s.PendingEndpoint = candidate
		s.PendingSince = now
		s.UnconfirmedEndpoint = ""
		return "", false
	}
	if now.Sub(s.PendingSince) < debounce {
		return "", false
	}
	return candidate, true
}

func (s *endpointUpdateState) HasUnconfirmedEndpoint(endpoint string) bool {
	return strings.TrimSpace(endpoint) != "" && strings.TrimSpace(endpoint) == s.UnconfirmedEndpoint
}

func (s *endpointUpdateState) MarkEndpointUnconfirmed(endpoint string) {
	s.PendingEndpoint = strings.TrimSpace(endpoint)
	s.UnconfirmedEndpoint = s.PendingEndpoint
}

func (s *endpointUpdateState) MarkEndpointConfirmed() {
	s.PendingEndpoint = ""
	s.PendingSince = time.Time{}
	s.UnconfirmedEndpoint = ""
}

func agentEndpointCandidates(endpoint, endpointFile string, listenPort int) ([]string, bool, error) {
	if strings.TrimSpace(endpointFile) != "" {
		raw, err := os.ReadFile(endpointFile)
		if err != nil {
			return nil, false, err
		}
		endpoint = strings.TrimSpace(string(raw))
	} else {
		endpoint = strings.TrimSpace(endpoint)
	}
	if endpoint != "" {
		return []string{endpoint}, false, nil
	}
	candidates := client.LocalEndpointCandidates(listenPort, client.LocalInterfaceStatuses())
	return candidates, len(candidates) > 0, nil
}

func updatePublishedEndpoint(configPath string, timeout time.Duration, candidates []string, generated bool, ttl time.Duration, state *endpointUpdateState, debounce time.Duration, now time.Time) (bool, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return false, err
	}
	if approvalState := strings.ToLower(strings.TrimSpace(cfg.NodeApprovalState)); approvalState == clientapi.NodeApprovalPending || approvalState == clientapi.NodeApprovalRejected {
		return false, nil
	}
	if len(cfg.ControlURLs()) == 0 {
		return false, fmt.Errorf("server URL is required; run up or login first")
	}
	if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return false, fmt.Errorf("node identity is missing; run up first")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return false, err
	}
	candidates = cleanEndpointCandidates(candidates)
	if len(candidates) == 0 {
		return false, nil
	}
	if state == nil {
		state = &endpointUpdateState{}
	}
	key := strings.Join(candidates, "\n")
	retryUnconfirmed := state.HasUnconfirmedEndpoint(key)
	req, key, refresh, ok, err := endpointUpdateRequest(cfg, candidates, generated, ttl, now, retryUnconfirmed)
	if err != nil {
		return false, err
	}
	if !ok {
		state.MarkEndpointConfirmed()
		return false, nil
	}
	if !refresh && !retryUnconfirmed {
		if _, ok := state.NextEndpointUpdate(key, "", now, debounce); !ok {
			return false, nil
		}
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Timeout = timeout + 5*time.Second
	if err := refreshMapSigningTrust(&cfg, api); err != nil {
		return false, err
	}
	networkMap, changed, err := updatePublishedEndpointRequestFromConfig(&cfg, api, req)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := client.SaveConfig(configPath, cfg); err != nil {
		return false, err
	}
	if !endpointUpdateConfirmed(networkMap.Node, req, time.Now().UTC()) {
		state.MarkEndpointUnconfirmed(key)
		return false, nil
	}
	state.MarkEndpointConfirmed()
	return true, nil
}

func cleanEndpointCandidates(candidates []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func endpointUpdateRequest(cfg client.Config, candidates []string, generated bool, ttl time.Duration, now time.Time, force bool) (clientapi.UpdateNodeEndpointRequest, string, bool, bool, error) {
	endpoint := strings.TrimSpace(candidates[0])
	key := strings.Join(candidates, "\n")
	if !generated {
		if !force && cfg.CachedMap != nil && strings.TrimSpace(cfg.CachedMap.Node.Endpoint) == endpoint {
			return clientapi.UpdateNodeEndpointRequest{}, key, false, false, nil
		}
		return clientapi.UpdateNodeEndpointRequest{Endpoint: endpoint}, key, false, true, nil
	}
	if ttl <= 0 {
		return clientapi.UpdateNodeEndpointRequest{}, key, false, false, fmt.Errorf("endpoint ttl must be positive")
	}
	generation := uint64(1)
	refresh := false
	if cfg.CachedMap != nil {
		node := cfg.CachedMap.Node
		if node.EndpointGeneration > 0 {
			generation = node.EndpointGeneration + 1
		}
		if strings.TrimSpace(node.Endpoint) == endpoint && sameOrderedStrings(node.EndpointCandidates, candidates) {
			if node.EndpointExpiresAt != nil && node.EndpointExpiresAt.After(now.Add(ttl/2)) {
				if !force {
					return clientapi.UpdateNodeEndpointRequest{}, key, false, false, nil
				}
				refresh = true
			}
			if node.EndpointExpiresAt == nil || !node.EndpointExpiresAt.After(now.Add(ttl/2)) {
				refresh = true
			}
		}
	}
	return clientapi.UpdateNodeEndpointRequest{
		Endpoint:   endpoint,
		Generation: generation,
		Candidates: candidates,
		TTL:        ttl.String(),
	}, key, refresh, true, nil
}

func endpointUpdateConfirmed(node clientapi.Node, req clientapi.UpdateNodeEndpointRequest, now time.Time) bool {
	if strings.TrimSpace(node.Endpoint) != strings.TrimSpace(req.Endpoint) {
		return false
	}
	if req.Generation == 0 {
		return node.EndpointGeneration == 0 &&
			len(node.EndpointCandidates) == 0 &&
			node.EndpointExpiresAt == nil
	}
	return node.EndpointGeneration == req.Generation &&
		sameOrderedStrings(node.EndpointCandidates, req.Candidates) &&
		node.EndpointExpiresAt != nil &&
		node.EndpointExpiresAt.After(now)
}

func sameOrderedStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func cmdAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	stateOutput := fs.String("state-output", "", "write agent state JSON to file after each successful sync")
	listenPort := fs.Int("listen-port", 0, "preferred UDP port for wireguard-go; 0 selects one automatically")
	wireGuardMTU := fs.Int("mtu", 0, "WireGuard interface MTU; 0 uses the wireguard-go default (1420)")
	configPath := fs.String("config", "", "client config path")
	diagnosticsDir := fs.String("diagnostics-dir", "", "directory where service IPC writes redacted diagnostics bundles")
	timeoutValue := fs.String("timeout", "5s", "control-plane and relay probe timeout")
	stunTimeoutValue := fs.String("stun-timeout", "2s", "per-STUN endpoint timeout")
	intervalValue := fs.String("interval", "30s", "sync interval when running continuously")
	reconnectMaxDelayValue := fs.String("reconnect-max-delay", "5m", "maximum delay after consecutive control-plane sync failures")
	reconnectJitter := fs.Float64("reconnect-jitter", 0.2, "fractional jitter applied to reconnect backoff; 0 disables jitter")
	endpoint := fs.String("endpoint", "", "published WireGuard endpoint host:port; sent only after debounce when it changes")
	endpointFile := fs.String("endpoint-file", "", "file containing the latest published endpoint candidate")
	endpointUpdateDebounceValue := fs.String("endpoint-update-debounce", "0s", "minimum stable endpoint duration before publishing an endpoint update; 0 publishes verified STUN results immediately")
	endpointTTLValue := fs.String("endpoint-ttl", "2m", "TTL for automatically discovered STUN and LAN endpoint candidates")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	offline := fs.Bool("offline", false, "render and optionally apply from the cached signed network map")
	maxCacheAgeValue := fs.String("max-cache-age", "0s", "maximum accepted cached map age for offline rendering; 0 disables the age check")
	wgInterface := fs.String("wg-interface", "", "wireguard-go interface name; defaults to endlessnet")
	probeRTT := fs.Bool("probe-rtt", false, "probe direct path RTT with ping; requires wg-interface")
	once := fs.Bool("once", false, "run one sync/probe/write iteration and exit")
	windowsService := fs.Bool("windows-service", false, "run the agent under the Windows Service Control Manager")
	ipcPipe := fs.String("ipc-pipe", "", "Windows named pipe used for local service IPC; defaults in windows-service mode")
	ipcSocket := fs.String("ipc-socket", "", "Unix domain socket used for local service IPC")
	eventLogSource := fs.String("event-log-source", client.DefaultWindowsEventLogSource, "Windows Event Log source used in windows-service mode")
	debugMode := fs.Bool("debug", false, "enable maximum debug logging")
	debugLogDir := fs.String("debug-log-dir", client.DefaultDebugLogDir, "debug log directory; supports ~")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *debugMode {
		component := "endlessnet-client"
		if *windowsService {
			component = "endlessnet-client-service"
		}
		debugLog, err := client.ConfigureDebugLogger(component, *debugLogDir)
		if err != nil {
			return err
		}
		defer func() { _ = debugLog.Close() }()
		log.Printf("agent debug mode enabled windows_service=%t debug_log_dir=%q", *windowsService, strings.TrimSpace(*debugLogDir))
	}
	if *windowsService && strings.TrimSpace(*ipcPipe) == "" {
		*ipcPipe = ipc.DefaultWindowsPipe
	}
	var recentLogs *recentLogBuffer
	if strings.TrimSpace(*ipcPipe) != "" || strings.TrimSpace(*ipcSocket) != "" {
		recentLogs = newRecentLogBuffer(200)
		previousLogOutput := log.Writer()
		log.SetOutput(io.MultiWriter(previousLogOutput, recentLogs))
		defer log.SetOutput(previousLogOutput)
	}
	if *probeRTT && strings.TrimSpace(*wgInterface) == "" {
		return fmt.Errorf("wg-interface is required when probe-rtt is set")
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		return fmt.Errorf("timeout must be a positive duration")
	}
	stunTimeout, err := time.ParseDuration(strings.TrimSpace(*stunTimeoutValue))
	if err != nil || stunTimeout <= 0 {
		return fmt.Errorf("stun-timeout must be a positive duration")
	}
	interval, err := time.ParseDuration(strings.TrimSpace(*intervalValue))
	if err != nil || interval <= 0 {
		return fmt.Errorf("interval must be a positive duration")
	}
	reconnectMaxDelay, err := time.ParseDuration(strings.TrimSpace(*reconnectMaxDelayValue))
	if err != nil || reconnectMaxDelay <= 0 {
		return fmt.Errorf("reconnect-max-delay must be a positive duration")
	}
	if *reconnectJitter < 0 || *reconnectJitter > 1 {
		return fmt.Errorf("reconnect-jitter must be between 0 and 1")
	}
	endpointUpdateDebounce, err := time.ParseDuration(strings.TrimSpace(*endpointUpdateDebounceValue))
	if err != nil || endpointUpdateDebounce < 0 {
		return fmt.Errorf("endpoint-update-debounce must be a non-negative duration")
	}
	endpointTTL, err := time.ParseDuration(strings.TrimSpace(*endpointTTLValue))
	if err != nil || endpointTTL <= 0 {
		return fmt.Errorf("endpoint-ttl must be a positive duration")
	}
	maxCacheAge, err := parseOptionalDuration("max-cache-age", *maxCacheAgeValue)
	if err != nil {
		return err
	}
	tlsConfig, err := relayTLSConfig(*relayCAFile)
	if err != nil {
		return err
	}
	if flagWasSet(fs, "mtu") {
		cfg, err := client.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		if err := updateWireGuardMTUFromFlag(fs, *wireGuardMTU, &cfg); err != nil {
			return err
		}
		if err := client.SaveConfig(*configPath, cfg); err != nil {
			return err
		}
	}
	run := func(ctx context.Context) error {
		lockPath, err := client.AgentLockPath(*configPath)
		if err != nil {
			return err
		}
		lock, err := client.AcquireAgentLock(lockPath)
		if err != nil {
			return err
		}
		defer func() { _ = lock.Close() }()
		configStore, err := client.OpenConfigStore(*configPath)
		if err != nil {
			return err
		}
		stored := configStore.Read()
		wireGuard, err := client.NewWireGuardEngine(client.WireGuardEngineOptions{
			Interface:      firstNonEmpty(*wgInterface, "endlessnet"),
			ListenPort:     *listenPort,
			MTU:            stored.WireGuardMTU,
			RelayTimeout:   timeout,
			RelayTLSConfig: tlsConfig,
		})
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := wireGuard.Close(); closeErr != nil {
				log.Printf("close wireguard-go: %v", closeErr)
			}
		}()
		operationMu := &sync.Mutex{}
		syncWake := make(chan struct{}, 1)
		ipcOpts := agentIPCOptions{
			Pipe:           *ipcPipe,
			UnixSocket:     *ipcSocket,
			ConfigPath:     *configPath,
			StateOutput:    *stateOutput,
			ConfigStore:    configStore,
			OperationMu:    operationMu,
			DiagnosticsDir: *diagnosticsDir,
			RecentLogs:     recentLogs,
			ListenPort:     *listenPort,
			WGInterface:    firstNonEmpty(*wgInterface, "endlessnet"),
			Timeout:        timeout,
			WireGuard:      wireGuard,
			SyncWake:       syncWake,
		}
		stopIPC, err := startAgentIPC(ctx, ipcOpts)
		if err != nil {
			return err
		}
		defer stopIPC()
		var streamFromRevision uint64
		consecutiveFailures := 0
		endpointState := endpointUpdateState{}
		disconnectedLogged := false
		for {
			var snapshot client.AgentSnapshot
			var mapUnchanged bool
			var err error
			if intent, disconnected, intentErr := agentConnectionIntentStore(ipcOpts).Disconnected(); intentErr != nil {
				err = intentErr
			} else if disconnected {
				if !disconnectedLogged {
					updatedAt := strings.TrimSpace(intent.UpdatedAt)
					if updatedAt == "" {
						updatedAt = "unknown"
					}
					log.Printf("agent local connection intent is disconnected since %s; network sync paused", updatedAt)
					disconnectedLogged = true
				}
				if _, downErr := downAgentWireGuard(ctx, ipcOpts); downErr != nil {
					err = fmt.Errorf("enforce disconnected WireGuard state: %w", downErr)
				}
				if *once {
					if err != nil {
						_ = writeAgentFailureSnapshot(*stateOutput, *configPath, err)
					}
					return err
				}
				nextDelay := interval
				if err != nil {
					if failureErr := writeAgentFailureSnapshot(*stateOutput, *configPath, err); failureErr != nil {
						log.Printf("agent failure state write failed: %v", failureErr)
					}
					consecutiveFailures++
					nextDelay = agentReconnectDelay(interval, reconnectMaxDelay, consecutiveFailures, *reconnectJitter, randomJitterUnit())
					log.Printf("agent disconnected-state enforcement failed: %v; retrying in %s", err, nextDelay.Round(time.Millisecond))
				} else {
					consecutiveFailures = 0
				}
				proceed, woken := waitForAgentSync(ctx, nextDelay, syncWake)
				if !proceed {
					return nil
				}
				if woken {
					consecutiveFailures = 0
				}
				continue
			} else {
				disconnectedLogged = false
			}
			operationMu.Lock()
			skipForDisconnected := false
			skipForRecovery := false
			if _, disconnected, intentErr := agentConnectionIntentStore(ipcOpts).Disconnected(); intentErr != nil {
				err = intentErr
			} else if disconnected {
				skipForDisconnected = true
			}
			if err == nil && !skipForDisconnected {
				cfg := configStore.Read()
				if recovery := cfg.EnrollmentRecovery; recovery != nil {
					if recovery.Phase == client.RecoveryPhaseRecovering || recovery.Retryable {
						if _, downErr := downAgentWireGuard(ctx, ipcOpts); downErr != nil {
							skipForRecovery = true
							err = fmt.Errorf("tear down tunnel before recovery: %w", downErr)
						} else {
							progress, _ := continueEnrollmentRecovery(ctx, *configPath)
							if !progress.Completed {
								skipForRecovery = true
								err = fmt.Errorf("recovery pending: %s", firstNonEmpty(progress.ErrorCode, recoveryErrorProtocol))
							} else if progress.Terminal {
								skipForRecovery = true
								if statePath := strings.TrimSpace(*stateOutput); statePath != "" {
									if removeErr := os.Remove(statePath); removeErr != nil && !os.IsNotExist(removeErr) {
										err = fmt.Errorf("remove terminal recovery state: %w", removeErr)
									}
								}
							}
						}
					} else {
						skipForRecovery = true
					}
				}
			}
			manualEndpoint := strings.TrimSpace(*endpoint) != "" || strings.TrimSpace(*endpointFile) != ""
			if err == nil && !skipForDisconnected && !skipForRecovery && !*offline && manualEndpoint {
				candidates, generated, candidateErr := agentEndpointCandidates(*endpoint, *endpointFile, *listenPort)
				if candidateErr != nil {
					err = candidateErr
				} else if len(candidates) > 0 {
					_, err = updatePublishedEndpoint(*configPath, timeout, candidates, generated, endpointTTL, &endpointState, endpointUpdateDebounce, time.Now().UTC())
				}
			}
			if err == nil && !skipForDisconnected && !skipForRecovery {
				snapshot, mapUnchanged, err = runAgentIteration(ctx, agentIterationOptions{
					ConfigPath:     *configPath,
					StateOutput:    *stateOutput,
					ListenPort:     *listenPort,
					Timeout:        timeout,
					STUNTimeout:    stunTimeout,
					RelayTLSConfig: tlsConfig,
					Offline:        *offline,
					MaxCacheAge:    maxCacheAge,
					WireGuard:      wireGuard,
					WGInterface:    firstNonEmpty(*wgInterface, "endlessnet"),
					ProbeRTT:       *probeRTT,
					FromRevision:   streamFromRevision,
				})
			}
			if err == nil && !skipForDisconnected && !skipForRecovery && !*offline && !manualEndpoint {
				discovery := wireGuard.LastEndpointDiscovery()
				if len(discovery.Candidates) > 0 {
					_, err = updatePublishedEndpoint(*configPath, timeout, discovery.Candidates, true, endpointTTL, &endpointState, endpointUpdateDebounce, time.Now().UTC())
				}
			}
			operationMu.Unlock()
			if skipForDisconnected {
				proceed, woken := waitForAgentSync(ctx, interval, syncWake)
				if !proceed {
					return nil
				}
				if woken {
					consecutiveFailures = 0
				}
				continue
			}
			if skipForRecovery {
				if err != nil {
					if failureErr := writeAgentFailureSnapshot(*stateOutput, *configPath, err); failureErr != nil {
						log.Printf("agent recovery state write failed: %v", failureErr)
					}
				}
				proceed, _ := waitForAgentSync(ctx, interval, syncWake)
				if !proceed {
					return nil
				}
				continue
			}
			if *once {
				if err != nil {
					_ = writeAgentFailureSnapshot(*stateOutput, *configPath, err)
				}
				return err
			}
			nextDelay := interval
			if err != nil {
				if failureErr := writeAgentFailureSnapshot(*stateOutput, *configPath, err); failureErr != nil {
					log.Printf("agent failure state write failed: %v", failureErr)
				}
				consecutiveFailures++
				nextDelay = agentReconnectDelay(interval, reconnectMaxDelay, consecutiveFailures, *reconnectJitter, randomJitterUnit())
				log.Printf("agent sync failed: %v; reconnecting in %s", err, nextDelay.Round(time.Millisecond))
			} else if mapUnchanged {
				consecutiveFailures = 0
				if !*offline {
					streamFromRevision = snapshot.MapRevision
				}
				log.Printf("agent map unchanged; refreshed state revision %d", snapshot.MapRevision)
			} else {
				consecutiveFailures = 0
				if !*offline {
					streamFromRevision = snapshot.MapRevision
				}
				log.Printf("agent synced revision %d", snapshot.MapRevision)
			}
			proceed, woken := waitForAgentSync(ctx, nextDelay, syncWake)
			if !proceed {
				return nil
			}
			if woken {
				consecutiveFailures = 0
			}
		}
	}
	if *windowsService {
		closeEventLog, err := client.ConfigureWindowsEventLogger(*eventLogSource)
		if err != nil {
			return err
		}
		defer closeEventLog()
		log.Printf("windows service logging initialized with source %q", strings.TrimSpace(*eventLogSource))
		return client.RunWindowsService("endlessnet-client", run)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx)
}

func runAgentIteration(ctx context.Context, opts agentIterationOptions) (client.AgentSnapshot, bool, error) {
	cfg, networkMap, mapUnchanged, err := agentNetworkMap(opts.ConfigPath, opts.Timeout, opts.Offline, opts.FromRevision, opts.MaxCacheAge)
	if err != nil {
		return client.AgentSnapshot{}, false, err
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return client.AgentSnapshot{}, false, fmt.Errorf("private key is missing; run up first")
	}
	var applyResult *client.WireGuardApplyResult
	if opts.WireGuard != nil {
		if err := client.RequireWireGuardPrivileges(); err != nil {
			return client.AgentSnapshot{}, false, err
		}
		ignoredInterface := firstNonEmpty(opts.WGInterface, "endlessnet")
		if err := routeConflictErrorForMap(networkMap, ignoredInterface); err != nil {
			return client.AgentSnapshot{}, false, err
		}
	}
	if opts.WireGuard != nil {
		result, configureErr := opts.WireGuard.Configure(ctx, cfg, networkMap)
		applyResult = &result
		if configureErr != nil {
			return client.AgentSnapshot{}, false, configureErr
		}
	}
	var stunSnapshot *client.AgentSTUNSnapshot
	var wireGuardSnapshot *client.WireGuardInspection
	var pathSnapshot *[]client.PeerPathStatus
	var relaySnapshot *client.AgentRelaySnapshot
	var relayResult *client.RelayDialResult
	var relayErr error
	if opts.WireGuard != nil {
		stun := opts.WireGuard.STUNSnapshot(ctx, networkMap, opts.STUNTimeout)
		stunSnapshot = &stun
		inspection := opts.WireGuard.Inspection()
		wireGuardSnapshot = &inspection
		paths := opts.WireGuard.PathStatus()
		pathSnapshot = &paths
		if status, ok, statusErr := opts.WireGuard.RelayStatus(); ok {
			result := status.Relay
			relayResult = &result
			relaySnapshot = &client.AgentRelaySnapshot{
				OK:       status.Relay.Selected != nil,
				Selected: status.Relay.Selected,
				Attempts: status.Relay.Attempts,
			}
		} else if statusErr != nil {
			relayErr = statusErr
			relaySnapshot = &client.AgentRelaySnapshot{Attempts: []client.RelayDialAttempt{}, Error: statusErr.Error()}
		}
	}
	snapshot := client.BuildAgentSnapshot(ctx, networkMap, client.AgentProbeOptions{
		STUNTimeout:        opts.STUNTimeout,
		STUNSnapshot:       stunSnapshot,
		RelayTimeout:       opts.Timeout,
		RelayTLSConfig:     opts.RelayTLSConfig,
		RelaySnapshot:      relaySnapshot,
		RelayResult:        relayResult,
		RelayError:         relayErr,
		WireGuardInterface: opts.WGInterface,
		WireGuardSnapshot:  wireGuardSnapshot,
		PathSnapshot:       pathSnapshot,
		ProbeRTT:           opts.ProbeRTT,
	})
	snapshot.Apply = applyResult
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return client.AgentSnapshot{}, false, err
	}
	raw = append(raw, '\n')
	if strings.TrimSpace(opts.StateOutput) == "" {
		fmt.Print(string(raw))
		return snapshot, mapUnchanged, nil
	}
	if err := client.WriteFileAtomic(opts.StateOutput, raw, 0o600); err != nil {
		return client.AgentSnapshot{}, false, err
	}
	return snapshot, mapUnchanged, nil
}

func writeAgentFailureSnapshot(stateOutput, configPath string, failure error) error {
	if strings.TrimSpace(stateOutput) == "" || failure == nil {
		return nil
	}
	snapshot := client.AgentSnapshot{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		LastError:   redactDiagnosticsStringForJSON(failure.Error()),
	}
	if cfg, err := client.LoadConfig(configPath); err == nil {
		snapshot.NodeID = cfg.NodeID
		snapshot.NetworkID = cfg.NetworkID
		snapshot.MapRevision = cfg.MapRevision
		if cfg.CachedMap != nil {
			if cached, verifyErr := verifiedCachedNetworkMap(&cfg); verifyErr == nil {
				snapshot.NodeID = cached.Node.ID
				snapshot.NetworkID = cached.Network.ID
				snapshot.NetworkName = cached.Network.Name
				snapshot.OverlayIP = cached.Node.AssignedIP
				snapshot.OverlayIPv6 = cached.Node.AssignedIPv6
				snapshot.MapRevision = cached.Network.Revision
				snapshot.PeerCount = len(cached.Peers)
			}
		}
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return client.WriteFileAtomic(stateOutput, raw, 0o600)
}

func agentNetworkMap(configPath string, timeout time.Duration, offline bool, fromRevision uint64, maxCacheAge time.Duration) (client.Config, clientapi.RegisterNodeResponse, bool, error) {
	if !offline {
		return agentOnlineNetworkMap(configPath, timeout, fromRevision)
	}
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	networkMap, err := verifiedCachedNetworkMapWithMaxAge(&cfg, maxCacheAge)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	return cfg, networkMap, false, nil
}

func agentOnlineNetworkMap(configPath string, timeout time.Duration, fromRevision uint64) (client.Config, clientapi.RegisterNodeResponse, bool, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	if len(cfg.ControlURLs()) == 0 {
		return cfg, clientapi.RegisterNodeResponse{}, false, fmt.Errorf("server URL is required; run up or login first")
	}
	if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return cfg, clientapi.RegisterNodeResponse{}, false, fmt.Errorf("node identity is missing; run up first")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Timeout = timeout + 5*time.Second
	if err := refreshMapSigningTrust(&cfg, api); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	previousRevision := cfg.MapRevision
	approvalState := strings.ToLower(strings.TrimSpace(cfg.NodeApprovalState))
	if approvalState == clientapi.NodeApprovalRejected {
		return cfg, clientapi.RegisterNodeResponse{}, false, errors.New("node enrollment was rejected")
	}
	if approvalState != clientapi.NodeApprovalPending {
		heartbeatMap, heartbeatSent, err := updatePublishedEndpointRequestFromConfig(&cfg, api, clientapi.UpdateNodeEndpointRequest{Status: clientapi.NodeStatusOnline})
		if err != nil {
			return cfg, clientapi.RegisterNodeResponse{}, false, err
		}
		if heartbeatSent {
			if err := client.SaveConfig(configPath, cfg); err != nil {
				return cfg, clientapi.RegisterNodeResponse{}, false, err
			}
			if heartbeatMap.Network.Revision > previousRevision {
				return cfg, heartbeatMap, false, nil
			}
		}
	}
	event, err := api.ReadMapStreamEvent(cfg.NodeID, mapStreamCursor(cfg, fromRevision), timeout)
	mapUnchanged := false
	if errors.Is(err, clientapi.ErrMapStreamNoEvent) {
		mapUnchanged = true
		event, err = api.ReadMapStreamEvent(cfg.NodeID, clientapi.MapCursor{}, timeout)
	}
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	networkMap, _, err := cacheNetworkMapFromEvent(&cfg, event)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	if err := client.SaveConfig(configPath, cfg); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, false, err
	}
	return cfg, networkMap, mapUnchanged, nil
}
