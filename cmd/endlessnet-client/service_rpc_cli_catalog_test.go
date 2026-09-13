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
		{"update-info", "GetUpdateInfo", nil, &ipc.GetUpdateInfoRequest{}, &ipc.GetUpdateInfoResponse{Info: &ipc.UpdateInfo{State: ipc.UpdateState_UPDATE_STATE_SOURCE_UNAVAILABLE}}},
		{"update-info", "GetUpdateInfo", []string{"--reported-ui", `{"commit":"ui-claim","architecture":"arm64"}`}, &ipc.GetUpdateInfoRequest{ReportedUi: &ipc.BuildIdentity{Commit: "ui-claim", Architecture: "arm64"}}, &ipc.GetUpdateInfoResponse{Info: &ipc.UpdateInfo{ReportedUi: &ipc.BuildIdentity{Commit: "ui-claim", Architecture: "arm64"}, State: ipc.UpdateState_UPDATE_STATE_VERIFICATION_FAILED}}},
		{"resources", "ListResources", []string{"--page-size", "4", "--page-token", "opaque-page", "--search", " HOST ", "--kinds", "host,subnet,service,application"}, &ipc.ListResourcesRequest{Profile: ref, Page: &ipc.PageRequest{PageSize: 4, PageToken: "opaque-page"}, Search: " HOST ", Kinds: []ipc.ResourceKind{ipc.ResourceKind_RESOURCE_KIND_HOST, ipc.ResourceKind_RESOURCE_KIND_SUBNET, ipc.ResourceKind_RESOURCE_KIND_SERVICE, ipc.ResourceKind_RESOURCE_KIND_APPLICATION}}, &ipc.ListResourcesResponse{Resources: []*ipc.Resource{
			{Id: "host", Kind: ipc.ResourceKind_RESOURCE_KIND_HOST, Target: &ipc.Resource_Host{Host: &ipc.HostTarget{Hostname: "host.test"}}, Enabled: &ipc.BooleanSetting{Requested: proto.Bool(false)}},
			{Id: "subnet", Kind: ipc.ResourceKind_RESOURCE_KIND_SUBNET, Target: &ipc.Resource_Subnet{Subnet: &ipc.SubnetTarget{Cidr: "192.0.2.0/24"}}},
			{Id: "service", Kind: ipc.ResourceKind_RESOURCE_KIND_SERVICE, Target: &ipc.Resource_Service{Service: &ipc.ServiceTarget{Hostname: "host.test", Port: 443, Protocol: "tcp"}}},
			{Id: "application", Kind: ipc.ResourceKind_RESOURCE_KIND_APPLICATION, Target: &ipc.Resource_Application{Application: &ipc.ApplicationTarget{BrowserUrl: "https://app.example.test"}}},
		}, Page: &ipc.PageResponse{NextPageToken: "next"}}},
		{"set-resource-enabled", "SetResourceEnabled", []string{"--resource-id", "resource-exact-id", "--enabled=true"}, &ipc.SetResourceEnabledRequest{Mutation: mutation, Profile: ref, ResourceId: "resource-exact-id", Enabled: true}, &ipc.SetResourceEnabledResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SET_RESOURCE_ENABLED)}},
		{"set-resource-enabled", "SetResourceEnabled", []string{"--resource-id", "resource-exact-id", "--enabled=false"}, &ipc.SetResourceEnabledRequest{Mutation: mutation, Profile: ref, ResourceId: "resource-exact-id", Enabled: false}, &ipc.SetResourceEnabledResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SET_RESOURCE_ENABLED)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "ipv4-only", "--lan-access", "allow"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_ALLOW}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "ipv4-only", "--lan-access", "block"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "ipv6-only", "--lan-access", "allow"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_ALLOW}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "ipv6-only", "--lan-access", "block"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "dual-stack", "--lan-access", "allow"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, LanAccess: ipc.LanAccess_LAN_ACCESS_ALLOW}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"select-exit-node", "SelectExitNode", []string{"--exit-node-id", "exit-exact-id", "--family-mode", "dual-stack", "--lan-access", "block"}, &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, &ipc.SelectExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE)}},
		{"clear-exit-node", "ClearExitNode", nil, &ipc.ClearExitNodeRequest{Mutation: mutation, Profile: ref}, &ipc.ClearExitNodeResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE)}},
		{"exit-nodes", "ListExitNodes", []string{"--page-size", "2", "--page-token", "opaque-page"}, &ipc.ListExitNodesRequest{Profile: ref, Page: page}, &ipc.ListExitNodesResponse{ExitNodes: []*ipc.ExitNode{{Id: "exit-a", AllowedFamilyModes: []ipc.ExitFamilyMode{ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY}}}, Page: &ipc.PageResponse{NextPageToken: "next"}}},
		{"exit-node", "GetExitNode", nil, &ipc.GetExitNodeRequest{Profile: ref}, &ipc.GetExitNodeResponse{Status: &ipc.ExitNodeStatus{ProfileId: ref.ProfileId, RequestedExitNodeId: proto.String("exit-a"), RequestedFamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, ApplyState: ipc.ApplyState_APPLY_STATE_FAILED, Ipv4: &ipc.ExitFamilyStatus{RequestedExitNodeId: proto.String("exit-a"), EffectiveExitNodeId: proto.String("exit-a"), ApplyState: ipc.ApplyState_APPLY_STATE_APPLIED, FailClosed: true}, Ipv6: &ipc.ExitFamilyStatus{RequestedExitNodeId: proto.String("exit-a"), ApplyState: ipc.ApplyState_APPLY_STATE_FAILED, Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE}}}}},
		{"peers", "ListPeers", []string{"--page-size", "2", "--page-token", "opaque-page", "--search", " HOST "}, &ipc.ListPeersRequest{Profile: ref, Page: page, Search: " HOST "}, &ipc.ListPeersResponse{Peers: []*ipc.Peer{{Id: "peer-a", Hostname: "host-a"}}, SnapshotState: ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT, MapRevision: 7, TargetMapRevision: 7, Page: &ipc.PageResponse{NextPageToken: "next"}}},
		{"preferences", "GetPreferences", nil, &ipc.GetPreferencesRequest{Profile: ref}, &ipc.GetPreferencesResponse{Preferences: &ipc.Preferences{ProfileId: ref.ProfileId, AcceptDns: &ipc.BooleanSetting{Effective: true}}}},
		{"managed-settings", "ListManagedSettings", nil, &ipc.ListManagedSettingsRequest{Profile: ref}, &ipc.ListManagedSettingsResponse{Settings: []*ipc.ManagedSetting{{Key: ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS, Control: &ipc.SettingControl{Locked: true, Source: ipc.SettingSource_SETTING_SOURCE_ACCOUNT_POLICY}}}}},
		{"session", "GetSession", nil, &ipc.GetSessionRequest{Profile: ref}, &ipc.GetSessionResponse{Session: &ipc.Session{State: ipc.SessionState_SESSION_STATE_ACTIVE}}},
		{"set-preferences", "SetPreferences", []string{"--patch", `{"acceptDns":false,"uiQuit":"LIFECYCLE_BEHAVIOR_DISCONNECT"}`}, &ipc.SetPreferencesRequest{Mutation: mutation, Profile: ref, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), UiQuit: ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()}}, &ipc.SetPreferencesResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_SET_PREFERENCES)}},
		{"reset-preferences", "ResetPreferences", []string{"--keys", "accept-dns,ui-quit"}, &ipc.ResetPreferencesRequest{Mutation: mutation, Profile: ref, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_DNS, ipc.PreferenceKey_PREFERENCE_KEY_UI_QUIT}}, &ipc.ResetPreferencesResponse{Operation: accepted(ipc.OperationKind_OPERATION_KIND_RESET_PREFERENCES)}},
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
		code            ipc.ErrorCode
		transportCode   connect.Code
	}{
		{"update-info", "GetUpdateInfo", &ipc.GetUpdateInfoRequest{}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"resources", "ListResources", &ipc.ListResourcesRequest{Profile: ref, Page: &ipc.PageRequest{}}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"set-resource-enabled", "SetResourceEnabled", &ipc.SetResourceEnabledRequest{Mutation: mutation, Profile: ref, ResourceId: "resource-exact-id", Enabled: true}, ipc.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT, connect.CodeFailedPrecondition},
		{"set-resource-enabled", "SetResourceEnabled", &ipc.SetResourceEnabledRequest{Mutation: mutation, Profile: ref, ResourceId: "resource-exact-id", Enabled: true}, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED, connect.CodePermissionDenied},
		{"select-exit-node", "SelectExitNode", &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"select-exit-node", "SelectExitNode", &ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: "exit-exact-id", FamilyMode: ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK, LanAccess: ipc.LanAccess_LAN_ACCESS_BLOCK}, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED, connect.CodePermissionDenied},
		{"clear-exit-node", "ClearExitNode", &ipc.ClearExitNodeRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"clear-exit-node", "ClearExitNode", &ipc.ClearExitNodeRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_POLICY_BLOCKED, connect.CodePermissionDenied},
		{"exit-nodes", "ListExitNodes", &ipc.ListExitNodesRequest{Profile: ref, Page: &ipc.PageRequest{}}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"exit-node", "GetExitNode", &ipc.GetExitNodeRequest{Profile: ref}, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED, connect.CodePermissionDenied},
		{"diagnostics", "GetDiagnostics", &ipc.GetDiagnosticsRequest{Profile: ref}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, connect.CodeUnavailable},
		{"peers", "ListPeers", &ipc.ListPeersRequest{Profile: ref, Page: &ipc.PageRequest{}}, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, connect.CodeUnavailable},
		{"session", "GetSession", &ipc.GetSessionRequest{Profile: ref}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"renew-session", "RenewSession", &ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED, connect.CodeUnimplemented},
		{"renew-session", "RenewSession", &ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN, connect.CodeFailedPrecondition},
		{"renew-session", "RenewSession", &ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_STALE_STATE, connect.CodeFailedPrecondition},
		{"renew-session", "RenewSession", &ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED, connect.CodePermissionDenied},
	}
	for _, tc := range failures {
		if err := fixture.Expect(testserver.Step{Method: "GetRuntimeInfo", Request: &ipc.GetRuntimeInfoRequest{}, Responses: []proto.Message{&ipc.GetRuntimeInfoResponse{Runtime: &ipc.RuntimeInfo{Protocol: rpc.Protocol, ContractSha256: rpc.Digest(), InstanceId: "instance"}}}}); err != nil {
			t.Fatal(err)
		}
		if err := fixture.Expect(testserver.Step{Method: tc.method, Request: tc.request, Err: rpc.Error(tc.transportCode, tc.code)}); err != nil {
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
		args := append([]string{tc.command, transportFlag, endpoint, "--timeout", "5s"}, tc.args...)
		if tc.command != "update-info" {
			args = append(args, "--profile-id", ref.ProfileId)
		}
		if tc.command == "set-preferences" || tc.command == "reset-preferences" || tc.command == "renew-session" || tc.command == "select-network" || tc.command == "diagnostics-bundle" || tc.command == "select-exit-node" || tc.command == "clear-exit-node" || tc.command == "set-resource-enabled" {
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
		args := []string{tc.command, transportFlag, endpoint, "--timeout", "5s"}
		if tc.command != "update-info" {
			args = append(args, "--profile-id", ref.ProfileId)
		}
		if tc.command == "renew-session" || tc.command == "clear-exit-node" || tc.command == "select-exit-node" || tc.command == "set-resource-enabled" {
			args = append(args, "--request-id", mutation.RequestId, "--expected-instance-id", "instance", "--expected-revision", "7")
		}
		if tc.command == "select-exit-node" {
			args = append(args, "--exit-node-id", "exit-exact-id", "--family-mode", "dual-stack", "--lan-access", "block")
		}
		if tc.command == "set-resource-enabled" {
			args = append(args, "--resource-id", "resource-exact-id", "--enabled=true")
		}
		output, err := captureStdout(t, func() error {
			return cmdService(args)
		})
		if err == nil || output != "" || rpc.FailureFromError(err).GetCode() != tc.code || connect.CodeOf(err) != tc.transportCode {
			t.Fatal("native failure must be returned without fallback or success output", tc.command, err)
		}
	}
}

func TestNativeCatalogCLIRejectsMissingContext(t *testing.T) {
	for _, command := range []string{"preferences", "managed-settings", "session", "networks", "peers", "diagnostics", "logs-recent", "exit-nodes", "exit-node"} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery(command, nil, &output); err == nil || output.Len() != 0 {
			t.Fatal("missing profile accepted", command)
		}
	}
	for _, command := range []string{"networks", "peers", "logs-recent", "exit-nodes"} {
		var output bytes.Buffer
		if err := cmdServiceRPCQuery(command, []string{"--profile-id", "profile-a", "--page-size", "501"}, &output); err == nil {
			t.Fatal("unbounded page accepted")
		}
	}
	for _, command := range []string{"set-preferences", "reset-preferences", "renew-session", "select-network", "diagnostics-bundle"} {
		var output bytes.Buffer
		if err := cmdServiceRPCMutation(command, []string{"--profile-id", "profile-a"}, &output); err == nil || output.Len() != 0 {
			t.Fatal("missing durable CAS accepted", command)
		}
	}
}

func TestNativeSessionCLIRejectsCallerSuppliedRenewalSecrets(t *testing.T) {
	base := []string{"--profile-id", "profile", "--request-id", "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", "--expected-instance-id", "instance", "--expected-revision", "7"}
	for _, flag := range []string{"--callback-url", "--enrollment-token-file", "--browser-login", "--token"} {
		var output bytes.Buffer
		args := append(append([]string(nil), base...), flag, "synthetic-value")
		if err := cmdServiceRPCMutation("renew-session", args, &output); err == nil || output.Len() != 0 {
			t.Fatal("renewal accepted out-of-contract input", flag)
		}
	}
}
