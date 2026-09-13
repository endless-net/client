package tests

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
)

// A diagnostics read can reject a snapshot changed during observation. Repeat
// only that typed outcome, with the same profile; callers still validate all
// identity/intent/content assertions on the successful, newly collected result.
func retryNativeDiagnosticsRead(read func(*ipc.GetDiagnosticsResponse) error) (*ipc.GetDiagnosticsResponse, error) {
	for attempt := 0; ; attempt++ {
		response := &ipc.GetDiagnosticsResponse{}
		err := read(response)
		if err == nil {
			return response, nil
		}
		if attempt == 2 || !testclient.IsNativeStaleState(err) {
			return nil, err
		}
	}
}

func TestNativeDiagnosticsReadRetryBoundaries(t *testing.T) {
	stale := testclient.NativeServiceCommandError("diagnostics", []byte(rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE).Error()))
	denied := testclient.NativeServiceCommandError("diagnostics", []byte(rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED).Error()))
	uncertain := errors.New("unclassified read failure")
	for _, tc := range []struct {
		name      string
		failure   error
		successAt int
		calls     int
	}{
		{"immediate", nil, 1, 1},
		{"fresh_snapshot", stale, 2, 2},
		{"exhausted", stale, 0, 3},
		{"denied", denied, 0, 1},
		{"uncertain", uncertain, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			seen := map[*ipc.GetDiagnosticsResponse]bool{}
			result, err := retryNativeDiagnosticsRead(func(response *ipc.GetDiagnosticsResponse) error {
				calls++
				if seen[response] || response.Diagnostics != nil {
					t.Fatal("retry reused a rejected response")
				}
				seen[response] = true
				response.Diagnostics = &ipc.Diagnostics{Status: &ipc.Status{MapRevision: uint64(calls)}}
				if calls == tc.successAt {
					return nil
				}
				return tc.failure
			})
			if calls != tc.calls {
				t.Fatalf("read calls=%d, want %d", calls, tc.calls)
			}
			if tc.successAt != 0 {
				if err != nil || result.GetDiagnostics().GetStatus().GetMapRevision() != uint64(tc.successAt) {
					t.Fatal("fresh result lost")
				}
			} else if err != tc.failure || result != nil {
				t.Fatal("failure or rejected response was masked")
			}
		})
	}
}
