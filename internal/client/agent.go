package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	relayauth "github.com/endless-net/relay/protocol/v1"
)

type AgentProbeOptions struct {
	GeneratedAt        time.Time
	STUNTimeout        time.Duration
	STUNSnapshot       *AgentSTUNSnapshot
	RelayTimeout       time.Duration
	RelayTLSConfig     *tls.Config
	RelaySnapshot      *AgentRelaySnapshot
	RelayResult        *RelayDialResult
	RelayError         error
	WireGuardInterface string
	WireGuardSnapshot  *WireGuardInspection
	PathSnapshot       *[]PeerPathStatus
	ProbeRTT           bool
	WGCommand          string
	IPCommand          string
	PingCommand        string
	Runner             CommandRunner
}

type AgentSTUNSnapshot struct {
	OK           bool                `json:"ok"`
	Results      []STUNCheckResult   `json:"results"`
	NAT          STUNMappingSummary  `json:"nat"`
	PortMappings []PortMappingResult `json:"port_mappings,omitempty"`
	Error        string              `json:"error,omitempty"`
}

type AgentRelaySnapshot struct {
	OK       bool                `json:"ok"`
	Selected *relayauth.Endpoint `json:"selected,omitempty"`
	Attempts []RelayDialAttempt  `json:"attempts"`
	Error    string              `json:"error,omitempty"`
}

type AgentSnapshot struct {
	GeneratedAt string                `json:"generated_at"`
	NodeID      string                `json:"node_id"`
	NetworkID   string                `json:"network_id"`
	NetworkName string                `json:"network_name"`
	OverlayIP   string                `json:"overlay_ip"`
	OverlayIPv6 string                `json:"overlay_ipv6,omitempty"`
	MapRevision uint64                `json:"map_revision"`
	PeerCount   int                   `json:"peer_count"`
	STUN        AgentSTUNSnapshot     `json:"stun"`
	Relay       AgentRelaySnapshot    `json:"relay"`
	WireGuard   *WireGuardInspection  `json:"wireguard,omitempty"`
	Apply       *WireGuardApplyResult `json:"apply,omitempty"`
	LastError   string                `json:"last_error,omitempty"`
	Paths       []PeerPathStatus      `json:"paths"`
}

func BuildAgentSnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, opts AgentProbeOptions) AgentSnapshot {
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	stun := buildAgentSTUNSnapshot(ctx, networkMap, opts.STUNTimeout)
	if opts.STUNSnapshot != nil {
		stun = *opts.STUNSnapshot
	}
	relay, relayResult, relayErr := agentRelaySnapshot(ctx, networkMap, opts)
	wireGuard, rttProbes := buildAgentWireGuardSnapshot(ctx, networkMap, opts)
	paths := BuildPathStatusesWithDirectProbes(networkMap, relayResult, relayErr, wireGuard, rttProbes)
	if opts.PathSnapshot != nil {
		paths = append([]PeerPathStatus(nil), (*opts.PathSnapshot)...)
	}
	return AgentSnapshot{
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		NodeID:      networkMap.Node.ID,
		NetworkID:   networkMap.Network.ID,
		NetworkName: networkMap.Network.Name,
		OverlayIP:   networkMap.Node.AssignedIP,
		OverlayIPv6: networkMap.Node.AssignedIPv6,
		MapRevision: networkMap.Network.Revision,
		PeerCount:   len(networkMap.Peers),
		STUN:        stun,
		Relay:       relay,
		WireGuard:   wireGuard,
		Paths:       paths,
	}
}

func agentRelaySnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, opts AgentProbeOptions) (AgentRelaySnapshot, RelayDialResult, error) {
	if opts.RelaySnapshot != nil {
		relay := *opts.RelaySnapshot
		var result RelayDialResult
		if opts.RelayResult != nil {
			result = *opts.RelayResult
		}
		return relay, result, opts.RelayError
	}
	return buildAgentRelaySnapshot(ctx, networkMap, opts.RelayTimeout, opts.RelayTLSConfig)
}

func LoadAgentSnapshot(path string) (AgentSnapshot, error) {
	var snapshot AgentSnapshot
	if strings.TrimSpace(path) == "" {
		return snapshot, fmt.Errorf("agent state path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return snapshot, fmt.Errorf("agent state file is empty")
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func buildAgentSTUNSnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, timeout time.Duration) AgentSTUNSnapshot {
	if len(networkMap.STUNEndpoints) == 0 {
		nat := ClassifySTUN(nil)
		return AgentSTUNSnapshot{
			Results: []STUNCheckResult{},
			NAT:     nat,
			Error:   nat.Error,
		}
	}
	results := CheckSTUN(ctx, networkMap.STUNEndpoints, timeout)
	nat := ClassifySTUN(results)
	out := AgentSTUNSnapshot{
		OK:      nat.ReachableEndpoints > 0,
		Results: results,
		NAT:     nat,
	}
	if !out.OK {
		out.Error = nat.Error
	}
	return out
}

func buildAgentRelaySnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, timeout time.Duration, tlsConfig *tls.Config) (AgentRelaySnapshot, RelayDialResult, error) {
	if len(networkMap.Relays) == 0 {
		err := fmt.Errorf("network map does not contain relay endpoints")
		return AgentRelaySnapshot{Attempts: []RelayDialAttempt{}, Error: err.Error()}, RelayDialResult{}, err
	}
	if networkMap.RelayCredential == nil {
		err := fmt.Errorf("relay credential is missing from network map")
		return AgentRelaySnapshot{Attempts: []RelayDialAttempt{}, Error: err.Error()}, RelayDialResult{}, err
	}
	conn, result, err := DialRelay(ctx, networkMap.Relays, *networkMap.RelayCredential, RelayDialOptions{
		Timeout:   timeout,
		TLSConfig: tlsConfig,
	})
	if conn != nil {
		_ = conn.Close()
	}
	out := AgentRelaySnapshot{
		OK:       err == nil,
		Selected: result.Selected,
		Attempts: result.Attempts,
	}
	if err != nil {
		out.Error = err.Error()
	}
	return out, result, err
}

func buildAgentWireGuardSnapshot(ctx context.Context, networkMap clientapi.RegisterNodeResponse, opts AgentProbeOptions) (*WireGuardInspection, map[string]DirectRTTProbe) {
	if opts.WireGuardSnapshot != nil {
		inspection := *opts.WireGuardSnapshot
		return &inspection, nil
	}
	if strings.TrimSpace(opts.WireGuardInterface) == "" {
		return nil, nil
	}
	routeTargets := WireGuardRouteTargetsForPeers(networkMap.Peers)
	var rttProbes map[string]DirectRTTProbe
	if opts.ProbeRTT {
		rttProbes = make(map[string]DirectRTTProbe, len(routeTargets))
		for _, target := range routeTargets {
			rttProbes[target] = ProbeDirectRTT(ctx, DirectRTTProbeOptions{
				Target:      target,
				PingCommand: opts.PingCommand,
				Runner:      opts.Runner,
			})
		}
	}
	inspection := InspectWireGuard(ctx, WireGuardInspectOptions{
		Interface:    opts.WireGuardInterface,
		RouteTargets: routeTargets,
		WGCommand:    opts.WGCommand,
		IPCommand:    opts.IPCommand,
		Runner:       opts.Runner,
	})
	return &inspection, rttProbes
}
