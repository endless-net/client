package tests

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func awaitInstalledNative(t *testing.T, binary, operation string, target proto.Message, ready func() bool, options ...string) {
	t.Helper()
	awaitInstalledNativeWithin(t, binary, operation, 45*time.Second, target, ready, options...)
}

// Installed services retain their production retry policy: up to five minutes
// between attempts, including jitter. Leave another minute for the sync and IPC.
const installedControlRecoveryTimeout = 6 * time.Minute

func awaitInstalledNativeWithin(t *testing.T, binary, operation string, timeout time.Duration, target proto.Message, ready func() bool, options ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	err := testclient.Await(ctx, func() bool {
		args := append([]string{"service", operation, "--timeout", "2s"}, options...)
		command := exec.CommandContext(ctx, binary, args...)
		command.WaitDelay = 2 * time.Second
		output, err := command.CombinedOutput()
		if err != nil {
			return false
		}
		if err := protojson.Unmarshal(output, target); err != nil {
			t.Fatal("installed service returned invalid native protobuf JSON (output withheld)")
		}
		return ready()
	})
	if err != nil {
		t.Fatalf("installed native service condition was not reached: %s", installedNativeObservation(target))
	}
}

// Timeout evidence is an explicit numeric/boolean allowlist, never a protobuf
// dump: installed status can contain identities, browser actions and failures.
func installedNativeObservation(target proto.Message) string {
	response, ok := target.(*ipc.GetStatusResponse)
	if !ok || response.GetStatus() == nil {
		return "status unavailable"
	}
	s := response.Status
	a := s.GetAgent()
	var generatedAt int64
	if stamp := a.GetGeneratedAt(); stamp != nil && stamp.IsValid() {
		generatedAt = stamp.GetSeconds()
	}
	return fmt.Sprintf("service=%d control=%d connection=%d desired=%d disconnected=%t map=%d agent_present=%t agent_state=%d agent_map=%d agent_failure=%t agent_generated_unix=%d",
		s.ServiceState, s.ControlState, s.ConnectionPhase, s.GetIntent().GetDesiredState(), s.UserDisconnected,
		s.MapRevision, a != nil, a.GetSnapshotState(), a.GetMapRevision(), a.GetLastFailure() != nil, generatedAt)
}

func waitInstalledNativeCondition(t *testing.T, binary string, predicate func(*ipc.Status) bool) *ipc.Status {
	t.Helper()
	response := &ipc.GetStatusResponse{}
	awaitInstalledNative(t, binary, "status", response, func() bool { return response.Status != nil && predicate(response.Status) })
	return response.Status
}

func runInstalledNativeMutation(t *testing.T, binary, operation, requestID string) {
	t.Helper()
	status := waitInstalledNativeCondition(t, binary, func(v *ipc.Status) bool { return v.ActiveProfileId != "" })
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	var output []byte
	err := retryNativeControlAdmission(operation, status, func() (*ipc.Status, error) {
		data, err := exec.CommandContext(ctx, binary, "service", "status", "--timeout", "2s").CombinedOutput()
		if err != nil {
			return nil, testclient.NativeServiceCommandError("status", data)
		}
		response := &ipc.GetStatusResponse{}
		if protojson.Unmarshal(data, response) != nil {
			return nil, fmt.Errorf("installed status returned invalid protobuf JSON (output withheld)")
		}
		return response.Status, nil
	}, func(current *ipc.Status) error {
		args := append([]string{"service", operation, "--timeout", "30s"}, testclient.NativeMutationArguments(requestID, current)...)
		var err error
		output, err = exec.CommandContext(ctx, binary, args...).CombinedOutput()
		if err != nil {
			return testclient.NativeServiceCommandError(operation, output)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var accepted *ipc.Operation
	var kind ipc.OperationKind
	switch operation {
	case "connect":
		response := &ipc.ConnectResponse{}
		if protojson.Unmarshal(output, response) != nil {
			t.Fatal("installed connect returned invalid protobuf JSON")
		}
		accepted, kind = response.Operation, ipc.OperationKind_OPERATION_KIND_CONNECT
	case "disconnect":
		response := &ipc.DisconnectResponse{}
		if protojson.Unmarshal(output, response) != nil {
			t.Fatal("installed disconnect returned invalid protobuf JSON")
		}
		accepted, kind = response.Operation, ipc.OperationKind_OPERATION_KIND_DISCONNECT
	default:
		t.Fatal("unsupported installed test mutation")
	}
	if accepted.GetId() == "" || accepted.GetRequestId() != requestID || accepted.GetProfileId() != status.ActiveProfileId || accepted.GetKind() != kind {
		t.Fatal("installed mutation omitted its request/profile-bound operation")
	}
	result := &ipc.GetOperationResponse{}
	awaitInstalledNative(t, binary, "operation", result, func() bool {
		switch result.GetOperation().GetState() {
		case ipc.OperationState_OPERATION_STATE_SUCCEEDED, ipc.OperationState_OPERATION_STATE_FAILED, ipc.OperationState_OPERATION_STATE_CANCELLED:
			return true
		default:
			return false
		}
	}, "--operation-id", accepted.Id)
	completed := result.GetOperation()
	if completed.GetId() != accepted.Id || completed.GetRequestId() != requestID || completed.GetProfileId() != status.ActiveProfileId || completed.GetKind() != kind || completed.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("installed mutation did not complete successfully for its original request/profile")
	}
}

func waitInstalledNativeRuntime(t *testing.T, binary string) *ipc.RuntimeInfo {
	t.Helper()
	response := &ipc.GetRuntimeInfoResponse{}
	awaitInstalledNative(t, binary, "runtime-info", response, func() bool { return response.GetRuntime().GetInstanceId() != "" })
	info := response.Runtime
	if info.Protocol != rpc.Protocol || info.IpcVersion != rpc.Version || info.ContractSha256 != rpc.Digest() {
		t.Fatal("installed service returned a different native contract")
	}
	return info
}

func waitInstalledNativeUnenrolled(t *testing.T, binary string) *ipc.Status {
	t.Helper()
	response := &ipc.GetStatusResponse{}
	awaitInstalledNative(t, binary, "status", response, func() bool {
		return response.GetStatus().GetServiceState() == ipc.ServiceState_SERVICE_STATE_NEEDS_ENROLLMENT
	})
	if response.Status.GetMetadata().GetInstanceId() == "" || response.Status.GetMetadata().GetRevision() == 0 {
		t.Fatal("native status omitted snapshot identity")
	}
	return response.Status
}
