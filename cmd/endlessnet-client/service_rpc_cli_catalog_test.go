//go:build windows || linux || darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	"github.com/endless-net/client/clientipc/testserver"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestNativeServiceCatalogCommandsUseExactProtobufRequests(t *testing.T) {
	endpoint := fmt.Sprintf(`\\.\pipe\en-cli-catalog-%d`, time.Now().UnixNano())
	transportFlag := "--ipc-pipe"
	if runtime.GOOS != "windows" {
		directory, err := os.MkdirTemp("/tmp", "en-cli-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(directory); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(directory, "rpc.sock")
		transportFlag = "--ipc-socket"
	}
	fixture := testserver.New()
	ref := &ipc.ProfileRef{ProfileId: "profile-a"}
	page := &ipc.PageRequest{PageSize: 2, PageToken: "opaque-page"}
	mutation := &ipc.MutationContext{RequestId: "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", ExpectedInstanceId: "instance", ExpectedRevision: 7}
	accepted := func(kind ipc.OperationKind) *ipc.Operation {
		return &ipc.Operation{Id: "operation", RequestId: mutation.RequestId, ProfileId: ref.ProfileId, Kind: kind, State: ipc.OperationState_OPERATION_STATE_PENDING}
	}
	cases := []struct {
		command, method   string
		args              []string
		request, response proto.Message
	}{
		{"peers", "ListPeers", []string{"--page-size", "2", "--page-token", "opaque-page", "--search", " HOST "}, &ipc.ListPeersRequest{Profile: ref, Page: page, Search: " HOST "}, &ipc.ListPeersResponse{Peers: []*ipc.Peer{{Id: "peer-a", Hostname: "host-a"}}, SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, MapRevision: 7, TargetMapRevision: 7, Page: &ipc.PageResponse{NextPageToken: "next"}}},
		{"session", "GetSession", nil, &ipc.GetSessionRequest{Profile: ref}, &ipc.GetSessionResponse{Session: &ipc.Session{State: ipc.SessionState_SESSION_STATE_ACTIVE}}},
		{"renew-session", "RenewSession", nil, &ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}, &ipc.RenewSessionResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_RENEW_SESSION)}},
		{"networks", "ListNetworks", []string{"--page-size", "2", "--page-token", "opaque-page"}, &ipc.ListNetworksRequest{Profile: ref, Page: page}, &ipc.ListNetworksResponse{}},
		{"diagnostics", "GetDiagnostics", nil, &ipc.GetDiagnosticsRequest{Profile: ref}, &ipc.GetDiagnosticsResponse{}},
		{"logs-recent", "ListRecentLogs", []string{"--page-size", "2", "--page-token", "opaque-page"}, &ipc.ListRecentLogsRequest{Profile: ref, Page: page}, &ipc.ListRecentLogsResponse{}},
		{"select-network", "SelectNetwork", []string{"--network-id", "network-exact-id"}, &ipc.SelectNetworkRequest{Mutation: mutation, Profile: ref, NetworkId: "network-exact-id"}, &ipc.SelectNetworkResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK)}},
		{"diagnostics-bundle", "CreateDiagnosticsBundle", nil, &ipc.CreateDiagnosticsBundleRequest{Mutation: mutation, Profile: ref}, &ipc.CreateDiagnosticsBundleResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE)}},
	}
	for _, tc := range cases {
		if err := fixture.Expect(testserver.Step{Method: "GetRuntimeInfo", Request: &ipc.GetRuntimeInfoRequest{}, Responses: []proto.Message{&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{Protocol: rpc.Protocol, ContractSha256: rpc.Digest(), InstanceId: "instance"}}}}); err != nil {
			t.Fatal(err)
		}
		if err := fixture.Expect(testserver.Step{Method: tc.method, Request: tc.request, Responses: []proto.Message{tc.response}}); err != nil {
			t.Fatal(err)
		}
	}
	failures := []struct {
		command, method string
		request         proto.Message
	}{
		{"diagnostics", "GetDiagnostics", &ipc.GetDiagnosticsRequest{Profile: ref}},
		{"peers", "ListPeers", &ipc.ListPeersRequest{Profile: ref, Page: &ipc.PageRequest{}}},
	}
	for _, tc := range failures {
		if err := fixture.Expect(testserver.Step{Method: "GetRuntimeInfo", Request: &ipc.GetRuntimeInfoRequest{}, Responses: []proto.Message{&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{Protocol: rpc.Protocol, ContractSha256: rpc.Digest(), InstanceId: "instance"}}}}); err != nil {
			t.Fatal(err)
		}
		if err := fixture.Expect(testserver.Step{Method: tc.method, Request: tc.request, Err: rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)}); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := local.NewServer(fixture.Handler(func(ctx context.Context, _ ipc.Access, _ string) error {
		if _, ok := local.PeerFromContext(ctx); !ok {
			return errors.New("missing native peer")
		}
		return nil
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		<-done
		if err := fixture.Verify(); err != nil {
			t.Error(err)
		}
	}()
	for _, tc := range cases {
		args := append([]string{tc.command, transportFlag, endpoint, "--profile-id", "profile-a", "--timeout", "5s"}, tc.args...)
		if tc.command == "renew-session" || tc.command == "select-network" || tc.command == "diagnostics-bundle" {
			args = append(args, "--request-id", mutation.RequestId, "--expected-instance-id", "instance", "--expected-revision", "7")
		}
		output, err := captureStdout(t, func() error { return cmdService(args) })
		if err != nil {
			t.Fatal(tc.command, err)
		}
		decoded := tc.response.ProtoReflect().Type().New().Interface()
		if err := protojson.Unmarshal([]byte(output), decoded); err != nil || !proto.Equal(decoded, tc.response) {
			t.Fatal("incorrect typed response", tc.command, err)
		}
	}
	for _, tc := range failures {
		output, err := captureStdout(t, func() error {
			return cmdService([]string{tc.command, transportFlag, endpoint, "--profile-id", ref.ProfileId, "--timeout", "5s"})
		})
		if err == nil || output != "" || rpc.FailureFromError(err).GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
			t.Fatal("native failure must be returned without fallback or success output", tc.command, err)
		}
	}
}

func TestNativeCatalogCLIRejectsMissingContext(t *testing.T) {
	for _, command := range []string{"session", "networks", "peers", "diagnostics", "logs-recent"} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery(command, nil, &output); err == nil || output.Len() != 0 {
			t.Fatal("missing profile accepted", command)
		}
	}
	for _, command := range []string{"networks", "peers", "logs-recent"} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery(command, []string{"--profile-id", "profile-a", "--page-size", "501"}, &output); err == nil {
			t.Fatal("unbounded page accepted")
		}
	}
	for _, command := range []string{"renew-session", "select-network", "diagnostics-bundle"} {
		var output bytes.Buffer
		if err := cmdServiceRPCMutation(command, []string{"--profile-id", "profile-a"}, &output); err == nil || output.Len() != 0 {
			t.Fatal("missing durable CAS accepted", command)
		}
	}
}
