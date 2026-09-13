package main

import (
	"bytes"
	"runtime"
	"strings"
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

func TestNativeMutationRequiresDurableIdentityAndCAS(t *testing.T) {
	for _, command := range []string{"connect", "disconnect", "logout", "create-profile", "select-profile", "rename-profile", "remove-profile"} {
		for _, args := range [][]string{nil, {"--request-id", "bad"}, {"--request-id", "00000000-0000-0000-0000-000000000000"}, {"--expected-revision", "-1"}} {
			var output bytes.Buffer
			if err := cmdServiceRPCMutation(command, args, &output); err == nil || output.Len() != 0 {
				t.Fatal("invalid mutation accepted", command, args)
			}
		}
	}
}

func TestNativeLocalForgetRequiresExplicitConfirmation(t *testing.T) {
	for _, args := range [][]string{nil, {"--confirm-local-forget=false"}} {
		var output bytes.Buffer
		err := cmdServiceRPCMutation("local-forget", args, &output)
		if err == nil || !strings.Contains(err.Error(), "requires --confirm-local-forget") || output.Len() != 0 {
			t.Fatal("unconfirmed cleanup reached dispatch", err)
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
