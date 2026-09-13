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

func TestNativeMutationFailureEnvelopeIsExactAndRedacted(t *testing.T) {
	const id = "5c110000-0000-4000-8000-000000000002"
	line := rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE).Error()
	wrapped := "service trust-server: " + line + "; inspect service operation --request-id " + id + " using the same endpoint before deciding whether to retry"
	want := fmt.Sprintf("native service trust-server failed (transport=%d failure=%d; output withheld)", connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if got := NativeServiceCommandError("trust-server", []byte(wrapped+"\n")).Error(); got != want {
		t.Fatal("canonical mutation failure envelope was not classified")
	}
	for _, invalid := range []string{
		strings.Replace(wrapped, "service trust-server:", "service logout:", 1),
		strings.Replace(wrapped, id, "synthetic-private", 1),
		strings.Replace(wrapped, id, id+"x", 1),
		wrapped + " synthetic-private", "synthetic-private " + wrapped,
		wrapped + "\n" + wrapped,
		strings.Replace(wrapped, line, line+"\n", 1),
	} {
		if got := NativeServiceCommandError("trust-server", []byte(invalid)).Error(); got != "native service trust-server failed (unclassified subprocess failure; output withheld)" {
			t.Fatal("noncanonical mutation output was accepted or exposed")
		}
	}
}
