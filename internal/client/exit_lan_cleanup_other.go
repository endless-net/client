//go:build !linux

package client

import "context"

func cleanupNativeExitLAN(context.Context, *linuxExitGuard, *exitLANOwnership) error {
	return errExitLANBPF
}
