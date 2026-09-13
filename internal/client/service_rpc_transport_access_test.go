//go:build windows || linux || darwin

package client

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// A real OS channel cannot assume the test runner is an ordinary user.
// Administrators are authorized before resource lookup; ordinary unowned callers
// must be rejected before lookup. Never accept either error interchangeably.
func rpcUnownedMissingResourceFailure(t *testing.T, info *ipc.RuntimeInfo) ipc.ErrorCode {
	t.Helper()
	switch info.GetCallerAccess() {
	case ipc.Access_ACCESS_OBSERVER:
		return ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
	case ipc.Access_ACCESS_ADMINISTRATOR:
		return ipc.ErrorCode_ERROR_CODE_NOT_FOUND
	default:
		t.Fatal("fresh unowned runtime exposed an unexpected caller access")
		return ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
	}
}

func assertRPCAccessAfterOwnerReplacement(t *testing.T, initial *ipc.RuntimeInfo, err error) {
	t.Helper()
	if initial.GetCallerAccess() == ipc.Access_ACCESS_ADMINISTRATOR {
		if err != nil {
			t.Fatal("administrator lost contract-authorized access after owner replacement")
		}
		return
	}
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
}
