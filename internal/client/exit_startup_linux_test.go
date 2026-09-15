//go:build linux

package client

import "testing"

func TestExitStartupGuardUsesRouterTableMark(t *testing.T) {
	for _, tc := range []struct {
		table string
		mark  uint32
	}{{"", 51820}, {"auto", 51820}, {"51999", 51999}, {"4294967295", 4294967295}} {
		t.Run(tc.table, func(t *testing.T) {
			// Construct only: no firewall or other privileged command is run.
			guard, err := newPlatformExitGuard("endlessnet", tc.table)
			if err != nil || guard.mark != tc.mark {
				t.Fatal("startup mark differs from the configured table", guard, err)
			}
		})
	}
}
