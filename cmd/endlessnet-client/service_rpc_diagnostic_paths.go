package main

import (
	"math"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func nativeDiagnosticPaths(peers []*ipc.Peer, paths []client.PeerPathStatus) []*ipc.Failure {
	byID := map[string]*ipc.Peer{}
	for _, peer := range peers {
		byID[peer.Id] = peer
	}
	seen := map[string]bool{}
	invalid := len(paths) > 4096
	if invalid {
		paths = nil
	}
	kind := map[string]ipc.PathKind{"none": ipc.PathKind_PATH_KIND_UNSPECIFIED, "": ipc.PathKind_PATH_KIND_UNSPECIFIED, "direct": ipc.PathKind_PATH_KIND_DIRECT, "relay": ipc.PathKind_PATH_KIND_RELAY}
	health := map[string]ipc.PathHealth{"missing": ipc.PathHealth_PATH_HEALTH_UNKNOWN, "untested": ipc.PathHealth_PATH_HEALTH_UNKNOWN, "reachable": ipc.PathHealth_PATH_HEALTH_REACHABLE, "degraded": ipc.PathHealth_PATH_HEALTH_UNREACHABLE, "failed": ipc.PathHealth_PATH_HEALTH_UNREACHABLE}
	stamp := func(raw string) (*timestamppb.Timestamp, bool) {
		if raw == "" {
			return nil, true
		}
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, false
		}
		timestamp := timestamppb.New(value)
		return timestamp, timestamp.CheckValid() == nil
	}
	for _, path := range paths {
		target := byID[path.PeerID]
		selected, known := kind[path.SelectedPath]
		transition, validTime := stamp(path.LastTransitionAt)
		if target == nil || seen[path.PeerID] || !known || !validTime || len(path.Candidates) > 128 {
			invalid = true
			continue
		}
		seen[path.PeerID] = true
		candidates := append([]client.PathCandidateStatus(nil), path.Candidates...)
		if len(candidates) == 0 && path.Direct.Type != "" {
			candidates = append(candidates, path.Direct)
		}
		if path.Relay.Type != "" {
			candidates = append(candidates, path.Relay)
		}
		var converted []*ipc.PathCandidate
		valid := true
		for _, candidate := range candidates {
			candidateKind, known := kind[candidate.Type]
			state, knownState := health[candidate.State]
			checked, okChecked := stamp(candidate.CheckedAt)
			reachable, okReachable := stamp(candidate.LastReachableAt)
			if !known || candidateKind == ipc.PathKind_PATH_KIND_UNSPECIFIED || !knownState || !okChecked || !okReachable || candidate.Priority < 0 || uint64(candidate.Priority) > uint64(^uint32(0)) || candidate.ConsecutiveFailures < 0 || uint64(candidate.ConsecutiveFailures) > uint64(^uint32(0)) || math.IsNaN(candidate.RTTMS) || math.IsInf(candidate.RTTMS, 0) || candidate.RTTMS < 0 || candidate.RTTMS > 86400000 {
				valid = false
				break
			}
			native := &ipc.PathCandidate{Kind: candidateKind, Health: state, Endpoint: candidate.Endpoint, RelayId: candidate.RelayID, Protocol: candidate.Protocol, Priority: uint32(candidate.Priority), ConsecutiveFailures: uint32(candidate.ConsecutiveFailures), CheckedAt: checked, LastReachableAt: reachable, Tier: candidate.Tier, ReasonKey: "path_monitor_" + candidate.State}
			if candidate.RTTMS > 0 {
				native.Rtt = durationpb.New(time.Duration(candidate.RTTMS * float64(time.Millisecond)))
			}
			converted = append(converted, native)
		}
		if !valid {
			invalid = true
			continue
		}
		target.Candidates = converted
		target.SelectedPath = selected
		if selected != ipc.PathKind_PATH_KIND_UNSPECIFIED {
			target.SelectedEndpoint = path.SelectedEndpoint
		}
		target.LastTransitionAt = transition
		target.SelectionReasonKey = "path_monitor_selection"
	}
	if invalid {
		return []*ipc.Failure{{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "diagnostics_path_observation_invalid"}}
	}
	return nil
}
