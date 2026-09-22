//go:build linux

package client

import (
	"context"
	"errors"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestNativeExitLANSourceRejectsInvalidScopeAndCancellation(t *testing.T) {
	if _, err := captureNativeExitLANSource(t.Context(), "endlessnet", api.ExitFamilyMode("unknown")); !errors.Is(err, errExitLANSource) {
		t.Fatal("unknown native family accepted", err)
	}
	// No host commands, interfaces or sysfs files may be touched on these paths.
	for _, name := range []string{"", ".", "..", "../eth0", "/eth0", " eth0", "lo"} {
		if _, err := captureNativeExitLANSource(t.Context(), name, api.ExitFamilyDualStack); !errors.Is(err, errExitLANSource) {
			t.Fatal("invalid native scope accepted", name, err)
		}
		if _, _, err := inspectExitLANPhysical(t.Context(), name); !errors.Is(err, errExitLANSource) {
			t.Fatal("invalid sysfs scope accepted", name, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := captureNativeExitLANSource(ctx, "endlessnet", api.ExitFamilyDualStack); !errors.Is(err, context.Canceled) {
		t.Fatal("native collection ignored cancellation", err)
	}
	if _, _, err := inspectExitLANPhysical(ctx, "eth0"); !errors.Is(err, context.Canceled) {
		t.Fatal("native sysfs inspection ignored cancellation", err)
	}
}
