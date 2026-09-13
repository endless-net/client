package tests

import (
	"slices"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
)

// A denied transport probe must still be a well-formed native CLI invocation.
// Otherwise invalid arguments can prevent it from ever testing OS socket access.
func installedPermissionProbeArguments(operation string, status *ipc.Status) []string {
	args := []string{"service", operation, "--timeout", "2s"}
	requestIDs := map[string]string{
		"connect":      "70e10000-0000-4000-8000-000000000001",
		"disconnect":   "70e10000-0000-4000-8000-000000000002",
		"local-forget": "70e10000-0000-4000-8000-000000000003",
	}
	if requestID := requestIDs[operation]; requestID != "" {
		args = append(args, testclient.NativeMutationArguments(requestID, status)...)
	}
	if operation == "local-forget" {
		args = append(args, "--confirm-local-forget")
	}
	return args
}

func TestInstalledPermissionProbesCarryNativeMutationContext(t *testing.T) {
	status := &ipc.Status{ActiveProfileId: "profile", Metadata: &ipc.SnapshotMetadata{InstanceId: "instance", Revision: 17}}
	seen := map[string]bool{}
	for _, operation := range []string{"status", "connect", "disconnect", "local-forget"} {
		args := installedPermissionProbeArguments(operation, status)
		if operation == "status" {
			if !slices.Equal(args, []string{"service", "status", "--timeout", "2s"}) {
				t.Fatal("observer probe acquired mutation arguments")
			}
			continue
		}
		for _, pair := range [][2]string{{"--profile-id", "profile"}, {"--expected-instance-id", "instance"}, {"--expected-revision", "17"}} {
			index := slices.Index(args, pair[0])
			if index < 0 || index+1 >= len(args) || args[index+1] != pair[1] {
				t.Fatal("permission probe lost native preconditions")
			}
		}
		index := slices.Index(args, "--request-id")
		if index < 0 || index+1 >= len(args) || len(args[index+1]) != 36 || seen[args[index+1]] {
			t.Fatal("permission probes require distinct retained request IDs")
		}
		seen[args[index+1]] = true
		if slices.Contains(args, "--confirm-local-forget") != (operation == "local-forget") {
			t.Fatal("local-forget confirmation did not match the probe")
		}
	}
}
