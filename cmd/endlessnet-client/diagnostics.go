package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/unng-lab/endlessnet-client/internal/client"
	ipc "github.com/unng-lab/endlessnet-client/ipc/v1"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

func cmdDiagnostics(args []string) error {
	fs := flag.NewFlagSet("diagnostics", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	output := fs.String("output", "", "write diagnostics JSON to file instead of stdout")
	agentStatePath := fs.String("agent-state", "", "agent state JSON path written by agent")
	controlMetrics := fs.Bool("control-metrics", false, "include public control-plane /metrics counters")
	wgInterface := fs.String("wg-interface", "", "live WireGuard interface to inspect")
	probeRTT := fs.Bool("probe-rtt", false, "probe direct path RTT with ping; requires wg-interface")
	routeTargets := multiFlag{}
	fs.Var(&routeTargets, "route-target", "route target to inspect with ip route get; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(routeTargets) > 0 && strings.TrimSpace(*wgInterface) == "" {
		err := fmt.Errorf("wg-interface is required when route-target is set")
		return writeDiagnosticsErrorPayload(*output, cliErrorWGInterfaceRequired, err)
	}
	if *probeRTT && strings.TrimSpace(*wgInterface) == "" {
		err := fmt.Errorf("wg-interface is required when probe-rtt is set")
		return writeDiagnosticsErrorPayload(*output, cliErrorWGInterfaceRequired, err)
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return writeDiagnosticsErrorPayload(*output, clientConfigErrorCode(err), err)
	}
	var agentState *client.AgentSnapshot
	if strings.TrimSpace(*agentStatePath) != "" {
		loaded, err := client.LoadAgentSnapshot(*agentStatePath)
		if err != nil {
			return writeDiagnosticsErrorPayload(*output, cliErrorAgentStateUnavailable, err)
		}
		agentState = &loaded
	}
	payload := diagnosticsPayloadWithAgentState(cfg, agentState)
	attachLocalRouteConflicts(payload, cfg, true, *wgInterface)
	if *controlMetrics {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		metrics, err := fetchControlMetrics(ctx, firstControlPlaneURL(cfg))
		if err != nil {
			return writeDiagnosticsErrorPayload(*output, cliErrorControlMetricsFailed, err)
		}
		payload["control_metrics"] = metrics
	}
	if strings.TrimSpace(*wgInterface) != "" {
		attachLiveWireGuardStatus(payload, cfg, *wgInterface, []string(routeTargets), *probeRTT, 5*time.Second, client.RelayDialResult{}, errors.New("relay probing was not run by diagnostics"))
	}
	payload = sanitizeDiagnosticsPayloadForJSON(payload)
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if strings.TrimSpace(*output) == "" {
		fmt.Print(string(raw))
		return nil
	}
	if err := os.WriteFile(*output, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote diagnostics to %s\n", *output)
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	agentStatePath := fs.String("agent-state", "", "agent state JSON path written by agent")
	wgInterface := fs.String("wg-interface", "", "live WireGuard interface to inspect")
	probeRTT := fs.Bool("probe-rtt", false, "probe direct path RTT with ping; requires wg-interface")
	probeRelay := fs.Bool("probe-relay", false, "probe relay reachability and include relay path selection")
	relayTimeoutValue := fs.String("relay-timeout", "2s", "overall relay probe timeout when probe-relay is set")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	controlMetrics := fs.Bool("control-metrics", false, "include public control-plane /metrics counters")
	routeTargets := multiFlag{}
	fs.Var(&routeTargets, "route-target", "route target to inspect with ip route get; repeatable")
	jsonOutput := fs.Bool("json", false, "write status as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(routeTargets) > 0 && strings.TrimSpace(*wgInterface) == "" {
		err := fmt.Errorf("wg-interface is required when route-target is set")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorWGInterfaceRequired, err)
		}
		return err
	}
	if *probeRTT && strings.TrimSpace(*wgInterface) == "" {
		err := fmt.Errorf("wg-interface is required when probe-rtt is set")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorWGInterfaceRequired, err)
		}
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(clientConfigErrorCode(err), err)
		}
		return err
	}
	relayResult := client.RelayDialResult{}
	relayErr := errors.New("relay probing was not run by status")
	if *probeRelay {
		relayTimeout, err := time.ParseDuration(strings.TrimSpace(*relayTimeoutValue))
		if err != nil || relayTimeout <= 0 {
			err := fmt.Errorf("relay-timeout must be a positive duration")
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorInvalidRelayTimeout, err)
			}
			return err
		}
		var networkMap clientapi.RegisterNodeResponse
		cfg, networkMap, err = freshNodeNetworkMap(*configPath, relayTimeout)
		if err != nil {
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorNetworkMapUnavailable, err)
			}
			return err
		}
		relayResult, relayErr, err = probeStatusRelay(networkMap, relayTimeout, *relayCAFile)
		if err != nil {
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorRelayUnavailable, err)
			}
			return err
		}
	}
	status := statusPayload(cfg)
	attachLocalRouteConflicts(status, cfg, false, *wgInterface)
	controlCtx, controlCancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer controlCancel()
	attachControlAvailability(controlCtx, status, cfg.ControlURLs()...)
	if *controlMetrics {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		metrics, err := fetchControlMetrics(ctx, firstControlPlaneURL(cfg))
		if err != nil {
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorControlMetricsFailed, err)
			}
			return err
		}
		status["control_metrics"] = metrics
	}
	if strings.TrimSpace(*agentStatePath) != "" {
		agentState, err := client.LoadAgentSnapshot(*agentStatePath)
		if err != nil {
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorAgentStateUnavailable, err)
			}
			return err
		}
		attachAgentStatus(status, agentState)
	}
	if strings.TrimSpace(*wgInterface) != "" {
		attachLiveWireGuardStatus(status, cfg, *wgInterface, []string(routeTargets), *probeRTT, 5*time.Second, relayResult, relayErr)
	}
	if *jsonOutput {
		raw, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	nodeID, _ := status["node_id"].(string)
	networkName, _ := status["network_name"].(string)
	overlayIP, _ := status["overlay_ip"].(string)
	controlState, _ := status["control_state"].(string)
	fmt.Printf("node_id: %s\n", firstNonEmpty(nodeID, "-"))
	fmt.Printf("network: %s\n", firstNonEmpty(networkName, "-"))
	fmt.Printf("overlay_ip: %s\n", firstNonEmpty(overlayIP, "-"))
	fmt.Printf("control_state: %s\n", controlState)
	fmt.Printf("map_revision: %v\n", status["map_revision"])
	fmt.Printf("route_table: %v\n", status["route_table"])
	fmt.Printf("peers: %v\n", status["peer_count"])
	stunCount := 0
	if endpoints, ok := status["stun_endpoints"].([]any); ok {
		stunCount = len(endpoints)
	}
	relayCount := 0
	if endpoints, ok := status["relay_endpoints"].([]any); ok {
		relayCount = len(endpoints)
	}
	fmt.Printf("stun: %d\n", stunCount)
	fmt.Printf("relays: %d\n", relayCount)
	fmt.Printf("route_conflicts: %v\n", status["route_conflict_count"])
	if conflicts, ok := status["route_conflicts"].([]client.OverlayCIDRConflict); ok {
		for _, conflict := range conflicts {
			fmt.Printf("route_conflict: %s overlaps %s on %s\n", conflict.OverlayCIDR, conflict.LocalPrefix, conflict.Interface)
		}
	}
	if agent, ok := status["agent"].(map[string]any); ok && agent["state_present"] == true {
		fmt.Printf("agent_relay_ok: %v\n", agent["relay_ok"])
		fmt.Printf("agent_stun_ok: %v\n", agent["stun_ok"])
	}
	if wg, ok := status["wireguard"].(client.WireGuardInspection); ok {
		fmt.Printf("wireguard_ok: %v\n", wg.OK)
		fmt.Printf("wireguard_peers: %d\n", wg.PeerCount)
	}
	if paths, ok := status["paths"].([]client.PeerPathStatus); ok {
		fmt.Printf("paths: %d\n", len(paths))
	}
	if controlMetrics, ok := status["control_metrics"].(map[string]any); ok {
		if relayMetrics, ok := controlMetrics["relay"].(map[string]float64); ok {
			fmt.Printf("relay_sessions_total: %v\n", relayMetrics["sessions_total"])
			fmt.Printf("relay_frames_inbound_total: %v\n", relayMetrics["frames_inbound_total"])
			fmt.Printf("relay_frames_outbound_total: %v\n", relayMetrics["frames_outbound_total"])
		}
	}
	return nil
}

