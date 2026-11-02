//go:build windows

package clientapp

import (
	"context"

	"github.com/endless-net/client/internal/state"
)

// InitializeWindowsState is an explicit, offline data initialization operation.
// It does not create a directory, alter its permissions, start a service, or
// replace any existing state. Startup uses the direct Upgrade followed by Load.
func InitializeWindowsState(ctx context.Context, config Config) (err error) {
	if config.Validate() != nil {
		return ErrConfig
	}
	ctx, cancel := context.WithTimeout(ctx, config.StartTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return ErrProtectedState
	}
	protector, err := state.NewDPAPI(config.InstallationID)
	if err != nil {
		return ErrProtectedState
	}
	backend, err := state.OpenWindows(config.StateDirectory, config.ServiceSID, config.StateLockRetry)
	if err != nil {
		return ErrProtectedState
	}
	defer func() {
		if backend.Close() != nil {
			err = ErrProtectedState
		}
	}()
	store, err := state.New(backend, protector, config.InstallationID, config.MaxStatePlainBytes, config.MaxStateCipherBytes)
	if err != nil || store.Initialize(ctx) != nil {
		return ErrProtectedState
	}
	return nil
}
