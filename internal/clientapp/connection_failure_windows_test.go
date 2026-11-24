//go:build windows

package clientapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"golang.org/x/sys/windows"
)

func TestNativeConnectionFailuresKeepSafeActionableCategory(t *testing.T) {
	for _, tc := range []struct {
		native   error
		fallback connectivity.FailureKind
		want     error
	}{
		{control.ErrNodeRenewalUnresolved, connectivity.FailureApply, connectivity.FailureNodeRenewal},
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

func TestWindowsNetworkFailureLogsStageWithoutRawError(t *testing.T) {
	signals, _, logs, _ := requestSignals(t)
	signals.windowsNetworkFailure(141, "cleanup_wfp_remove", errors.Join(dataplane.ErrPolicyPermission, windows.ERROR_ACCESS_DENIED, errors.New("synthetic-secret")))
	signals.windowsNetworkFailure(141, "apply_route", context.Canceled)
	var record struct {
		Message     string `json:"msg"`
		Operation   string `json:"operation"`
		ErrorCode   string `json:"error_code"`
		Generation  uint64 `json:"generation"`
		WindowsCode uint32 `json:"win32_code"`
	}
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Message != "client.connection.native_failure" || record.Operation != "cleanup_wfp_remove" || record.ErrorCode != "permission_denied" || record.Generation != 141 || record.WindowsCode != uint32(windows.ERROR_ACCESS_DENIED) || strings.Contains(logs.String(), "synthetic-secret") {
		t.Fatal("missing or unsafe network failure event", record)
	}
}
