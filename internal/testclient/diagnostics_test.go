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
		"Native exit apply: engine configuration started",
		"Native exit apply: durable operation validation failed", "Native exit apply: durable operation validated",
		"Native exit apply: context canceled before guard", "Native exit apply: guard acquisition started",
		"Native exit apply: guard acquisition failed",
		"Native exit guard: ownership validation failed", "Native exit guard: protection scope missing",
		"Native exit guard: operation id missing", "Native exit guard: profile id missing",
		"Native exit guard: owner id missing", "Native exit guard: node id missing",
		"Native exit guard: network id missing", "Native exit guard: interface name invalid",
		"Native exit guard: loopback interface rejected", "Native exit guard: interface name whitespace rejected",
		"Native exit guard: route table validation failed",
		"Native exit guard: runtime interface conflict", "Native exit guard: platform guard creation failed",
		"Native exit guard: platform guard validation failed", "Native exit guard: runtime identity conflict",
		"Native exit guard: existing guard validated", "Native exit guard: platform guard validated",
		"Native exit plan: connected intent missing", "Native exit plan: durable journal missing",
		"Native exit plan: operation identity mismatch", "Native exit plan: journal lifecycle mismatch",
		"Native exit plan: requested selection mismatch", "Native exit plan: protection scope mismatch",
		"Native exit plan: LAN cleanup journal remains", "Native exit plan: operation journal decode failed",
		"Native exit plan: operation journal is not running", "Native exit plan: operation binding mismatch",
		"Native exit plan: request owner missing", "Native exit plan: operation request owner mismatch",
		"Native exit plan: operation journal not found",
		"WireGuard engine: exit lock acquisition started", "WireGuard engine: exit lock acquired",
		"WireGuard engine: exit configure started", "WireGuard engine: exit configure context canceled",
		"WireGuard engine: exit guard containment started", "WireGuard engine: exit guard containment failed",
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
