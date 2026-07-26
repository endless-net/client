package client

import (
	"context"
	"net/netip"
	"strconv"
	"strings"

	clientapi "github.com/unng-lab/endlessnet/clientapi/v1"
)

type PathCandidateStatus struct {
	Type                string  `json:"type"`
	Tier                string  `json:"tier,omitempty"`
	Priority            int     `json:"priority,omitempty"`
	State               string  `json:"state"`
	Endpoint            string  `json:"endpoint,omitempty"`
	RelayID             string  `json:"relay_id,omitempty"`
	Protocol            string  `json:"protocol,omitempty"`
	RTTMS               float64 `json:"rtt_ms,omitempty"`
	CheckedAt           string  `json:"checked_at,omitempty"`
	LastReachableAt     string  `json:"last_reachable_at,omitempty"`
	ConsecutiveFailures int     `json:"consecutive_failures,omitempty"`
	Reason              string  `json:"reason,omitempty"`
}

type PeerPathStatus struct {
	PeerID           string                `json:"peer_id"`
	Hostname         string                `json:"hostname"`
	Direct           PathCandidateStatus   `json:"direct"`
	Candidates       []PathCandidateStatus `json:"candidates,omitempty"`
	Relay            PathCandidateStatus   `json:"relay"`
	SelectedPath     string                `json:"selected_path"`
	SelectedEndpoint string                `json:"selected_endpoint,omitempty"`
	LastTransitionAt string                `json:"last_transition_at,omitempty"`
	SelectionReason  string                `json:"selection_reason,omitempty"`
}

type DirectRTTProbe struct {
	Target string
	RTTMS  float64
	Error  string
}

type DirectRTTProbeOptions struct {
	Target      string
	PingCommand string
	Runner      CommandRunner
}

func BuildPathStatusesWithDirectProbes(networkMap clientapi.RegisterNodeResponse, relayResult RelayDialResult, relayErr error, directInspection *WireGuardInspection, rttProbes map[string]DirectRTTProbe) []PeerPathStatus {
	out := make([]PeerPathStatus, 0, len(networkMap.Peers))
	relayStatus := relayCandidateStatus(networkMap, relayResult, relayErr)
	for _, peer := range networkMap.Peers {
		direct := directCandidateStatus(peer, directInspection, rttProbes)
		selectedPath := "none"
		if direct.State == "reachable" {
			selectedPath = "direct"
		} else if relayStatus.State == "reachable" {
			selectedPath = "relay"
		}
		out = append(out, PeerPathStatus{
			PeerID:       peer.ID,
			Hostname:     peer.Hostname,
			Direct:       direct,
			Relay:        relayStatus,
			SelectedPath: selectedPath,
		})
	}
	return out
}

func directCandidateStatus(peer clientapi.Peer, inspection *WireGuardInspection, rttProbes map[string]DirectRTTProbe) PathCandidateStatus {
	endpoint := strings.TrimSpace(peer.Endpoint)
	if endpoint == "" {
		for _, candidate := range peer.EndpointCandidates {
			if endpoint = strings.TrimSpace(candidate); endpoint != "" {
				break
			}
		}
	}
	if endpoint == "" {
		return PathCandidateStatus{
			Type:   "direct",
			State:  "missing",
			Reason: "peer endpoint is not published in the network map",
		}
	}
	if inspection == nil {
		return PathCandidateStatus{
			Type:     "direct",
			Tier:     directEndpointTier(peer, endpoint),
			Priority: directEndpointPriority(peer, endpoint),
			State:    "untested",
			Endpoint: endpoint,
			Reason:   "live WireGuard direct-path probing requires --wg-interface",
		}
	}
	status := PathCandidateStatus{
		Type:     "direct",
		Tier:     directEndpointTier(peer, endpoint),
		Priority: directEndpointPriority(peer, endpoint),
		State:    "failed",
		Endpoint: endpoint,
	}
	if !inspection.OK {
		status.Reason = firstNonEmptyString(inspection.Error, "live WireGuard inspection failed")
		return status
	}
	wgPeer, ok := wireGuardPeerForMapPeer(*inspection, peer)
	if !ok {
		status.Reason = "peer is not present in the live WireGuard interface"
		return status
	}
	if strings.TrimSpace(wgPeer.Endpoint) != "" {
		status.Endpoint = strings.TrimSpace(wgPeer.Endpoint)
		status.Tier = directEndpointTier(peer, status.Endpoint)
		status.Priority = directEndpointPriority(peer, status.Endpoint)
	}
	if status.Endpoint != "" && !peerDirectEndpointContains(peer, status.Endpoint) {
		status.Reason = "live WireGuard endpoint is not a signed direct candidate"
		return status
	}
	if wgPeer.LatestHandshakeUnix == 0 {
		status.Reason = "peer has no live WireGuard handshake"
		return status
	}
	target := PeerRouteTarget(peer)
	if target == "" {
		status.Reason = "peer has no route target in allowed_ips"
		return status
	}
	route, ok := routeInspectionForTarget(*inspection, target)
	if !ok {
		status.Reason = "route target was not inspected"
		return status
	}
	if route.Error != "" {
		status.Reason = route.Error
		return status
	}
	if !route.UsesInterface {
		status.Reason = "route target does not use the WireGuard interface"
		return status
	}
	if rttProbes != nil {
		probe, ok := rttProbes[target]
		if !ok {
			status.Reason = "RTT target was not probed"
			return status
		}
		if probe.Error != "" {
			status.Reason = probe.Error
			return status
		}
		status.RTTMS = probe.RTTMS
	}
	return PathCandidateStatus{
		Type:     "direct",
		Tier:     status.Tier,
		Priority: status.Priority,
		State:    "reachable",
		Endpoint: status.Endpoint,
		RTTMS:    status.RTTMS,
	}
}

