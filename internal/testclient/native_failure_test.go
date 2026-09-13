package testclient

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeCommandFailureClassificationIsExactAndRedacted(t *testing.T) {
	for _, code := range []connect.Code{connect.CodePermissionDenied, connect.CodeFailedPrecondition, connect.CodeUnavailable, connect.CodeUnimplemented} {
		for _, failure := range []ipc.ErrorCode{ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED, ipc.ErrorCode_ERROR_CODE_STALE_STATE, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED} {
			line := rpc.Error(code, failure).Error()
			want := fmt.Sprintf("native service diagnostics failed (transport=%d failure=%d; output withheld)", code, failure)
			if got := NativeServiceCommandError("diagnostics", []byte(line+"\n")).Error(); got != want {
				t.Fatal("canonical typed failure was not classified")
			}
			for _, output := range []string{"synthetic-private " + line, line + " synthetic-private", line + "\n" + line, strings.Repeat("x", 257)} {
				got := NativeServiceCommandError("diagnostics", []byte(output)).Error()
				if got != "native service diagnostics failed (unclassified subprocess failure; output withheld)" {
					t.Fatal("arbitrary process output was exposed or interpreted as a canonical failure")
				}
			}
		}
	}
}
