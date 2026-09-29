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

func TestExitOperationStageLogsWithholdUnrecognizedContext(t *testing.T) {
	stages := []string{
		"Native exit apply: begin", "Native exit apply: guard validated",
		"Native exit apply: engine configured", "Native exit apply: observation confirmed",
		"WireGuard engine: exit guard contained", "WireGuard engine: exit LAN cleanup complete",
		"WireGuard engine: exit underlay capture complete", "WireGuard engine: exit runtime configure complete",
		"WireGuard engine: exit routes confirmed", "WireGuard engine: exit policy opened",
		"WireGuard engine: routes begin", "WireGuard engine: routes complete",
	}
	for _, message := range stages {
		if got := nativeExitOperationStage(message); got != message {
			t.Fatalf("fixed public stage %q was not classified", message)
		}
		for _, unknown := range []string{message + " private context", "private context " + message, "unrelated message"} {
			if nativeExitOperationStage(unknown) != "" {
				t.Fatalf("unrecognized log context escaped: %q", unknown)
			}
		}
	}
	got := nativeExitOperationStagesFromOutput([]byte("WireGuard engine: exit guard contained\nprivate WireGuard engine: exit policy opened"))
	if len(got) != 1 || got[0] != "WireGuard engine: exit guard contained" {
		t.Fatal("exit stage output parser did not retain only exact public markers")
	}
}