const diagnosticsRedactedValue = "[redacted]"

func sanitizeDiagnosticsPayloadForJSON(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	sanitized, ok := sanitizeDiagnosticsAnyForJSON(payload).(map[string]any)
	if !ok {
		return payload
	}
	return sanitized
}

func sanitizeDiagnosticsAnyForJSON(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return sanitizeDiagnosticsValueForJSON(value)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return sanitizeDiagnosticsValueForJSON(value)
	}
	return sanitizeDiagnosticsValueForJSON(decoded)
}

func sanitizeDiagnosticsValueForJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveDiagnosticsKey(key) {
				out[key] = diagnosticsRedactedValue
				continue
			}
			out[key] = sanitizeDiagnosticsValueForJSON(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, sanitizeDiagnosticsValueForJSON(child))
		}
		return out
	case string:
		return redactDiagnosticsStringForJSON(typed)
	default:
		return value
	}
}

func sensitiveDiagnosticsKey(key string) bool {
	normalized := strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	if strings.HasSuffix(normalized, "_present") {
		return false
	}
	switch normalized {
	case "token", "enroll_token", "enrollment_token", "join_token", "access_token", "refresh_token", "session_token", "authorization":
		return true
	}
	return strings.Contains(normalized, "private_key") ||
		strings.Contains(normalized, "privatekey") ||
		strings.Contains(normalized, "node_credential") ||
		strings.Contains(normalized, "nodecredential") ||
		strings.Contains(normalized, "relay_credential") ||
		strings.Contains(normalized, "relaycredential") ||
		strings.Contains(normalized, "client_secret")
}

