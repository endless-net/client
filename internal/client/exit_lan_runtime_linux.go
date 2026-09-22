//go:build linux

package client

import (
	"context"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func newPlatformExitLANRuntime(store *ConfigStore) *exitLANRuntime {
	return &exitLANRuntime{store: store, ops: exitLANRuntimeOps{
		clock:       captureExitLANClock,
		observeHook: newNativeExitLANHookObservation,
		inspect:     resourceObservedUAPI,
		cleanup:     cleanupNativeExitLAN,
		capture: func(ctx, lifetime context.Context, own string, family api.ExitFamilyMode) (*exitLANSource, error) {
			stream, err := openExitLANRouteWatch(lifetime)
			if err != nil {
				return nil, err
			}
			keep := false
			defer func() {
				if !keep {
					_ = stream.Close()
				}
			}()
			watch := &exitLANSourceLifetime{ctx: lifetime, stream: stream}
			if !watch.current() {
				return nil, errExitLANSource
			}
			source, err := captureExitLANSource(ctx, own, family, func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return runExitCommand(ctx, "", name, args...)
			}, inspectExitLANPhysical)
			if err != nil {
				return nil, err
			}
			if !watch.current() {
				return nil, errExitLANSource
			}
			source.lifetime = watch
			keep = true
			return source, nil
		},
		session: func(ctx context.Context, g *linuxExitGuard, plan *exitLANPlan, mark uint32, scope string, checkpoint func(context.Context, *exitLANOwnership) error) (*exitLANBPFSession, error) {
			return prepareExitLANBPFSession(ctx, mark, plan.topology.Family, exitLANPacketHook, exitLANPacketPriority, scope, checkpoint, g.ObserveContained, exitLANBPFSessionOps{
				namespace: newNativeExitLANBootNamespace, directory: newNativeExitLANBPFDirectory, observe: newNativeExitLANHookObservation,
				preparation: func(ctx context.Context, mark uint32) (*exitLANBPFPreparation, error) {
					return newNativeExitLANPacketPreparation(ctx, mark, plan)
				},
			})
		},
	}}
}
