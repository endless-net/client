package testclient

import (
	"context"
	"errors"
	"strings"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Both calls share the caller's diagnostic deadline. Only fixed public stage
// labels escape this function, never subprocess output or arbitrary log text.
func nativeStartupStages(ctx context.Context, call func(context.Context, string, ...string) ([]byte, error)) ([]string, error) {
	unavailable := errors.New("public startup-stage log unavailable")
	out, err := call(ctx, "status")
	status := &ipc.GetStatusResponse{}
	if err != nil || decodeNativeService(out, status) != nil {
		return nil, unavailable
	}
	profile := status.GetStatus().GetActiveProfileId()
	if profile == "" {
		return nil, unavailable
	}
	out, err = call(ctx, "logs-recent", "--profile-id", profile, "--page-size", "500")
	logs := &ipc.ListRecentLogsResponse{}
	if err != nil || decodeNativeService(out, logs) != nil {
		return nil, unavailable
	}
	var stages []string
	for _, entry := range logs.Logs {
		if stage := wireGuardStartupStage(entry.GetMessage()); stage != "" {
			stages = append(stages, stage)
		}
	}
	return stages, nil
}

// nativeExitOperationStagesFromOutput emits only exact fixed markers from the
// bounded process-output snapshot. All other content stays private.
func nativeExitOperationStagesFromOutput(output []byte) []string {
	var stages []string
	for _, line := range strings.Split(string(output), "\n") {
		if stage := nativeExitOperationStage(strings.TrimSpace(line)); stage != "" {
			stages = append(stages, stage)
		}
	}
	return stages
}

func nativeExitOperationStage(message string) string {
	switch message {
	case "Native exit apply: begin", "Native exit apply: guard validated",
		"Native exit apply: engine configured", "Native exit apply: observation confirmed",
		"Native exit apply: durable operation validation failed", "Native exit apply: durable operation validated",
		"Native exit apply: context canceled before guard", "Native exit apply: guard acquisition started",
		"Native exit apply: guard acquisition failed",
		"Native exit guard: ownership validation failed", "Native exit guard: route table validation failed",
		"Native exit guard: runtime interface conflict", "Native exit guard: platform guard creation failed",
		"Native exit guard: platform guard validation failed", "Native exit guard: runtime identity conflict",
		"Native exit guard: existing guard validated", "Native exit guard: platform guard validated",
		"Native exit plan: connected intent missing", "Native exit plan: durable journal missing",
		"Native exit plan: operation identity mismatch", "Native exit plan: journal lifecycle mismatch",
		"Native exit plan: requested selection mismatch", "Native exit plan: protection scope mismatch",
		"Native exit plan: LAN cleanup journal remains", "Native exit plan: operation journal decode failed",
		"Native exit plan: operation journal is not running", "Native exit plan: operation binding mismatch",
		"Native exit plan: operation owner mismatch", "Native exit plan: operation journal not found",
		"WireGuard engine: exit guard contained", "WireGuard engine: exit LAN cleanup complete",
		"WireGuard engine: exit underlay capture complete", "WireGuard engine: exit runtime configure complete",
		"WireGuard engine: exit routes confirmed", "WireGuard engine: exit policy opened":
		return message
	case "WireGuard engine: tun-create begin", "WireGuard engine: tun-create complete",
		"WireGuard engine: device-up begin", "WireGuard engine: device-up complete",
		"WireGuard engine: routes begin", "WireGuard engine: routes complete":
		return message
	default:
		return ""
	}
}
