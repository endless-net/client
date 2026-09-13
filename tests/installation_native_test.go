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

func awaitInstalledNative(t *testing.T, binary, operation string, target proto.Message, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	err := testclient.Await(ctx, func() bool {
		command := exec.CommandContext(ctx, binary, "service", operation, "--timeout", "2s")
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
