package client

import (
	"context"
	"fmt"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type clientRPCScopedLog struct {
	profileID string
	entry     *ipc.LogEntry
}

// recordDiagnosticTransitionLocked runs only after durable acceptance/update or
// accepted runtime observation. Attribution comes from the committed operation
// or observation, never from whichever profile happens to be active when a
// global logger writes. No arbitrary string fields enter these messages.
func (m *ClientRPCMutations) recordDiagnosticTransitionLocked(cfg Config, operation *ipc.Operation) {
	// Forget deleted profiles immediately, including when removal itself is the
	// published operation. The bound below is global, not multiplied by profiles.
	retained := m.recentLogs[:0]
	for _, item := range m.recentLogs {
		if cfg.RPCState != nil {
			if _, exists := cfg.RPCState.Profiles[item.profileID]; exists {
				retained = append(retained, item)
			}
		}
	}
	clear(m.recentLogs[len(retained):])
	m.recentLogs = retained
	if cfg.RPCState == nil {
		return
	}
	var profileID, message string
	if operation != nil {
		profileID = operation.ProfileId
		message = fmt.Sprintf("operation kind=%s state=%s continuity=%s failure=%s", operation.Kind, operation.State, operation.Continuity, operation.GetFailure().GetCode())
	} else if status := m.observedStatus; status != nil && status.ActiveProfileId == cfg.RPCState.ActiveProfileID {
		profileID = status.ActiveProfileId
		message = fmt.Sprintf("runtime service=%s control=%s phase=%s", status.ServiceState, status.ControlState, status.ConnectionPhase)
	}
	if profileID == "" {
		return
	}
	if _, exists := cfg.RPCState.Profiles[profileID]; !exists {
		return
	}
	timestamp := timestamppb.New(m.now())
	if timestamp.CheckValid() != nil {
		return
	}
	// Wall-clock adjustment must not turn an ordered buffer into an invalid
	// provider snapshot; equal timestamps still preserve append order.
	if n := len(m.recentLogs); n > 0 && timestamp.AsTime().Before(m.recentLogs[n-1].entry.Timestamp.AsTime()) {
		timestamp = proto.Clone(m.recentLogs[n-1].entry.Timestamp).(*timestamppb.Timestamp)
	}
	if len(m.recentLogs) == maxRPCRecentLogs {
		copy(m.recentLogs, m.recentLogs[1:])
		m.recentLogs = m.recentLogs[:maxRPCRecentLogs-1]
	}
	m.recentLogs = append(m.recentLogs, clientRPCScopedLog{profileID: profileID, entry: &ipc.LogEntry{Timestamp: timestamp, Message: message}})
}

// recentLogsSnapshot is process-local diagnostic history, not an operation
// recovery journal. It is empty after restart; durable recovery uses GetOperation.
func (m *ClientRPCMutations) recentLogsSnapshot(ctx context.Context, profileID string) ([]*ipc.LogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []*ipc.LogEntry
	for _, item := range m.recentLogs {
		if item.profileID == profileID {
			result = append(result, proto.Clone(item.entry).(*ipc.LogEntry))
		}
	}
	return result, nil
}
