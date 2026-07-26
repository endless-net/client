//go:build windows

package client

import "testing"

func TestWindowsServicePipeSDDLDeniesNetworkAndLimitsInteractiveUsers(t *testing.T) {
	const want = `D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)`
	if WindowsServicePipeSDDL != want {
		t.Fatalf("WindowsServicePipeSDDL = %q, want %q", WindowsServicePipeSDDL, want)
	}
}
