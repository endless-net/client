package testclient

import (
	"testing"
)

func TestStartupStageLogsWithholdUnrecognizedContext(t *testing.T) {
	for _, stage := range []string{"tun-create begin", "tun-create complete", "device-up begin", "device-up complete", "routes begin", "routes complete"} {
		message := "WireGuard engine: " + stage
		if got := wireGuardStartupStage(message); got != stage {
			t.Fatal("known public stage was not classified")
		}
		for _, unknown := range []string{message + " sensitive fixture context", "sensitive fixture context " + message, "unrelated message"} {
			if wireGuardStartupStage(unknown) != "" {
				t.Fatal("unrecognized public log context was exposed")
			}
		}
	}
}
