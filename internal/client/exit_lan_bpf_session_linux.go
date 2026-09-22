//go:build linux

package client

import (
	"context"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// Caller owns the runtime effect lock and the guard's durable protection scope.
// The LAN marker must never equal the privileged WireGuard underlay exemption.
func newNativeExitLANBPFSession(ctx context.Context, guard *linuxExitGuard, mark uint32, mode api.ExitFamilyMode, hook uint32, priority int32, scope string, checkpoint func(context.Context, *exitLANOwnership) error) (*exitLANBPFSession, error) {
	if guard == nil || mark == guard.mark {
		return nil, errExitLANBPF
	}
	return prepareExitLANBPFSession(ctx, mark, mode, hook, priority, scope, checkpoint, guard.ObserveContained, exitLANBPFSessionOps{
		namespace:   newNativeExitLANBootNamespace,
		directory:   newNativeExitLANBPFDirectory,
		preparation: newNativeExitLANBPFPreparation,
		observe:     newNativeExitLANHookObservation,
	})
}
