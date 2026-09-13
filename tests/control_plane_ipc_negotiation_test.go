package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
)

// HC-053/HC-058: exact native admission over real platform sockets/pipes.
// Retired version-range overlap and HTTP fallback are not supported.
func TestControlPlaneIPCNegotiation(t *testing.T) {
	s, n, id := nativeControlScenario(t)
	expectedCommit := os.Getenv("ENDLESSNET_TEST_COMMIT")
	if raw, err := hex.DecodeString(expectedCommit); err != nil || len(raw) != 20 {
		t.Fatal("native build identity requires the exact CI source commit")
	}
	output, err := n.Run("version")
	if err != nil || !strings.Contains(string(output), "\ncommit: "+expectedCommit+"\n") || !strings.Contains(string(output), "\ntarget: "+runtime.GOOS+"/"+runtime.GOARCH+"\n") {
		t.Fatal("CLI build identity does not match CI source/target (output withheld)")
	}
	endpoint := n.Socket
	if runtime.GOOS == "windows" {
		endpoint = n.Pipe
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	assertRuntime := func(info *ipc.RuntimeInfo) {
		t.Helper()
		platform := map[string]ipc.Platform{"windows": ipc.Platform_PLATFORM_WINDOWS, "darwin": ipc.Platform_PLATFORM_MACOS, "linux": ipc.Platform_PLATFORM_LINUX}[runtime.GOOS]
		if info.GetBuild().GetCommit() != expectedCommit || info.GetBuild().GetArchitecture() != runtime.GOARCH || info.GetBuild().GetPlatform() != platform {
			t.Fatal("runtime build identity does not match CI source/target")
		}
		if info.GetInstanceId() == "" || info.GetProtocol() != rpc.Protocol || info.GetIpcVersion() != rpc.Version || info.GetContractSha256() != rpc.Digest() {
			t.Fatal("runtime exposed a mismatched native contract")
		}
	}
	initial := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.GetStoredState().GetCachedMapValid() })
	// Bypass only the SDK header injector, not the real platform transport or host.
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialNativeTestIPC(ctx, n.Socket, n.Pipe)
	}}
	defer transport.CloseIdleConnections()
	probe := clientipcconnect.NewClientServiceClient(&http.Client{Transport: transport}, "http://endlessnet.local", connect.WithGRPC(),
		connect.WithReadMaxBytes(rpc.MaxResponseBytes), connect.WithSendMaxBytes(rpc.MaxRequestBytes))
	type headerCase struct {
		name  string
		alter func(http.Header)
	}
	cases := []headerCase{
		{"missing-all", func(h http.Header) { clear(h) }},
		{"wrong-protocol", func(h http.Header) { h.Set(rpc.ProtocolHeader, "unsupported-ipc") }},
		{"malformed-version", func(h http.Header) { h.Set(rpc.VersionHeader, "invalid") }},
		{"other-version", func(h http.Header) { h.Set(rpc.VersionHeader, "1") }}, // Invalid probe, not a version increase.
		{"wrong-digest", func(h http.Header) { h.Set(rpc.DigestHeader, strings.Repeat("0", 64)) }},
	}
	for _, key := range []string{rpc.ProtocolHeader, rpc.VersionHeader, rpc.DigestHeader} {
		cases = append(cases,
			headerCase{"missing-" + key, func(h http.Header) { h.Del(key) }},
			headerCase{"duplicate-" + key, func(h http.Header) { h.Add(key, h.Get(key)) }})
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			set := func(h http.Header) { rpc.SetHeaders(h); tc.alter(h) }
			bootstrap := connect.NewRequest(&ipc.GetRuntimeInfoRequest{})
			set(bootstrap.Header())
			info, err := probe.GetRuntimeInfo(ctx, bootstrap)
			if err != nil {
				t.Fatal("mismatched caller could not diagnose runtime identity")
			}
			assertRuntime(info.Msg.Runtime)
			assertMismatch := func(err error) {
				t.Helper()
				if connect.CodeOf(err) != connect.CodeFailedPrecondition || rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_CONTRACT_MISMATCH {
					t.Fatal("invalid headers did not produce typed CONTRACT_MISMATCH")
				}
			}
			query := connect.NewRequest(&ipc.GetStatusRequest{})
			set(query.Header())
			_, err = probe.GetStatus(ctx, query)
			assertMismatch(err)
			before, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", i+100)
			mutation := connect.NewRequest(&ipc.DisconnectRequest{Profile: &ipc.ProfileRef{ProfileId: before.Msg.Status.ActiveProfileId},
				Mutation: &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: before.Msg.Status.GetMetadata().GetInstanceId(), ExpectedRevision: before.Msg.Status.GetMetadata().GetRevision()}})
			set(mutation.Header())
			_, err = probe.Disconnect(ctx, mutation)
			assertMismatch(err)
			_, err = consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_RequestId{RequestId: requestID}}))
			if rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_NOT_FOUND {
				t.Fatal("rejected contract admitted a mutation")
			}
			after, err := consumer.GetStatus(ctx, connect.NewRequest(&ipc.GetStatusRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			v := after.Msg.GetStatus()
			if v == nil || v.NodeId != id || v.ActiveProfileId != initial.ActiveProfileId || !v.GetStoredState().GetCachedMapValid() ||
				v.UserDisconnected != initial.UserDisconnected || v.GetIntent().GetDesiredState() != initial.GetIntent().GetDesiredState() || v.GetMetadata().GetInstanceId() != initial.GetMetadata().GetInstanceId() {
				t.Fatal("rejected contract changed enrollment, intent or host instance")
			}
		})
	}
	n.Stop()
	n.Start()
	restarted := n.AwaitNativeStatus(func(v *ipc.Status) bool { return v.NodeId == id && v.GetStoredState().GetCachedMapValid() })
	if restarted.GetMetadata().GetInstanceId() == initial.GetMetadata().GetInstanceId() {
		t.Fatal("restart reused the old host instance")
	}
	info := &ipc.GetRuntimeInfoResponse{}
	if err := n.NativeService("runtime-info", info); err != nil {
		t.Fatal(err)
	}
	assertRuntime(info.Runtime)
	runNativeControlMutation(t, n, "disconnect", "00000000-0000-4000-8000-000000000001")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_DISCONNECTED
	})
	runNativeControlMutation(t, n, "connect", "00000000-0000-4000-8000-000000000002")
	n.AwaitNativeStatus(func(v *ipc.Status) bool {
		return v.NodeId == id && !v.UserDisconnected && v.GetIntent().GetDesiredState() == ipc.DesiredState_DESIRED_STATE_CONNECTED && v.GetStoredState().GetCachedMapValid()
	})
	registered := 0
	for _, event := range s.Events() {
		if event.Kind == "registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Fatal("native probes or restart caused a new enrollment")
	}
}