func redactDiagnosticsStringForJSON(value string) string {
	redacted := client.RedactServiceLogMessage(value)
	if redacted != value {
		return redacted
	}
	if strings.Contains(strings.ToLower(value), "authorization: bearer") {
		return diagnosticsRedactedValue
	}
	return value
}

func serviceIPCDiagnosticsPayload(cfg client.Config, status ipc.StatusResponse, agentState *client.AgentSnapshot) ipc.Diagnostics {
	payload := ipc.Diagnostics{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Client: ipc.DiagnosticsClientInfo{
			Product:    "EndlessNet Client",
			Version:    version,
			Commit:     commit,
			BuildDate:  buildDate,
			TargetOS:   runtime.GOOS,
			TargetArch: runtime.GOARCH,
		},
		Runtime: ipc.DiagnosticsRuntimeInfo{
			GOOS:      runtime.GOOS,
			GOARCH:    runtime.GOARCH,
			GOVersion: runtime.Version(),
			OS:        serviceIPCDiagnosticsOSVersion(),
		},
		Status:     status,
		LastErrors: serviceIPCDiagnosticsLastErrors(status, agentState),
		Config: ipc.DiagnosticsConfig{
			ControlPlaneURLs:          append([]string(nil), cfg.ControlURLs()...),
			NodeID:                    cfg.NodeID,
			MapRevision:               cfg.MapRevision,
			MapSigningTrustPresent:    client.HasSigningTrust(cfg),
			TokenPresent:              strings.TrimSpace(cfg.Token) != "",
			IdentityPrivateKeyPresent: strings.TrimSpace(cfg.IdentityPrivateKey) != "",
			PrivateKeyPresent:         strings.TrimSpace(cfg.PrivateKey) != "",
			NodeCredentialPresent:     strings.TrimSpace(cfg.NodeCredential) != "",
			DeviceFingerprintPresent:  strings.TrimSpace(cfg.DeviceFingerprint) != "",
			WireGuardRouteTable:       strings.TrimSpace(cfg.WireGuardRouteTable),
			CachedMapPresent:          cfg.CachedMap != nil,
		},
		RecentLogs:     []ipc.LogEntry{},
		Interfaces:     []ipc.NetworkInterfaceStatus{},
		RouteConflicts: []ipc.OverlayCIDRConflict{},
	}
	if cfg.CachedMap != nil {
		cachedMap := *cfg.CachedMap
		cachedMap.NodeCredential = ""
		cachedMap.RelayCredential = nil
		dnsSummary := serviceIPCDiagnosticsDNSSummary(cachedMap)
		routeSummary := serviceIPCDiagnosticsRouteSummary(cachedMap, cfg)
		payload.DNSSummary = &dnsSummary
		payload.RouteSummary = &routeSummary
	}
	return payload
}

