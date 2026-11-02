//go:build windows

package clientapp

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
)

// nativeConnectionFailure discards raw backend diagnostics. Keep only known
// actionable classifications, with no arbitrary message or native code payload.
func nativeConnectionFailure(err error, fallback connectivity.FailureKind) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, dataplane.ErrAdapterPermission), errors.Is(err, dataplane.ErrIPPermission), errors.Is(err, dataplane.ErrPolicyPermission):
		return connectivity.FailurePermission
	case errors.Is(err, dataplane.ErrAdapterConflict), errors.Is(err, dataplane.ErrIPConflict), errors.Is(err, dataplane.ErrAdapterOwnership), errors.Is(err, dataplane.ErrIPOwnership), errors.Is(err, dataplane.ErrPolicyOwnership):
		return connectivity.FailureConflict
	case errors.Is(err, connectivity.ErrClock):
		return connectivity.ErrClock
	case errors.Is(err, connectivity.ErrDenied), errors.Is(err, connectivity.ErrRevoked), errors.Is(err, connectivity.ErrExpired):
		return connectivity.FailureAccess
	case errors.Is(err, state.ErrStale):
		return state.ErrStale
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		if fallback == connectivity.FailurePath {
			return connectivity.FailurePath
		}
		return context.DeadlineExceeded
	default:
		return fallback
	}
}
