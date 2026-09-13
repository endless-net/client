package main

import (
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// An unavailable control plane does not imply that the applied dataplane stopped.
// The caller must bind inspection to the verified map's network, node and revision.
// Explicit transition/disconnected phases are never overridden by this observation.
func agentRPCObservedDataplanePhase(status *ipc.Status, inspection client.WireGuardInspection, available bool) ipc.ConnectionPhase {
	phase := status.GetConnectionPhase()
	if phase != ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED || status.GetActiveProfileId() == "" || status.GetUserDisconnected() ||
		status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED || !status.GetStoredState().GetCachedMapValid() ||
		!available || !inspection.OK || inspection.Error != "" {
		return phase
	}
	return ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED
}