func serviceIPCDiagnosticsLastErrors(status ipc.StatusResponse, agentState *client.AgentSnapshot) []string {
	errors := []string{}
	appendError := func(value string) {
		if value = strings.TrimSpace(value); value != "" {
			errors = append(errors, value)
		}
	}
	appendError(status.LocalStateError)
	appendError(status.ApprovalError)
	appendError(status.CachedMapError)
	appendError(status.ConnectionIntentError)
	if status.Control != nil {
		appendError(status.Control.Error)
	}
	if agentState == nil {
		return errors
	}
	appendError(agentState.LastError)
	appendError(agentState.STUN.Error)
	appendError(agentState.Relay.Error)
	if agentState.WireGuard != nil {
		appendError(agentState.WireGuard.Error)
	}
	if agentState.Apply != nil {
		appendError(agentState.Apply.DownError)
		appendError(agentState.Apply.UpError)
		appendError(agentState.Apply.SyncError)
		appendError(agentState.Apply.RouteError)
	}
	return errors
}

func serviceIPCDiagnosticsDNSSummary(networkMap clientapi.RegisterNodeResponse) ipc.DiagnosticsDNSSummary {
	records := []ipc.DiagnosticsDNSRecord{}
	addRecord := func(nodeID, hostname, ipv4, ipv6 string) {
		label := client.DNSLabel(hostname)
		if label == "" || strings.TrimSpace(nodeID) == "" || (strings.TrimSpace(ipv4) == "" && strings.TrimSpace(ipv6) == "") {
			return
		}
		records = append(records, ipc.DiagnosticsDNSRecord{
			NodeID:   strings.TrimSpace(nodeID),
			Hostname: strings.TrimSpace(hostname),
			Label:    label,
			FQDN:     label + "." + client.DefaultDNSDomain(networkMap.Network.Name),
			IPv4:     strings.TrimSpace(ipv4),
			IPv6:     strings.TrimSpace(ipv6),
		})
	}
	addRecord(networkMap.Node.ID, networkMap.Node.Hostname, diagnosticNormalizedAddress(networkMap.Node.AssignedIP, false), diagnosticNormalizedAddress(networkMap.Node.AssignedIPv6, true))
	for _, peer := range networkMap.Peers {
		ipv4, ipv6 := diagnosticPeerDNSAddresses(peer.AllowedIPs)
		addRecord(peer.ID, peer.Hostname, ipv4, ipv6)
	}
	return ipc.DiagnosticsDNSSummary{
		SearchDomain:      client.DefaultDNSDomain(networkMap.Network.Name),
		TTLSeconds:        client.DefaultDNSTTLSeconds,
		NetworkDNSServers: append([]string{}, networkMap.Network.DNS...),
		RecordCount:       len(records),
		Records:           records,
	}
}

