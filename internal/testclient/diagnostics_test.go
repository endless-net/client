package testclient

import (
	"testing"

	ipc "github.com/endless-net/client/ipc/v2"
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

func TestWireGuardInspectionDiagnosticLabels(t *testing.T) {
	for _, tc := range []struct {
		name       string
		inspection *ipc.WireGuardInspection
		want       string
	}{
		{"absent", nil, "absent"},
		{"ready", &ipc.WireGuardInspection{OK: true}, "ready"},
		{"not-ready", &ipc.WireGuardInspection{}, "not-ready"},
		{"not-running", &ipc.WireGuardInspection{Error: "wireguard-go is not running"}, "not-running"},
		{"busy", &ipc.WireGuardInspection{Error: "wireguard-go operation in progress; inspection unavailable"}, "operation-in-progress"},
		{"unknown", &ipc.WireGuardInspection{Error: "sensitive fixture context"}, "other-error-withheld"},
		{"known-prefix-with-context", &ipc.WireGuardInspection{Error: "wireguard-go is not running: sensitive fixture context"}, "other-error-withheld"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wireGuardInspectionState(tc.inspection); got != tc.want {
				t.Fatalf("diagnostic label = %q, want %q", got, tc.want)
			}
		})
	}
}
