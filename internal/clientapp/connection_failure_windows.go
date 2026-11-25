//go:build windows

package clientapp

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	"golang.org/x/sys/windows"
)

func (s *Signals) windowsNetworkFailure(generation uint64, stage string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	class := connectionFailureCode(err)
	switch {
	case errors.Is(err, dataplane.ErrAdapterCleanupRequired):
		class = "adapter_still_present"
	case errors.Is(err, dataplane.ErrPolicyCleanupRequired):
		class = "policy_cleanup_required"
	case errors.Is(err, dataplane.ErrNoAuthority):
		class = "no_authority"
	case errors.Is(err, connectivity.ErrSourceChanged):
		class = "source_changed"
	case errors.Is(err, connectivity.ErrResync):
		class = "resync_required"
	case errors.Is(err, dataplane.ErrClosed):
		class = "closed"
	case errors.Is(err, connectivity.ErrConfig), errors.Is(err, dataplane.ErrConfiguration):
		class = "invalid_configuration"
	case errors.Is(err, state.ErrStale):
		class = "stale_state"
	case errors.Is(err, dataplane.ErrAdapterPermission), errors.Is(err, dataplane.ErrIPPermission), errors.Is(err, dataplane.ErrPolicyPermission), errors.Is(err, windows.ERROR_ACCESS_DENIED):
		class = "permission_denied"
	case errors.Is(err, dataplane.ErrAdapterOwnership), errors.Is(err, dataplane.ErrIPOwnership), errors.Is(err, dataplane.ErrPolicyOwnership):
		class = "ownership_changed"
	case errors.Is(err, dataplane.ErrAdapterUncertain), errors.Is(err, dataplane.ErrIPUncertain), errors.Is(err, dataplane.ErrPolicyUncertain):
		class = "outcome_uncertain"
	case errors.Is(err, dataplane.ErrAdapterConflict), errors.Is(err, dataplane.ErrIPConflict):
		class = "resource_conflict"
	}
	// Stage labels come from the native call sites. Raw errors may contain paths,
	// addresses or identifiers, so the diagnostic records only a bounded class
	// and a typed Windows code if the backend preserved one.
	fields := []any{"component", "connection", "generation", generation, "operation", stage, "outcome", "failure", "error_code", class}
	deadline := errors.Is(err, context.DeadlineExceeded)
	var cleanup interface{ WFPCleanupStep() (string, bool) }
	if errors.As(err, &cleanup) {
		step, exhausted := cleanup.WFPCleanupStep()
		fields = append(fields, "wfp_step", step)
		deadline = deadline || exhausted
	}
	if deadline && class != "deadline" {
		fields = append(fields, "deadline_exceeded", true)
	}
	var code windows.Errno
	if errors.As(err, &code) {
		fields = append(fields, "win32_code", uint32(code))
	}
	s.logger.Warn("client.connection.native_failure", fields...)
}

func windowsCleanupFailure(err error) error {
	if dataplane.CanRetryCleanup(err) {
		return errRetryCleanup
	}
	return nativeConnectionFailure(err, connectivity.FailureCleanup)
}
