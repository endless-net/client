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
