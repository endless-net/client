package client

import (
	"context"
	"errors"
)

// exitProtectionAuthorityUnavailableError is returned only after the owned
// firewall's closed state has been observed. It does not authorize control I/O,
// tunnel application, or release; it permits the host to keep local recovery
// RPC available while the protected runtime remains unavailable.
type exitProtectionAuthorityUnavailableError struct{ cause error }

func (e *exitProtectionAuthorityUnavailableError) Error() string {
	return "exit protection restored without control authority"
}

func (e *exitProtectionAuthorityUnavailableError) Unwrap() error { return e.cause }

// ExitProtectionRestoredWithoutAuthority classifies a RestoreExitProtection
// result for startup admission. A true result permits serving authenticated
// local recovery RPC only; it is not exit readiness or permission to use an
// ordinary control transport. Cancellation and unconfirmed containment remain
// startup failures.
func ExitProtectionRestoredWithoutAuthority(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var unavailable *exitProtectionAuthorityUnavailableError
	return errors.As(err, &unavailable)
}
