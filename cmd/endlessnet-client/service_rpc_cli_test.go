package main

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/endless-net/client/clientipc/local"
)

func TestNativeServiceQueryRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"unexpected"}, {"--timeout", "0s"}, {"--timeout", "invalid"},
		{"--ipc-pipe", "pipe", "--ipc-socket", "socket"}, {"--unknown"},
	} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery("status", args, &output); err == nil || output.Len() != 0 {
			t.Fatal("invalid CLI input succeeded or emitted a response", args)
		}
	}
}

func TestNativeServiceOperationRequiresOneLookup(t *testing.T) {
	for _, args := range [][]string{nil, {"--request-id", " "}, {"--request-id", "request", "--operation-id", "operation"}} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery("operation", args, &output); err == nil || output.Len() != 0 {
			t.Fatal("ambiguous or empty lookup accepted", args)
		}
	}
}

func TestNativeServiceEndpointUsesPlatformDefaults(t *testing.T) {
	want := map[string]string{"windows": local.DefaultWindowsPipe, "linux": local.DefaultUnixSocket, "darwin": local.DefaultDarwinSocket}[runtime.GOOS]
	got, err := nativeServiceEndpoint("", "")
	if want == "" {
		if err == nil {
			t.Fatal("unsupported platform accepted")
		}
		return
	}
	if err != nil || got != want {
		t.Fatal("wrong platform endpoint", got, err)
	}
	pipe, socket := "wrong-platform-pipe", ""
	if runtime.GOOS == "windows" {
		pipe, socket = "", "/wrong-platform.sock"
	}
	if _, err := nativeServiceEndpoint(pipe, socket); err == nil {
		t.Fatal("wrong-platform endpoint accepted")
	}
}
