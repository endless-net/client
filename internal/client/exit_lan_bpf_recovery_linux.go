//go:build linux

package client

import (
	"context"
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// Caller must preserve the original durable protection scope and effect lock.
func newNativeExitLANBPFRecovery(ctx context.Context, guard *linuxExitGuard, owned *exitLANOwnership) (*exitLANBPFRecovery, error) {
	if guard == nil {
		return nil, errExitLANBPF
	}
	var order binary.ByteOrder = binary.LittleEndian
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		order = binary.BigEndian
	}
	return recoverExitLANBPF(ctx, owned, guard.ObserveContained, exitLANBPFRecoveryOps{
		namespace: newNativeExitLANBootNamespace,
		directory: newNativeExitLANBPFDirectory,
		pins: func(ctx context.Context, directory *exitLANBPFDirectory, owned *exitLANOwnership) (*exitLANBPFOwnedPins, error) {
			return openExitLANBPFOwnedPins(ctx, directory, owned, order, callExitLANBPF, unix.Close)
		},
	})
}