func serviceIPCDiagnosticsRouteSummary(networkMap clientapi.RegisterNodeResponse, cfg client.Config) ipc.DiagnosticsRouteSummary {
	subnetRoutes := []ipc.DiagnosticsSubnetRoute{}
	allowedIPCount := 0
	defaultRoutePresent := false
	for _, peer := range networkMap.Peers {
		for _, allowed := range peer.AllowedIPs {
			allowed = strings.TrimSpace(strings.TrimSuffix(allowed, ","))
			if allowed == "" {
				continue
			}
			allowedIPCount++
			prefix, ok := diagnosticAllowedIPPrefix(allowed)
			if !ok {
				continue
			}
			if prefix.Bits() == 0 {
				defaultRoutePresent = true
			}
			if prefix.Bits() == prefix.Addr().BitLen() {
				continue
			}
			subnetRoutes = append(subnetRoutes, ipc.DiagnosticsSubnetRoute{
				PeerID:   peer.ID,
				Hostname: peer.Hostname,
				CIDR:     prefix.String(),
			})
		}
	}
	return ipc.DiagnosticsRouteSummary{
		OverlayCIDR:         networkMap.Network.CIDR,
		OverlayIPv6CIDR:     networkMap.Network.IPv6CIDR,
		PeerCount:           len(networkMap.Peers),
		AllowedIPCount:      allowedIPCount,
		PeerRouteTargets:    append([]string{}, client.WireGuardRouteTargetsForPeers(networkMap.Peers)...),
		SubnetRouteCount:    len(subnetRoutes),
		SubnetRoutes:        subnetRoutes,
		DefaultRoutePresent: defaultRoutePresent,
		WireGuardRouteTable: strings.TrimSpace(cfg.WireGuardRouteTable),
	}
}

func serviceIPCDiagnosticsRouteConflicts(cfg client.Config, interfaces []client.NetworkInterfaceStatus, ignoredInterfaces ...string) []client.OverlayCIDRConflict {
	conflicts := []client.OverlayCIDRConflict{}
	if cfg.CachedMap == nil {
		return conflicts
	}
	cached, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		return conflicts
	}
	return client.OverlayCIDRConflicts(cached, interfaces, ignoredInterfaces...)
}

func sanitizeServiceIPCDiagnosticsPayload(payload ipc.Diagnostics) (ipc.Diagnostics, error) {
	sanitized := sanitizeDiagnosticsAnyForJSON(payload)
	raw, err := json.Marshal(sanitized)
	if err != nil {
		return ipc.Diagnostics{}, err
	}
	var out ipc.Diagnostics
	if err := json.Unmarshal(raw, &out); err != nil {
		return ipc.Diagnostics{}, err
	}
	return out, nil
}

func diagnosticsPayloadWithAgentState(cfg client.Config, agentState *client.AgentSnapshot) map[string]any {
	var cachedMap *clientapi.RegisterNodeResponse
	if cfg.CachedMap != nil {
		cached := *cfg.CachedMap
		cached.NodeCredential = ""
		cached.RelayCredential = nil
		cachedMap = &cached
	}
	status := statusPayload(cfg)
	if agentState != nil {
		attachAgentStatus(status, *agentState)
	}
	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"client": map[string]any{
			"product":     "EndlessNet Client",
			"version":     version,
			"commit":      commit,
			"build_date":  buildDate,
			"target_os":   runtime.GOOS,
			"target_arch": runtime.GOARCH,
		},
		"runtime":     diagnosticsRuntimeInfo(),
		"status":      status,
		"last_errors": diagnosticsLastErrors(status, agentState),
		"config": map[string]any{
			"control_plane_urls":           append([]string(nil), cfg.ControlPlaneURLs...),
			"node_id":                      cfg.NodeID,
			"map_revision":                 cfg.MapRevision,
			"map_signing_trust_present":    client.HasSigningTrust(cfg),
			"token_present":                strings.TrimSpace(cfg.Token) != "",
			"identity_private_key_present": strings.TrimSpace(cfg.IdentityPrivateKey) != "",
			"private_key_present":          strings.TrimSpace(cfg.PrivateKey) != "",
			"node_credential_present":      strings.TrimSpace(cfg.NodeCredential) != "",
			"device_fingerprint_present":   strings.TrimSpace(cfg.DeviceFingerprint) != "",
			"wireguard_route_table":        strings.TrimSpace(cfg.WireGuardRouteTable),
			"cached_map_present":           cachedMap != nil,
		},
		"cached_map": cachedMap,
	}
	if cachedMap != nil {
		payload["dns_summary"] = diagnosticsDNSSummary(*cachedMap)
		payload["route_summary"] = diagnosticsRouteSummary(*cachedMap, cfg)
	}
	if agentState != nil {
		payload["agent_state"] = *agentState
	}
	return payload
}

