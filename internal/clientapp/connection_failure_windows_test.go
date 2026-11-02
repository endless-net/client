//go:build windows

package clientapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
)

func TestNativeConnectionFailuresKeepSafeActionableCategory(t *testing.T) {
	for _, tc := range []struct {
		native   error
		fallback connectivity.FailureKind
		want     error
	}{
		{dataplane.ErrAdapterPermission, connectivity.FailureApply, connectivity.FailurePermission},
		{dataplane.ErrIPPermission, connectivity.FailureApply, connectivity.FailurePermission},
		{dataplane.ErrPolicyPermission, connectivity.FailureApply, connectivity.FailurePermission},
		{dataplane.ErrIPConflict, connectivity.FailureApply, connectivity.FailureConflict},
		{dataplane.ErrPolicyOwnership, connectivity.FailureCleanup, connectivity.FailureConflict},
		{dataplane.ErrPolicyUncertain, connectivity.FailureCleanup, connectivity.FailureCleanup},
		{dataplane.ErrEngine, connectivity.FailureApply, connectivity.FailureApply},
		{context.DeadlineExceeded, connectivity.FailurePath, connectivity.FailurePath},
		{connectivity.ErrRevoked, connectivity.FailureApply, connectivity.FailureRevoked},
		{connectivity.ErrTrust, connectivity.FailureApply, connectivity.FailureTrust},
		{connectivity.ErrCredentialExpired, connectivity.FailureApply, connectivity.FailureCredential},
		{connectivity.ErrClock, connectivity.FailureApply, connectivity.ErrClock},
	} {
		got := nativeConnectionFailure(errors.Join(tc.native, errors.New("synthetic-secret backend response")), tc.fallback)
		if got != tc.want || strings.Contains(got.Error(), "synthetic-secret") || connectionOperationError(got) != tc.want {
			t.Fatal("classification or sanitization lost through worker", got)
		}
	}
}
