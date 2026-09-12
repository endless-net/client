package tests

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This checks installed Unix transport permissions, not IPC application roles.
// The pre-existing nobody account needs no password or host account changes.
func assertInstalledUnixPeerDenied(t *testing.T, binary string) {
	t.Helper()
	uidOutput := command(t, "sudo", "-n", "-u", "nobody", "--", "id", "-u")
	uid, err := strconv.ParseUint(strings.TrimSpace(string(uidOutput)), 10, 32)
	if err != nil || uid == 0 {
		t.Fatal("Unix authorization fixture did not select a non-root peer")
	}
	version := command(t, "sudo", "-n", "-u", "nobody", "--", binary, "version")
	if !strings.HasPrefix(string(version), "endlessnet-client ") {
		t.Fatal("unprivileged peer could not execute the installed CLI")
	}
	for _, operation := range []string{"status", "connect", "disconnect", "local-forget"} {
		args := []string{"-n", "-u", "nobody", "--", binary, "service", operation, "--timeout", "2s"}
		if operation == "local-forget" {
			args = append(args, "--confirm-local-forget")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "sudo", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		deadlineErr := ctx.Err()
		cancel()
		var exit *exec.ExitError
		if deadlineErr != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || !strings.Contains(strings.ToLower(stderr.String()), "permission denied") {
			t.Fatalf("unprivileged Unix %s did not fail at the protected IPC transport (output withheld)", operation)
		}
	}
}