func diagnosticsRuntimeInfo() map[string]any {
	return map[string]any{
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"go_version": runtime.Version(),
		"os":         diagnosticsOSVersion(),
	}
}

func diagnosticsLastErrors(status map[string]any, agentState *client.AgentSnapshot) []string {
	errors := []string{}
	appendError := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			errors = append(errors, value)
		}
	}
	appendMapError := func(values map[string]any, key string) {
		if value, ok := values[key]; ok {
			appendError(fmt.Sprint(value))
		}
	}
	appendMapError(status, "local_state_error")
	appendMapError(status, "cached_map_error")
	if controlStatus, ok := status["control"].(map[string]any); ok {
		appendMapError(controlStatus, "error")
	}
	if agentState != nil {
		appendError(agentState.LastError)
		appendError(agentState.STUN.Error)
		appendError(agentState.Relay.Error)
		if agentState.WireGuard != nil {
			appendError(agentState.WireGuard.Error)
		}
		if agentState.Apply != nil {
			appendError(agentState.Apply.DownError)
			appendError(agentState.Apply.UpError)
			appendError(agentState.Apply.SyncError)
			appendError(agentState.Apply.RouteError)
		}
	}
	return errors
}

func diagnosticsDNSSummary(networkMap clientapi.RegisterNodeResponse) map[string]any {
	records := []map[string]string{}
	addRecord := func(nodeID, hostname, ipv4, ipv6 string) {
		label := client.DNSLabel(hostname)
		if label == "" || strings.TrimSpace(nodeID) == "" || (strings.TrimSpace(ipv4) == "" && strings.TrimSpace(ipv6) == "") {
			return
		}
		record := map[string]string{
			"node_id":  strings.TrimSpace(nodeID),
			"hostname": strings.TrimSpace(hostname),
			"label":    label,
			"fqdn":     label + "." + client.DefaultDNSDomain(networkMap.Network.Name),
		}
		if strings.TrimSpace(ipv4) != "" {
			record["ipv4"] = strings.TrimSpace(ipv4)
		}
		if strings.TrimSpace(ipv6) != "" {
			record["ipv6"] = strings.TrimSpace(ipv6)
		}
		records = append(records, record)
	}
	addRecord(networkMap.Node.ID, networkMap.Node.Hostname, diagnosticNormalizedAddress(networkMap.Node.AssignedIP, false), diagnosticNormalizedAddress(networkMap.Node.AssignedIPv6, true))
	for _, peer := range networkMap.Peers {
		ipv4, ipv6 := diagnosticPeerDNSAddresses(peer.AllowedIPs)
		addRecord(peer.ID, peer.Hostname, ipv4, ipv6)
	}
	return map[string]any{
		"search_domain":       client.DefaultDNSDomain(networkMap.Network.Name),
		"ttl_seconds":         client.DefaultDNSTTLSeconds,
		"network_dns_servers": append([]string(nil), networkMap.Network.DNS...),
		"record_count":        len(records),
		"records":             records,
	}
}

func diagnosticsRouteSummary(networkMap clientapi.RegisterNodeResponse, cfg client.Config) map[string]any {
	subnetRoutes := []map[string]string{}
	allowedIPCount := 0
	defaultRoutePresent := false
	for _, peer := range networkMap.Peers {
		for _, allowed := range peer.AllowedIPs {
			allowed = strings.TrimSpace(strings.TrimSuffix(allowed, ","))
			if allowed == "" {
				continue
			}
			allowedIPCount++
			prefix, ok := diagnosticAllowedIPPrefix(allowed)
			if !ok {
				continue
			}
			if prefix.Bits() == 0 {
				defaultRoutePresent = true
			}
			if prefix.Bits() == prefix.Addr().BitLen() {
				continue
			}
			subnetRoutes = append(subnetRoutes, map[string]string{
				"peer_id":  peer.ID,
				"hostname": peer.Hostname,
				"cidr":     prefix.String(),
			})
		}
	}
	summary := map[string]any{
		"overlay_cidr":          networkMap.Network.CIDR,
		"peer_count":            len(networkMap.Peers),
		"allowed_ip_count":      allowedIPCount,
		"peer_route_targets":    client.WireGuardRouteTargetsForPeers(networkMap.Peers),
		"subnet_route_count":    len(subnetRoutes),
		"subnet_routes":         subnetRoutes,
		"default_route_present": defaultRoutePresent,
		"wireguard_route_table": strings.TrimSpace(cfg.WireGuardRouteTable),
	}
	if strings.TrimSpace(networkMap.Network.IPv6CIDR) != "" {
		summary["overlay_ipv6_cidr"] = networkMap.Network.IPv6CIDR
	}
	return summary
}

