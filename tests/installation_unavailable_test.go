package tests

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// HC-004/052/053: automation must distinguish an unavailable installed agent
// from successful reads or mutations. Exercise the real CLI and OS IPC, without
// inspecting state files or relying on platform-specific transport messages.
func assertStoppedServiceCommands(t *testing.T, binary string) {
	t.Helper()
	for _, operation := range []string{"status", "networks", "diagnostics", "connect", "disconnect"} {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		cmd := exec.CommandContext(ctx, binary, "service", operation, "--timeout", "2s")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		deadlineErr := ctx.Err()
		cancel()
		var exit *exec.ExitError
		if deadlineErr != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("stopped-service %s did not return CLI failure within the harness deadline (output withheld)", operation)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("stopped-service %s must report an error on stderr without a success payload on stdout (output withheld)", operation)
		}
	}
}
