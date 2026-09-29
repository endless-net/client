package testclient

import (
	"context"
	"errors"

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

// nativeExitOperationStages returns only exact, fixed runtime markers. The
// native log payload and all unrecognized messages remain private to the test
// process.
func nativeExitOperationStages(ctx context.Context, call func(context.Context, string, ...string) ([]byte, error), profile string) ([]string, error) {
	if profile == "" {
		return nil, errors.New("public exit-operation log unavailable")
	}
	out, err := call(ctx, "logs-recent", "--profile-id", profile, "--page-size", "500")
	logs := &ipc.ListRecentLogsResponse{}
	if err != nil || decodeNativeService(out, logs) != nil {
		return nil, errors.New("public exit-operation log unavailable")
	}
	var stages []string
	for _, entry := range logs.Logs {
		if stage := nativeExitOperationStage(entry.GetMessage()); stage != "" {
			stages = append(stages, stage)
		}
	}
	return stages, nil
}

func nativeExitOperationStage(message string) string {
	switch message {
	case "Native exit apply: begin", "Native exit apply: guard validated",
		"Native exit apply: engine configured", "Native exit apply: observation confirmed",
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