func diagnosticPeerDNSAddresses(allowedIPs []string) (string, string) {
	var ipv4, ipv6 string
	for _, allowed := range allowedIPs {
		addr, ok := diagnosticHostAddressFromAllowedIP(allowed)
		if !ok {
			continue
		}
		if addr.Is6() {
			if ipv6 == "" {
				ipv6 = addr.String()
			}
			continue
		}
		if ipv4 == "" {
			ipv4 = addr.String()
		}
	}
	return ipv4, ipv6
}

func diagnosticHostAddressFromAllowedIP(value string) (netip.Addr, bool) {
	prefix, ok := diagnosticAllowedIPPrefix(value)
	if !ok {
		return netip.Addr{}, false
	}
	addr := prefix.Addr()
	if prefix.Bits() != addr.BitLen() {
		return netip.Addr{}, false
	}
	return addr, true
}

func diagnosticAllowedIPPrefix(value string) (netip.Prefix, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ","))
	if value == "" {
		return netip.Prefix{}, false
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix, true
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	return netip.Prefix{}, false
}

func diagnosticNormalizedAddress(value string, wantIPv6 bool) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Is6() != wantIPv6 {
		return ""
	}
	return addr.String()
}

func fetchControlMetrics(ctx context.Context, serverURL string) (map[string]any, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required to fetch control metrics")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(serverURL, "/")+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := clientapi.NewControlPlaneHTTPClient(15*time.Second, nil).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("control metrics returned %s", resp.Status)
	}
	return map[string]any{
		"relay": parseRelayMetrics(string(body)),
	}, nil
}

func parseRelayMetrics(raw string) map[string]float64 {
	metrics := map[string]string{
		"sessions_active":           "endlessnet_relay_sessions_active",
		"sessions_total":            "endlessnet_relay_sessions_total",
		"sessions_revoked_total":    "endlessnet_relay_sessions_revoked_total",
		"auth_failures_total":       "endlessnet_relay_auth_failures_total",
		"frames_inbound_total":      `endlessnet_relay_frames_total{direction="inbound"}`,
		"frames_outbound_total":     `endlessnet_relay_frames_total{direction="outbound"}`,
		"bytes_inbound_total":       `endlessnet_relay_bytes_total{direction="inbound"}`,
		"bytes_outbound_total":      `endlessnet_relay_bytes_total{direction="outbound"}`,
		"drops_invalid_total":       `endlessnet_relay_drops_total{reason="invalid"}`,
		"drops_no_peer_total":       `endlessnet_relay_drops_total{reason="no_peer"}`,
		"drops_write_failed_total":  `endlessnet_relay_drops_total{reason="write_failed"}`,
		"drops_slow_consumer_total": `endlessnet_relay_drops_total{reason="slow_consumer"}`,
		"drops_bandwidth_total":     `endlessnet_relay_drops_total{reason="bandwidth"}`,
		"drops_acl_total":           `endlessnet_relay_drops_total{reason="acl"}`,
		"slow_consumers_total":      "endlessnet_relay_slow_consumers_total",
		"draining":                  "endlessnet_relay_draining",
	}
	out := map[string]float64{}
	for key, metric := range metrics {
		if value, ok := prometheusMetricValue(raw, metric); ok {
			out[key] = value
		}
	}
	return out
}

func prometheusMetricValue(raw, metric string) (float64, bool) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != metric {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