func directEndpointTier(peer clientapi.Peer, endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if peerEndpointCandidateContains(peer, endpoint) && endpoint != strings.TrimSpace(peer.Endpoint) {
		if addr, ok := endpointAddr(endpoint); !ok || !addr.IsPrivate() {
			return "mapped_direct"
		}
		return "lan_direct"
	}
	return "public_direct"
}

func directEndpointPriority(peer clientapi.Peer, endpoint string) int {
	switch directEndpointTier(peer, endpoint) {
	case "lan_direct":
		return 10
	case "public_direct":
		return 20
	case "mapped_direct":
		return 30
	default:
		return 0
	}
}

func peerDirectEndpointContains(peer clientapi.Peer, endpoint string) bool {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return false
	}
	if endpoint == strings.TrimSpace(peer.Endpoint) {
		return true
	}
	return peerEndpointCandidateContains(peer, endpoint)
}

func WireGuardRouteTargetsForPeers(peers []clientapi.Peer) []string {
	out := make([]string, 0, len(peers))
	seen := map[string]bool{}
	for _, peer := range peers {
		target := PeerRouteTarget(peer)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out
}

func PeerRouteTarget(peer clientapi.Peer) string {
	for _, allowed := range peer.AllowedIPs {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(allowed); err == nil {
			if prefix.Bits() == prefix.Addr().BitLen() {
				return prefix.Addr().String()
			}
			continue
		}
		if addr, err := netip.ParseAddr(allowed); err == nil {
			return addr.String()
		}
	}
	return ""
}

func wireGuardPeerForMapPeer(inspection WireGuardInspection, peer clientapi.Peer) (WireGuardPeerInspection, bool) {
	publicKey := strings.TrimSpace(peer.PublicKey)
	if publicKey == "" {
		return WireGuardPeerInspection{}, false
	}
	for _, wgPeer := range inspection.Peers {
		if strings.TrimSpace(wgPeer.PublicKey) == publicKey {
			return wgPeer, true
		}
	}
	return WireGuardPeerInspection{}, false
}

func routeInspectionForTarget(inspection WireGuardInspection, target string) (WireGuardRouteInspection, bool) {
	for _, route := range inspection.Routes {
		if route.Target == target {
			return route, true
		}
	}
	return WireGuardRouteInspection{}, false
}

func ProbeDirectRTT(ctx context.Context, opts DirectRTTProbeOptions) DirectRTTProbe {
	target := strings.TrimSpace(opts.Target)
	result := DirectRTTProbe{Target: target}
	if target == "" {
		result.Error = "RTT target is required"
		return result
	}
	runner := opts.Runner
	if runner == nil {
		runner = runCommand
	}
	pingCommand := strings.TrimSpace(opts.PingCommand)
	if pingCommand == "" {
		pingCommand = "ping"
	}
	out, err := runner(ctx, pingCommand, "-c", "1", "-W", "1", target)
	if err != nil {
		result.Error = commandError(err, out)
		return result
	}
	rtt, ok := parsePingRTTMS(string(out))
	if !ok {
		result.Error = "ping output did not contain RTT"
		return result
	}
	result.RTTMS = rtt
	return result
}

func parsePingRTTMS(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, "time=")
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(line[idx+len("time="):])
		value = strings.TrimSuffix(value, "ms")
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		rtt, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		if err == nil {
			return rtt, true
		}
	}
	return 0, false
}

func relayCandidateStatus(networkMap clientapi.RegisterNodeResponse, relayResult RelayDialResult, relayErr error) PathCandidateStatus {
	if len(networkMap.Relays) == 0 {
		return PathCandidateStatus{
			Type:     "relay",
			Priority: 40,
			State:    "missing",
			Reason:   "network map does not contain relay endpoints",
		}
	}
	if relayResult.Selected != nil {
		return PathCandidateStatus{
			Type:     "relay",
			Priority: RelayEndpointPriority(*relayResult.Selected),
			State:    "reachable",
			Endpoint: relayResult.Selected.Addr,
			RelayID:  relayResult.Selected.ID,
			Protocol: relayProtocol(relayResult.Selected.Protocol),
		}
	}
	reason := "relay probing was not run"
	if relayErr != nil {
		reason = relayErr.Error()
	}
	return PathCandidateStatus{
		Type:     "relay",
		Priority: 40,
		State:    "failed",
		Reason:   reason,
	}
}

func relayProtocol(protocol string) string {
	return protocol
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
