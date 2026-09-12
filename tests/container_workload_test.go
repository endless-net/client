package tests

import (
	"os"
	"testing"
)

// HC-054: the dedicated CI job runs this process and its real Client inside a
// privileged container. Persistent and ephemeral workloads have deliberately
// different identity lifecycles and both prove application traffic.
func TestContainerWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("container workload acceptance is a dedicated CI job")
	}
	if os.Getenv("ENDLESSNET_CONTAINER_TEST") != "1" {
		if os.Getenv("GITHUB_ACTIONS") != "true" {
			t.Skip("requires explicit container acceptance opt-in")
		}
		t.Fatal("container workload acceptance requires its dedicated CI environment")
	}
	requireControlScenario(t)
	t.Run("persistent-state-restart", func(t *testing.T) {
		for _, family := range []string{"ipv4", "ipv6"} {
			for _, protocol := range []string{"tcp", "udp"} {
				t.Run(family+"/"+protocol, func(t *testing.T) {
					exerciseNativeTrafficScenario(t, family == "ipv6", protocol, false, "", true)
				})
			}
		}
	})
	t.Run("ephemeral-recreation", func(t *testing.T) {
		exerciseEphemeralLifecycle(t)
	})
}
