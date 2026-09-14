//go:build windows || linux || darwin

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestAgentNativeRPCHostBootstrapAndStop(t *testing.T) {
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-agent-rpc-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-host-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	configPath := filepath.Join(t.TempDir(), "client.json")
	store, err := client.OpenConfigStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	wireGuard := &testAgentWireGuard{}
	opts := agentIPCOptions{ConfigStore: store, OperationMu: &sync.Mutex{}, WireGuard: wireGuard}
	if runtime.GOOS == "windows" {
		opts.Pipe = endpoint
	} else {
		opts.UnixSocket = endpoint
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	stop, mutations, err := startAgentRPC(ctx, cancel, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	requestCtx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	bootstrapInfo, err := consumer.Bootstrap(requestCtx)
	if err != nil {
		t.Fatal("agent did not expose native bootstrap", err)
	}
	assertBuild := func(build *ipc.BuildIdentity) {
		t.Helper()
		platform := map[string]ipc.Platform{"windows": ipc.Platform_PLATFORM_WINDOWS, "linux": ipc.Platform_PLATFORM_LINUX, "darwin": ipc.Platform_PLATFORM_MACOS}[runtime.GOOS]
		if build == nil || build.Version != version || build.Commit != commit || build.BuildDate != buildDate || build.Architecture != runtime.GOARCH || build.Platform != platform {
			t.Fatal("native host lost the executable build identity")
		}
	}
	assertBuild(bootstrapInfo.Build)
	assertConnectionCapability := func(info *ipc.RuntimeInfo) {
		t.Helper()
		want := []ipc.Capability{ipc.Capability_CAPABILITY_ENROLLMENT, ipc.Capability_CAPABILITY_CONNECTION, ipc.Capability_CAPABILITY_PEERS, ipc.Capability_CAPABILITY_NETWORK_SELECTION, ipc.Capability_CAPABILITY_IDENTITY_RECOVERY, ipc.Capability_CAPABILITY_DIAGNOSTICS, ipc.Capability_CAPABILITY_LOGOUT, ipc.Capability_CAPABILITY_LOCAL_FORGET, ipc.Capability_CAPABILITY_PROFILES, ipc.Capability_CAPABILITY_SESSION_RENEWAL, ipc.Capability_CAPABILITY_PREFERENCES, ipc.Capability_CAPABILITY_RESOURCES, ipc.Capability_CAPABILITY_SUPPORT_INFO}
		if len(info.Capabilities) != len(want) {
			t.Fatal("native host advertised incomplete or unwired capability families")
		}
		for i, capability := range info.Capabilities {
			if capability.Capability != want[i] || capability.Restriction.Availability != ipc.Availability_AVAILABILITY_AVAILABLE || capability.Platform != info.Build.Platform {
				t.Fatal("native host capability readiness differs from its workers")
			}
		}
	}
	assertConnectionCapability(bootstrapInfo)
	publishAgentRPCObservation(requestCtx, mutations, opts, ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED, nil)
	observed, err := consumer.GetStatus(requestCtx, connect.NewRequest(&ipc.GetStatusRequest{}))
	if err != nil || observed.Msg.Status.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
		t.Fatal("native host did not publish runtime observation", err)
	}
	transportFlag := "--ipc-socket"
	if runtime.GOOS == "windows" {
		transportFlag = "--ipc-pipe"
	}
	for command, response := range map[string]proto.Message{
		"status": &ipc.GetStatusResponse{}, "runtime-info": &ipc.GetRuntimeInfoResponse{}, "support-info": &ipc.GetSupportInfoResponse{},
	} {
		output, err := captureStdout(t, func() error {
			return cmdService([]string{command, transportFlag, endpoint, "--timeout", "5s"})
		})
		if err != nil {
			t.Fatal(command, err)
		}
		if err := protojson.Unmarshal([]byte(output), response); err != nil {
			t.Fatal("CLI response does not follow native protobuf JSON", command, err)
		}
		switch response := response.(type) {
		case *ipc.GetRuntimeInfoResponse:
			assertBuild(response.GetRuntime().GetBuild())
			info := response.GetRuntime()
			assertConnectionCapability(info)
			if info.GetProtocol() != bootstrapInfo.Protocol || info.GetIpcVersion() != bootstrapInfo.IpcVersion || info.GetContractSha256() != bootstrapInfo.ContractSha256 || info.GetInstanceId() != bootstrapInfo.InstanceId {
				t.Fatal("native CLI runtime identity differs from verified bootstrap")
			}
		case *ipc.GetSupportInfoResponse:
			assertBuild(response.GetInfo().GetRuntime())
		}
	}
	eventOutput, err := captureStdout(t, func() error {
		return cmdService([]string{"events", transportFlag, endpoint, "--timeout", "1s"})
	})
	if err != nil {
		t.Fatal("native CLI event subscription failed", err)
	}
	event := new(ipc.WatchEventsResponse)
	if err := protojson.Unmarshal([]byte(eventOutput), event); err != nil || event.Sequence != 1 || event.GetSnapshot() == nil {
		t.Fatal("native CLI did not emit the opening snapshot", err)
	}
	assertConnectionCapability(event.GetSnapshot().GetRuntime())
	requestID := "c96bfe40-876a-4bc8-95da-4fdd494ab48d"
	accepted, err := consumer.CreateProfile(requestCtx, connect.NewRequest(&ipc.CreateProfileRequest{
		Mutation:    &ipc.MutationContext{RequestId: requestID, ExpectedInstanceId: event.Metadata.InstanceId, ExpectedRevision: event.Metadata.Revision},
		DisplayName: "CLI recovery", ControlOrigin: "https://control.example.test",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for flag, id := range map[string]string{"--request-id": requestID, "--operation-id": accepted.Msg.Operation.Id} {
		output, err := captureStdout(t, func() error {
			return cmdService([]string{"operation", flag, id, "--wait", transportFlag, endpoint, "--timeout", "5s"})
		})
		if err != nil {
			t.Fatal("CLI operation lookup failed", flag, err)
		}
		response := new(ipc.GetOperationResponse)
		if err := protojson.Unmarshal([]byte(output), response); err != nil || !proto.Equal(response.Operation, accepted.Msg.Operation) {
			t.Fatal("CLI did not recover the original operation", flag, err)
		}
	}
	output, err := captureStdout(t, func() error {
		return cmdService([]string{"operation", "--request-id", "3d68cf78-6998-42dc-9b62-aaae3dfc6535", transportFlag, endpoint, "--timeout", "5s"})
	})
	if connect.CodeOf(err) != connect.CodeNotFound || output != "" {
		t.Fatal("unknown request was not reported as missing", err)
	}
	profileCommandCounter := 0
	identityOutput, identityErr := captureStdout(t, func() error {
		return cmdService([]string{"server-identity", transportFlag, endpoint, "--profile-id", accepted.Msg.Operation.ProfileId, "--timeout", "5s"})
	})
	if connect.CodeOf(identityErr) != connect.CodeFailedPrecondition || identityOutput != "" {
		t.Fatal("empty profile identity was fabricated", identityErr)
	}
	profileCommand := func(command string, extra ...string) *ipc.Operation {
		t.Helper()
		listed, err := captureStdout(t, func() error { return cmdService([]string{"profiles", transportFlag, endpoint, "--timeout", "5s"}) })
		if err != nil {
			t.Fatal(err)
		}
		catalog := new(ipc.ListProfilesResponse)
		if err := protojson.Unmarshal([]byte(listed), catalog); err != nil {
			t.Fatal(err)
		}
		profileCommandCounter++
		args := []string{command, transportFlag, endpoint, "--timeout", "5s", "--request-id", fmt.Sprintf("0fa35b42-a172-4866-8089-%012x", profileCommandCounter),
			"--expected-instance-id", catalog.Page.Metadata.InstanceId, "--expected-revision", fmt.Sprint(catalog.Page.Metadata.Revision)}
		args = append(args, extra...)
		result, err := captureStdout(t, func() error { return cmdService(args) })
		if err != nil {
			t.Fatal(command, err)
		}
		decoded := new(ipc.GetOperationResponse)
		if err := protojson.Unmarshal([]byte(result), decoded); err != nil {
			t.Fatal(err)
		}
		return decoded.Operation
	}
	selected := profileCommand("select-profile", "--profile-id", accepted.Msg.Operation.ProfileId)
	for selected.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		if selected.State == ipc.OperationState_OPERATION_STATE_FAILED {
			t.Fatal("CLI profile selection failed", selected.GetFailure().Code)
		}
		select {
		case <-requestCtx.Done():
			t.Fatal(requestCtx.Err())
		case <-time.After(10 * time.Millisecond):
		}
		polled, err := consumer.GetOperation(requestCtx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: selected.Id}}))
		if err != nil {
			t.Fatal(err)
		}
		selected = polled.Msg.Operation
	}
	created := profileCommand("create-profile", "--display-name", "temporary", "--control-origin", "https://other.example.test")
	inactiveEnrollment, err := captureStdout(t, func() error {
		return cmdService([]string{"enroll", transportFlag, endpoint, "--timeout", "5s", "--profile-id", created.ProfileId,
			"--expected-instance-id", created.Metadata.InstanceId, "--expected-revision", fmt.Sprint(created.Metadata.Revision),
			"--request-id", "d3e5a9e6-6af0-4f19-b290-e1775b38f095", "--mode", "interactive", "--browser-login"})
	})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || inactiveEnrollment != "" {
		t.Fatal("CLI enrollment bypassed active-profile requirement", err)
	}
	renamed := profileCommand("rename-profile", "--profile-id", created.ProfileId, "--display-name", "renamed")
	if renamed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("CLI rename failed")
	}
	listed, err := captureStdout(t, func() error {
		return cmdService([]string{"profiles", transportFlag, endpoint, "--page-size", "1", "--timeout", "5s"})
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := new(ipc.ListProfilesResponse)
	if err := protojson.Unmarshal([]byte(listed), catalog); err != nil || len(catalog.Profiles) != 1 || catalog.Page.NextPageToken == "" {
		t.Fatal("CLI pagination failed", err)
	}
	secondPage, err := captureStdout(t, func() error {
		return cmdService([]string{"profiles", transportFlag, endpoint, "--page-size", "1", "--page-token", catalog.Page.NextPageToken, "--timeout", "5s"})
	})
	if err != nil {
		t.Fatal(err)
	}
	next := new(ipc.ListProfilesResponse)
	if err := protojson.Unmarshal([]byte(secondPage), next); err != nil || len(next.Profiles) != 1 || next.Page.NextPageToken != "" || proto.Equal(next.Profiles[0], catalog.Profiles[0]) {
		t.Fatal("CLI next page failed", err)
	}
	if next.Profiles[0].DisplayName != "renamed" && catalog.Profiles[0].DisplayName != "renamed" {
		t.Fatal("CLI rename not reflected in catalog")
	}
	removed := profileCommand("remove-profile", "--profile-id", created.ProfileId)
	if removed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("CLI remove failed")
	}
	mutationArgs := []string{transportFlag, endpoint, "--timeout", "5s", "--profile-id", accepted.Msg.Operation.ProfileId,
		"--expected-instance-id", event.Metadata.InstanceId, "--expected-revision", fmt.Sprint(removed.Metadata.Revision)}
	for _, command := range []string{"connect", "logout"} {
		args := append([]string{command, "--request-id", "3152ad0b-8ecb-4b42-bd17-e68dcb31fa44"}, mutationArgs...)
		output, err := captureStdout(t, func() error { return cmdService(args) })
		if connect.CodeOf(err) != connect.CodeFailedPrecondition || output != "" {
			t.Fatal("unregistered mutation accepted", command, err)
		}
	}
	disconnectArgs := append([]string{"disconnect", "--request-id", "0b9b9e54-42c5-4fca-a2e7-1d9c3a3d39c0"}, mutationArgs...)
	staleArgs := append([]string{}, disconnectArgs...)
	staleArgs[len(staleArgs)-1] = "999999"
	output, err = captureStdout(t, func() error { return cmdService(staleArgs) })
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || output != "" {
		t.Fatal("stale disconnect accepted", err)
	}
	var original *ipc.Operation
	downBefore := wireGuard.downCalls
	for range 2 {
		output, err := captureStdout(t, func() error { return cmdService(disconnectArgs) })
		if err != nil {
			t.Fatal("native CLI disconnect failed", err)
		}
		response := new(ipc.DisconnectResponse)
		if err := protojson.Unmarshal([]byte(output), response); err != nil || response.GetOperation().GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			t.Fatal("disconnect did not confirm actual Down", err)
		}
		if original != nil && !proto.Equal(original, response.Operation) {
			t.Fatal("CLI exact retry created a new operation")
		}
		original = response.Operation
	}
	if wireGuard.downCalls != downBefore+1 {
		t.Fatal("native disconnect retry repeated tunnel teardown")
	}
	disconnected, err := consumer.GetStatus(requestCtx, connect.NewRequest(&ipc.GetStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	state := disconnected.Msg.Status
	if state.ConnectionPhase != ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED || !state.UserDisconnected || state.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED || state.GetStoredState().GetNodeCredentialPresent() {
		t.Fatal("unenrolled native disconnect lost explicit user intent")
	}
	persisted, err := client.LoadConfig(configPath)
	if err != nil || persisted.ConnectionIntent == nil || persisted.ConnectionIntent.DesiredState != client.ConnectionIntentDesiredDisconnected || persisted.ConnectionIntent.Reason != "user_disconnect" {
		t.Fatal("native disconnect intent was not durable", err)
	}
	info, err := consumer.Bootstrap(requestCtx)
	if err != nil {
		t.Fatal(err)
	}
	forgetArgs := []string{"local-forget", transportFlag, endpoint, "--timeout", "5s", "--profile-id", accepted.Msg.Operation.ProfileId,
		"--expected-instance-id", original.Metadata.InstanceId, "--expected-revision", fmt.Sprint(original.Metadata.Revision),
		"--request-id", "28ab0d8e-6e10-4292-a273-4a6be96fdc26", "--confirm-local-forget"}
	trustArgs := []string{"trust-server", transportFlag, endpoint, "--timeout", "5s", "--profile-id", accepted.Msg.Operation.ProfileId,
		"--expected-instance-id", original.Metadata.InstanceId, "--expected-revision", fmt.Sprint(original.Metadata.Revision),
		"--request-id", "460956c3-df16-4ff6-b180-2ee5bca50d97", "--confirmed-control-origin", "https://control.example.test",
		"--confirmed-key-id", "synthetic-key", "--confirmed-announcement-id", strings.Repeat("a", 64)}
	trustOutput, trustErr := captureStdout(t, func() error { return cmdService(trustArgs) })
	expectedTrustCode := connect.CodeFailedPrecondition // Empty profile has no installed trust.
	if info.CallerAccess != ipc.Access_ACCESS_ADMINISTRATOR {
		expectedTrustCode = connect.CodePermissionDenied
	}
	if connect.CodeOf(trustErr) != expectedTrustCode || trustOutput != "" {
		t.Fatal("native trust CLI bypassed authorization/preconditions", trustErr)
	}
	forgottenOutput, forgetErr := captureStdout(t, func() error { return cmdService(forgetArgs) })
	if info.CallerAccess != ipc.Access_ACCESS_ADMINISTRATOR {
		if connect.CodeOf(forgetErr) != connect.CodePermissionDenied || forgottenOutput != "" {
			t.Fatal("local forget bypassed OS administrator authorization", forgetErr)
		}
	} else {
		if forgetErr != nil {
			t.Fatal("native CLI local forget failed", forgetErr)
		}
		forgotten := new(ipc.ForgetLocalEnrollmentResponse)
		if err := protojson.Unmarshal([]byte(forgottenOutput), forgotten); err != nil {
			t.Fatal(err)
		}
		for forgotten.Operation.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			if forgotten.Operation.State == ipc.OperationState_OPERATION_STATE_FAILED {
				t.Fatal("native cleanup failed")
			}
			select {
			case <-requestCtx.Done():
				t.Fatal(requestCtx.Err())
			case <-time.After(10 * time.Millisecond):
			}
			polled, err := consumer.GetOperation(requestCtx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: forgotten.Operation.Id}}))
			if err != nil {
				t.Fatal(err)
			}
			forgotten.Operation = polled.Msg.Operation
		}
		cleanup := forgotten.Operation.GetCleanup()
		if cleanup.GetOutcome() != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_UNCONFIRMED || !cleanup.GetLocalRegistrationRemoved() {
			t.Fatal("local cleanup incorrectly claimed remote revocation")
		}
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("normal host stop reported runtime failure", context.Cause(ctx))
	}
	if _, err := consumer.Bootstrap(requestCtx); err == nil {
		t.Fatal("native host continued accepting after stop")
	}
	consumer.Close()
	if err := store.Update(func(cfg *client.Config) error {
		cfg.RPCState = &client.ClientRPCState{DisconnectOperationID: "missing-operation"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	failedStop, _, err := startAgentRPC(ctx, cancel, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = failedStop() }()
	select {
	case <-ctx.Done():
		if err := failedStop(); err == nil || err != context.Cause(ctx) {
			t.Fatal("host failure did not reach the agent loop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed worker left the agent running")
	}
}

func TestAgentNativeRPCHostRejectsMixedEndpoints(t *testing.T) {
	store, err := client.OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = startAgentRPC(t.Context(), func(error) {}, agentIPCOptions{
		Pipe: "pipe", UnixSocket: "socket", ConfigStore: store, OperationMu: &sync.Mutex{}, WireGuard: &testAgentWireGuard{},
	})
	if err == nil {
		t.Fatal("mixed endpoints accepted")
	}
}
