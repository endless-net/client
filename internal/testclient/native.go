package testclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// NativeService preserves the published protobuf JSON schema, including enums,
// oneofs and presence. Never translate it into the retired HTTP DTOs or print
// arbitrary subprocess output on failure.
func (n *Node) NativeService(operation string, target proto.Message, options ...string) error {
	out, err := n.ServiceCommand(operation, options...)
	if err != nil {
		return NativeServiceCommandError(operation, out)
	}
	return decodeNativeService(out, target)
}

// NativeServiceCommandError recognizes only the complete canonical typed failure line emitted by the CLI.
// Never retain subprocess output or infer a code from a substring in diagnostics.
func NativeServiceCommandError(operation string, output []byte) error {
	if len(output) <= 256 {
		line := strings.TrimSpace(string(output))
		for code := connect.CodeCanceled; code <= connect.CodeUnauthenticated; code++ {
			for number := range ipc.ErrorCode_name {
				failure := ipc.ErrorCode(number)
				if number != 0 && line == rpc.Error(code, failure).Error() {
					return fmt.Errorf("native service %s failed (transport=%d failure=%d; output withheld)", operation, code, failure)
				}
			}
		}
	}
	return fmt.Errorf("native service %s failed (unclassified subprocess failure; output withheld)", operation)
}

func decodeNativeService(out []byte, target proto.Message) error {
	if target == nil || !target.ProtoReflect().IsValid() {
		return errors.New("native response target required")
	}
	if err := protojson.Unmarshal(out, target); err != nil {
		return errors.New("invalid native protobuf JSON (output withheld)")
	}
	return nil
}

func (n *Node) AwaitNativeStatus(match func(*ipc.Status) bool) *ipc.Status {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var status *ipc.Status
	if err := Await(ctx, func() bool {
		response := &ipc.GetStatusResponse{}
		if n.NativeService("status", response, "--timeout", "1s") != nil {
			return false
		}
		status = response.Status
		return status != nil && match(status)
	}); err != nil {
		n.logWireGuardStartupStages()
		n.t.Fatal("native status condition not reached")
	}
	return status
}

// Mutation arguments are captured once. Callers retain and explicitly replay
// these exact arguments for idempotency tests; no CAS refresh or automatic retry.
func NativeMutationArguments(id string, status *ipc.Status) []string {
	return []string{"--request-id", id, "--profile-id", status.ActiveProfileId, "--expected-instance-id", status.GetMetadata().GetInstanceId(), "--expected-revision", fmt.Sprint(status.GetMetadata().GetRevision())}
}

func (n *Node) AwaitNativeOperation(id string) *ipc.Operation {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var op *ipc.Operation
	if err := Await(ctx, func() bool {
		response := &ipc.GetOperationResponse{}
		if n.NativeService("operation", response, "--operation-id", id, "--timeout", "1s") != nil {
			return false
		}
		op = response.Operation
		return op != nil && (op.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.State == ipc.OperationState_OPERATION_STATE_FAILED || op.State == ipc.OperationState_OPERATION_STATE_CANCELLED)
	}); err != nil {
		n.t.Fatal("native operation did not finish")
	}
	return op
}

func (n *Node) awaitNativeReady() {
	n.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Await(ctx, func() bool {
		response := &ipc.GetRuntimeInfoResponse{}
		return n.NativeService("runtime-info", response, "--timeout", "1s") == nil && response.GetRuntime().GetInstanceId() != ""
	}); err != nil {
		n.t.Fatal("native runtime did not become ready")
	}
}
