//go:build !windows

package tests

import "testing"

func assertInstalledPeerDenied(t *testing.T, binary string) {
	t.Helper()
	assertInstalledUnixPeerDenied(t, binary)
}
