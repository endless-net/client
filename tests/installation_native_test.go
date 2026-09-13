package tests

import (
	"context"
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
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
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
		t.Fatal("installed native service condition was not reached")
	}
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
	args := append([]string{"service", operation, "--timeout", "30s"}, testclient.NativeMutationArguments(requestID, status)...)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, args...).Output()
	if err != nil {
		t.Fatal("installed native mutation failed (output withheld)")
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
